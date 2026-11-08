import { api } from "./api-client";
import type { EventItem, RefundStatus } from "@/types";

export interface PendingEvent extends EventItem {
  organizer_id: string;
}

export interface PendingListResponse {
  events: PendingEvent[];
  total: number;
}

export const adminApi = {
  listPending(limit = 20, offset = 0) {
    return api.get<PendingListResponse>(`/api/events/pending?limit=${limit}&offset=${offset}`);
  },

  listAll(status = "", limit = 20, offset = 0) {
    const qs = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    if (status) qs.set("status", status);
    return api.get<{ events: EventItem[]; total: number }>(`/api/admin/events?${qs.toString()}`);
  },

  approveEvent(id: string) {
    return api.post<void>(`/api/events/${id}/approve`);
  },

  rejectEvent(id: string, reason: string) {
    return api.post<void>(`/api/events/${id}/reject`, { reason });
  },

  listPayouts(limit = 50) {
    return api.get<AdminPayout[]>(`/api/admin/payouts?limit=${limit}`);
  },

  // Creates the next payout attempt for an event, reusing the organizer's
  // saved account. Only valid when the current attempt failed.
  retryPayout(eventID: string) {
    return api.post<void>(`/api/admin/events/${eventID}/payout/retry`);
  },
};

// AdminPayout is one row of the admin payout queue: the newest attempt of an
// event.
export interface AdminPayout {
  event_id: string;
  event_title: string;
  attempt: number;
  organizer_id: string;
  bank_code: string;
  bank_account_number: string;
  bank_account_holder_name: string;
  status: "none" | "pending" | "failed" | "paid_out";
  amount_rupiah: number;
  // What a retry would actually send. Zero means there is nothing left to pay,
  // so Retry is disabled (the backend rejects it too).
  collectable_rupiah: number;
  provider_ref?: string;
  failure_reason?: string;
  requested_at: string;
  completed_at?: string;
}

export interface AdminRefund {
  id: string;
  booking_id: string;
  event_id: string;
  customer_email: string;
  amount_rupiah: number;
  currency: string;
  // Same four states customers see, but the admin table shows the raw value.
  status: RefundStatus;
  reason: string;
  provider_ref?: string;
  failure_reason?: string;
  // The destination itself is not in the admin payload — only whether the
  // customer registered one. Retry is impossible without it.
  has_destination: boolean;
  created_at: string;
  processed_at?: string;
}

export const adminRefundApi = {
  list(status = "", limit = 50) {
    const qs = new URLSearchParams({ limit: String(limit) });
    if (status) qs.set("status", status);
    return api.get<AdminRefund[]>(`/api/admin/refunds?${qs.toString()}`);
  },
  retry(id: string) {
    return api.post<void>(`/api/admin/refunds/${id}/retry`);
  },
};

import { api } from "./api-client";
import type { EventItem } from "@/types";

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

  approveEvent(id: string) {
    return api.post<void>(`/api/events/${id}/approve`);
  },

  rejectEvent(id: string, reason: string) {
    return api.post<void>(`/api/events/${id}/reject`, { reason });
  },
};

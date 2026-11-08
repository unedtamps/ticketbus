export interface User {
  id: string;
  email: string;
  name: string;
  role: "admin" | "eo" | "customer";
}

export interface AuthResponse {
  user?: User;
  access_token: string;
  expires_in: number;
  refresh_token?: string;
}

export interface EventItem {
  id: string;
  reservations_open?: boolean;
  organizer_id: string;
  title: string;
  description: string;
  image_url?: string;
  venue_name: string;
  venue_address: string;
  venue_capacity: number;
  start_at: string;
  end_at: string;
  status: string;
  created_at: string;
  updated_at: string;
  // Organizer-scoped views (GET /api/events/mine) only.
  payout_status?: "none" | "pending" | "paid_out" | "failed";
  payout_amount_rupiah?: number;
  payout_requested_at?: string;
  payout_completed_at?: string;
  // Confirmed booking revenue. Still collectable = this minus payout_amount_rupiah.
  confirmed_revenue_rupiah?: number;
}

export interface PayoutAccount {
  bank_code: string;
  bank_account_number: string;
  bank_account_holder_name: string;
  created_at?: string;
  updated_at?: string;
}

export interface EventPayoutAttempt {
  event_id: string;
  attempt: number;
  status: "pending" | "paid_out" | "failed";
  amount_rupiah: number;
  bank_code?: string;
  bank_account_number?: string;
  bank_account_holder_name?: string;
  provider_ref?: string;
  failure_reason?: string;
  requested_at: string;
  completed_at?: string;
}

export interface EventPayout {
  event_id: string;
  status: "none" | "pending" | "paid_out" | "failed";
  // amount_rupiah is the cumulative amount paid out across all attempts.
  amount_rupiah: number;
  attempts: number;
  attempts_list?: EventPayoutAttempt[];
  provider_ref?: string;
  failure_reason?: string;
  requested_at?: string;
  completed_at?: string;
}

export interface TicketType {
  id: string;
  name: string;
  price_rupiah: number;
  quantity: number;
  available: number;
  max_per_order: number;
}

export interface EventDetail {
  event: EventItem;
  ticket_types: TicketType[];
}

export interface EventListResponse {
  events: EventItem[];
  total: number;
}

// Aggregate over ALL of an organizer's events, not one page. The dashboard
// header reads these so paging the table cannot change the figures.
export interface OrganizerSummary {
  available_to_collect: number;
  confirmed_total: number;
  paid_out_total: number;
  collectable_events: number;
}

// One page of GET /api/events/mine. next_cursor is opaque and empty on the
// last page.
export interface OrganizerEventListResponse {
  events: EventItem[];
  next_cursor: string;
  summary: OrganizerSummary;
}

export interface ReservationResponse {
  booking_id: string;
  event_id: string;
  total_rupiah: number;
  status: string;
  expires_at: string;
}

export interface BookingItemResp {
  id: string;
  ticket_type_id: string;
  quantity: number;
  unit_price_rupiah: number;
  total_price: number;
  ticket_type_name?: string;
}

export interface BookingResponse {
  id: string;
  user_id: string;
  event_id: string;
  status: string;
  expires_at?: string;
  /** Last moment a payment can be started: expires_at minus the gateway's
   *  minimum session lifetime. The backend enforces the same boundary. */
  payment_deadline?: string;
  total_rupiah: number;
  payment_id: string;
  items: BookingItemResp[];
  created_at: string;
  // Event context, present on customer-facing lists only.
  event_title?: string;
  event_start_at?: string;
  event_venue?: string;
}

export interface TransactionResponse {
  id: string;
  booking_id: string;
  amount_rupiah: number;
  currency: string;
  status: string;
  refund_status?: string;
  payment_link_url?: string;
  expires_at?: string;
  created_at: string;
}

// The only four refund states a customer ever sees. Gateway-internal statuses
// are collapsed into these by the payment service.
export type RefundStatus = "pending" | "processing" | "succeeded" | "failed";

// One row of GET /api/refunds — the customer's own refund ledger. Payment owns
// this state; there is no mirrored copy in ticketing. Deliberately carries no
// provider_ref or raw failure code: those are gateway internals.
export interface CustomerRefund {
  id: string;
  booking_id: string;
  event_id: string;
  amount_rupiah: number;
  currency: string;
  status: RefundStatus;
  failure_reason?: string;
  channel_code?: string;
  account_number?: string;
  has_account: boolean;
  created_at: string;
  processed_at?: string;
}

export interface PaymentStatusResponse {
  transaction_id: string;
  status: string;
  payment_link_url?: string;
  amount_rupiah: number;
  currency: string;
}

export interface InitiatePaymentResponse {
  transaction_id: string;
  payment_session_id: string;
  payment_link_url: string;
  amount_rupiah: number;
  currency: string;
  status: string;
  expires_at: string;
}

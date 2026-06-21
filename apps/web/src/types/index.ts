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
  organizer_id: string;
  title: string;
  description: string;
  venue_name: string;
  venue_address: string;
  venue_capacity: number;
  start_at: string;
  end_at: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface TicketType {
  id: string;
  name: string;
  price_cents: number;
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

export interface ReservationResponse {
  booking_id: string;
  event_id: string;
  total_cents: number;
  status: string;
  expires_at: string;
}

export interface TransactionResponse {
  id: string;
  booking_id: string;
  amount_cents: number;
  currency: string;
  status: string;
  created_at: string;
}

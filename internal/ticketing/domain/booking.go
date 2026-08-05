package domain

import "time"

// Booking is the durable order lifecycle for a reservation.
type Booking struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	EventID   string     `json:"event_id"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Deprecated compatibility fields. The target schema derives totals from items
	// and keeps payment state in the payment service.
	TotalCents   int           `json:"total_cents"`
	PaymentID    string        `json:"payment_id"`
	RefundStatus string        `json:"refund_status,omitempty"`
	Items        []BookingItem `json:"items"`
	CreatedAt    time.Time     `json:"created_at"`
}

// BookingItem is a line item within a booking.
type BookingItem struct {
	ID             string `json:"id"`
	BookingID      string `json:"booking_id"`
	TicketTypeID   string `json:"ticket_type_id"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int    `json:"unit_price_cents"`
	TotalPrice     int    `json:"total_price"`
}

// Reservation is a temporary hold in Redis.
type Reservation struct {
	BookingID  string        `json:"booking_id"`
	UserID     string        `json:"user_id"`
	EventID    string        `json:"event_id"`
	Items      []BookingItem `json:"items"`
	TotalCents int           `json:"total_cents"`
	Status     string        `json:"status"`
	ExpiresAt  time.Time     `json:"expires_at"`
	CreatedAt  time.Time     `json:"created_at"`
}

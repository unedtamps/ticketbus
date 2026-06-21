package domain

import "time"

// Booking is a confirmed order.
type Booking struct {
	ID          string        `json:"id"`
	UserID      string        `json:"user_id"`
	EventID     string        `json:"event_id"`
	Status      string        `json:"status"`
	TotalCents  int           `json:"total_cents"`
	PaymentID   string        `json:"payment_id"`
	Items       []BookingItem `json:"items"`
	CreatedAt   time.Time     `json:"created_at"`
}

// BookingItem is a line item within a booking.
type BookingItem struct {
	ID             string `json:"id"`
	BookingID      string `json:"booking_id"`
	TicketTypeID   string `json:"ticket_type_id"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int    `json:"unit_price_cents"`
}

// Reservation is a temporary hold in Redis.
type Reservation struct {
	BookingID  string        `json:"booking_id"`
	UserID     string        `json:"user_id"`
	EventID    string        `json:"event_id"`
	Items      []BookingItem `json:"items"`
	TotalCents int           `json:"total_cents"`
	Status     string        `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
}

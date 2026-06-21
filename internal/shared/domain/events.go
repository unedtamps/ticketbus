package domain

import "time"

// Shared Kafka event payloads (cross-service message schemas).
type EventCreated struct {
	EventID string    `json:"event_id"`
	Status  string    `json:"status"`
	At      time.Time `json:"at"`
}

type EventApproved struct {
	EventID     string           `json:"event_id"`
	TicketTypes []TicketTypeInfo `json:"ticket_types"`
	At          time.Time        `json:"at"`
}

type TicketTypeInfo struct {
	TicketTypeID string `json:"ticket_type_id"`
	Name         string `json:"name"`
	Quantity     int    `json:"quantity"`
	PriceCents   int    `json:"price_cents"`
}

type EventRejected struct {
	EventID string    `json:"event_id"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

type EventUpdated struct {
	EventID string    `json:"event_id"`
	At      time.Time `json:"at"`
}

type EventCancelled struct {
	EventID string    `json:"event_id"`
	At      time.Time `json:"at"`
}

type BookingItem struct {
	TicketTypeID  string `json:"ticket_type_id"`
	Quantity      int    `json:"quantity"`
	UnitPriceCents int   `json:"unit_price_cents"`
}

type ReservationCreated struct {
	BookingID  string        `json:"booking_id"`
	UserID     string        `json:"user_id"`
	EventID    string        `json:"event_id"`
	Items      []BookingItem `json:"items"`
	TotalCents int           `json:"total_cents"`
	At         time.Time     `json:"at"`
}

type ReservationExpired struct {
	BookingID string    `json:"booking_id"`
	EventID   string    `json:"event_id"`
	At        time.Time `json:"at"`
}

type PaymentInitiated struct {
	TransactionID string    `json:"transaction_id"`
	BookingID     string    `json:"booking_id"`
	UserID        string    `json:"user_id"`
	AmountCents   int       `json:"amount_cents"`
	At            time.Time `json:"at"`
}

type PaymentCompleted struct {
	TransactionID string    `json:"transaction_id"`
	BookingID     string    `json:"booking_id"`
	UserID        string    `json:"user_id"`
	At            time.Time `json:"at"`
}

type PaymentFailed struct {
	TransactionID string    `json:"transaction_id"`
	BookingID     string    `json:"booking_id"`
	UserID        string    `json:"user_id"`
	Reason        string    `json:"reason"`
	At            time.Time `json:"at"`
}

type TicketIssued struct {
	TicketID     string `json:"ticket_id"`
	BookingID    string `json:"booking_id"`
	UserID       string `json:"user_id"`
	EventID      string `json:"event_id"`
	TicketTypeID string `json:"ticket_type_id"`
}

type OrganizerCreated struct {
	UserID       string    `json:"user_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	ProfileLink  string    `json:"profile_link"`
	ContactEmail string    `json:"contact_email"`
	At           time.Time `json:"at"`
}

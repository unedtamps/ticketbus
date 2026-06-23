package domain

import "context"

// BookingRepository defines the persistence contract for bookings.
type BookingRepository interface {
	Create(ctx context.Context, booking *Booking) error
	FindByID(ctx context.Context, id string) (*Booking, error)
	ListByUser(ctx context.Context, userID string) ([]Booking, error)
	UpdateStatus(ctx context.Context, bookingID, status string) error
}

// ReservationCache defines the contract for temporary reservation storage (Redis).
type ReservationCache interface {
	Save(ctx context.Context, res *Reservation, ttlSeconds int) error
	Find(ctx context.Context, bookingID string) (*Reservation, error)
	Delete(ctx context.Context, bookingID string) error
	SubscribeExpiry(ctx context.Context) (<-chan *Reservation, error)
}

// SeatCounter defines the contract for atomic seat capacity tracking (Redis).
type SeatCounter interface {
	Init(ctx context.Context, eventID, ticketTypeID string, total int) error
	Reserve(ctx context.Context, eventID, ticketTypeID string, qty int) error
	Release(ctx context.Context, eventID, ticketTypeID string, qty int) error
	Available(ctx context.Context, eventID, ticketTypeID string) (int, error)
}

// EventConsumer defines the contract for consuming events from other services.
type EventConsumer interface {
	OnPaymentCompleted(ctx context.Context, fn func(ctx context.Context, bookingID, transactionID string) error)
	OnPaymentFailed(ctx context.Context, fn func(ctx context.Context, bookingID string) error)
	OnEventApproved(ctx context.Context, fn func(ctx context.Context, eventID string, ticketTypes []TicketTypeInfo) error)
	OnEventCancelled(ctx context.Context, fn func(ctx context.Context, eventID string) error)
	Start(ctx context.Context) error
	Close() error
}

// TicketTypeInfo carries event-approved ticket type data from Kafka.
type TicketTypeInfo struct {
	TicketTypeID string `json:"ticket_type_id"`
	Name         string `json:"name"`
	Quantity     int    `json:"quantity"`
	PriceCents   int    `json:"price_cents"`
}

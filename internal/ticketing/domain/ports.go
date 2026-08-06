package domain

import (
	"context"
	"time"
)

// EventRepository defines the persistence contract for events and ticket types.
type EventRepository interface {
	CreateEvent(ctx context.Context, event *Event) error
	UpdateEvent(ctx context.Context, event *Event) error
	FindEventByID(ctx context.Context, id string) (*Event, error)
	ListPublished(ctx context.Context, limit, offset int) ([]Event, int, error)
	ListByOrganizer(ctx context.Context, organizerID string) ([]Event, error)
	ListPending(ctx context.Context, limit, offset int) ([]Event, int, error)
	FindAll(ctx context.Context, status string, limit, offset int) ([]Event, int, error)

	// Ticket types
	CreateTicketTypes(ctx context.Context, types []TicketType) error
	ListTicketTypesByEvent(ctx context.Context, eventID string) ([]TicketType, error)
}

// SeatReader reads live seat availability from Redis.
type SeatReader interface {
	Available(ctx context.Context, eventID, ticketTypeID string) int
}

// SeatInitializer creates the Redis availability state for a published event.
type SeatInitializer interface {
	Init(ctx context.Context, eventID, ticketTypeID string, total int) error
	SetPrice(ctx context.Context, eventID, ticketTypeID string, price int) error
}

// BookingRepository defines the persistence contract for bookings.
type BookingRepository interface {
	Create(ctx context.Context, booking *Booking) error
	FindByID(ctx context.Context, id string) (*Booking, error)
	ListByUser(ctx context.Context, userID string) ([]Booking, error)
	ListByEventID(ctx context.Context, eventID string) ([]Booking, error)
	UpdateStatusIfPending(ctx context.Context, bookingID, status string) (bool, error)
	// TransitionAndReleaseSeats atomically transitions a booking (guarded by its
	// current status) and releases its seats in a single transaction.
	TransitionAndReleaseSeats(ctx context.Context, booking *Booking, toStatus string) (bool, error)
	// Delete removes a pending booking (used to roll back a reserve when the
	// payment transaction could not be created).
	Delete(ctx context.Context, bookingID string) error
}

// PaymentClient creates the payment transaction for a booking. Called
// synchronously during reserve so the transaction always exists before any
// gateway activity.
type PaymentClient interface {
	CreateTxnForBooking(ctx context.Context, bookingID, eventID, userID, email string, amountCents int, expiresAt time.Time) error
}

// SeatCounter defines the contract for atomic seat capacity tracking (Redis).
type SeatCounter interface {
	Init(ctx context.Context, eventID, ticketTypeID string, total int) error
	Reserve(ctx context.Context, eventID, ticketTypeID string, qty int) error
	Release(ctx context.Context, eventID, ticketTypeID string, qty int) error
	Available(ctx context.Context, eventID, ticketTypeID string) (int, error)
	SetPrice(ctx context.Context, eventID, ticketTypeID string, price int) error
	GetPrice(ctx context.Context, eventID, ticketTypeID string) (int, error)
}

// EventConsumer defines the contract for consuming events from other services.
type EventConsumer interface {
	OnPaymentCompleted(ctx context.Context, fn func(ctx context.Context, bookingID, transactionID string) error)
	OnPaymentExpired(ctx context.Context, fn func(ctx context.Context, bookingID string) error)
	OnEventCancelled(ctx context.Context, fn func(ctx context.Context, eventID string) error)
	Start(ctx context.Context) error
	Close() error
}

// EventStatusRepository provides event status for guard checks.
type EventStatusRepository interface {
	IsPublished(ctx context.Context, eventID string) (bool, error)
}

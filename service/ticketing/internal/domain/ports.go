package domain

import (
	"context"
	"io"
	"time"

	"github.com/unedtamps/ticketbus/pkg/dto"
)

// EventRepository defines the persistence contract for events and ticket types.
type EventRepository interface {
	CreateEvent(ctx context.Context, event *Event) error
	UpdateEvent(ctx context.Context, event *Event) error
	// CancelAndPublish marks a published event cancelled and writes the
	// event.cancelled outbox row in one database transaction — the row drives
	// the whole cancellation cascade, so it must not be separable from the
	// status change. Reports whether the update was applied.
	CancelAndPublish(ctx context.Context, eventID string, ev OutboxEvent) (bool, error)
	// ListBookerEmails returns one entry per booking of the event whose status
	// is in statuses and whose email was snapshotted — the recipient list for
	// the cancellation notification.
	ListBookerEmails(ctx context.Context, eventID string, statuses []string) ([]dto.EventCancelledBooker, error)
	FindEventByID(ctx context.Context, id string) (*Event, error)
	ListPublished(ctx context.Context, limit, offset int) ([]Event, int, error)
	ListByOrganizer(ctx context.Context, organizerID string, limit int, cursor *Cursor) ([]Event, error)
	OrganizerSummary(ctx context.Context, organizerID string) (*OrganizerSummary, error)
	ListPending(ctx context.Context, limit, offset int) ([]Event, int, error)
	FindAll(ctx context.Context, status string, limit, offset int) ([]Event, int, error)

	// Ticket types
	CreateTicketTypes(ctx context.Context, types []TicketType) error
	ListTicketTypesByEvent(ctx context.Context, eventID string) ([]TicketType, error)

	// Payouts
	UpsertPayoutAccount(ctx context.Context, acct *OrganizerPayoutAccount) error
	FindPayoutAccount(ctx context.Context, organizerID string) (*OrganizerPayoutAccount, error)
	// RequestPayout inserts the next payout attempt for an event and its
	// outbox event in one transaction. The attempt number is allocated inside
	// the transaction, so the payload is built by the given function once the
	// number is known. It returns false when an attempt is already in flight
	// (the organizer must wait) or when the attempt number collides with a
	// concurrent request.
	RequestPayout(
		ctx context.Context,
		attempt *EventPayoutAttempt,
		payload func(attempt int) any,
	) (bool, error)
	// ListPayoutAttempts returns every payout attempt of an event, oldest
	// attempt first.
	ListPayoutAttempts(ctx context.Context, eventID string) ([]EventPayoutAttempt, error)
	// ListPayoutAttemptsForAdmin returns the newest attempt of every event that
	// has one, newest first — the admin payout queue.
	ListPayoutAttemptsForAdmin(ctx context.Context, limit int) ([]AdminPayoutRow, error)
	// PayoutSummary aggregates the attempts of an event into its payout state.
	PayoutSummary(ctx context.Context, eventID string) (*EventPayoutSummary, error)
	// CollectableRupiah returns what a new payout attempt would actually send:
	// confirmed booking revenue minus everything already paid out. Zero means
	// a retry has nothing to pay.
	CollectableRupiah(ctx context.Context, eventID string) int64
	// SetPayoutResult applies the gateway result to one attempt while it is
	// still pending. Returns false for duplicate deliveries.
	SetPayoutResult(ctx context.Context, eventID string, attempt int, status dto.PayoutStatus, amountRupiah int64, providerRef, reason string) (bool, error)

	// SetEventImage records the object key of the event's photo.
	SetEventImage(ctx context.Context, eventID, imageKey string) error
}

// ImageStorage persists event images in object storage and resolves their
// public URLs.
type ImageStorage interface {
	Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	PublicURL(key string) string
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

// OutboxEvent is a domain event written to the outbox table in the same
// transaction as the business write that produced it.
type OutboxEvent struct {
	Topic   string
	Key     string
	Payload any
}

// BookingRepository defines the persistence contract for bookings.
type BookingRepository interface {
	// CreateReservation atomically decrements seat counters, inserts the
	// booking with its items, and writes the outbox event in one transaction.
	// Either all three land or none do.
	CreateReservation(ctx context.Context, booking *Booking, ev OutboxEvent) error
	FindByID(ctx context.Context, id string) (*Booking, error)
	ListByUser(ctx context.Context, userID string) ([]Booking, error)
	ListByEventID(ctx context.Context, eventID string) ([]Booking, error)
	UpdateStatusIfPending(ctx context.Context, bookingID, status string) (bool, error)
	// ConfirmWithTickets transitions a pending booking to confirmed and writes
	// one ticket.issued outbox row per item in one database transaction, so a
	// confirmed booking can never lack its tickets. Reports whether the
	// transition was applied.
	ConfirmWithTickets(ctx context.Context, bookingID string, events []OutboxEvent) (bool, error)
	// TransitionAndReleaseSeats atomically transitions a booking (guarded by its
	// current status) and releases its seats in a single transaction.
	TransitionAndReleaseSeats(ctx context.Context, booking *Booking, toStatus string) (bool, error)
}

// PaymentClient creates the payment transaction for a booking by calling the
// payment service's internal REST API. Used by the outbox REST dispatcher,
// which runs in the background after a reservation commits — never on the
// request path.
type PaymentClient interface {
	InitiateTxnForBooking(ctx context.Context, bookingID, eventID, userID, email string, amountRupiah int, expiresAt time.Time) error
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
	OnPayoutCompleted(ctx context.Context, fn func(ctx context.Context, evt dto.PayoutCompleted) error)
	Start(ctx context.Context) error
	Close() error
}

// EventStatusRepository provides event status for guard checks.
type EventStatusRepository interface {
	IsPublished(ctx context.Context, eventID string) (bool, error)
	// EventWindow returns the event status and its reservation window. It is
	// used to reject reservations before the event starts, after it ends, and
	// too close to the end to complete a payment.
	EventWindow(ctx context.Context, eventID string) (status string, startAt, endAt time.Time, err error)
}

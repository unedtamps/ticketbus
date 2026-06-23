package fixtures

import (
	"time"

	"github.com/google/uuid"
	"github.com/nedo/TicketSaas/internal/inventory/domain"
)

// BookingOption is a functional option for NewTestBooking.
type BookingOption func(*domain.Booking)

// WithBookingID overrides the default booking ID.
func WithBookingID(id string) BookingOption {
	return func(b *domain.Booking) { b.ID = id }
}

// WithBookingUserID overrides the user ID.
func WithBookingUserID(userID string) BookingOption {
	return func(b *domain.Booking) { b.UserID = userID }
}

// WithBookingEventID overrides the event ID.
func WithBookingEventID(eventID string) BookingOption {
	return func(b *domain.Booking) { b.EventID = eventID }
}

// WithBookingStatus overrides the status (default: confirmed).
func WithBookingStatus(status string) BookingOption {
	return func(b *domain.Booking) { b.Status = status }
}

// WithBookingPaymentID overrides the payment ID.
func WithBookingPaymentID(paymentID string) BookingOption {
	return func(b *domain.Booking) { b.PaymentID = paymentID }
}

// WithBookingItems overrides the items slice.
func WithBookingItems(items []domain.BookingItem) BookingOption {
	return func(b *domain.Booking) { b.Items = items }
}

// NewTestBooking creates a Booking with sensible defaults.
func NewTestBooking(opts ...BookingOption) *domain.Booking {
	now := time.Now().Truncate(time.Second)
	b := &domain.Booking{
		ID:         uuid.NewString(),
		UserID:     uuid.NewString(),
		EventID:    uuid.NewString(),
		Status:     "confirmed",
		TotalCents: 10000,
		PaymentID:  uuid.NewString(),
		Items: []domain.BookingItem{
			*NewTestBookingItem(WithBookingItemID(uuid.NewString()), WithBookingItemBookingID(uuid.NewString())),
		},
		CreatedAt: now,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// BookingItemOption is a functional option for NewTestBookingItem.
type BookingItemOption func(*domain.BookingItem)

// WithBookingItemID overrides the item ID.
func WithBookingItemID(id string) BookingItemOption {
	return func(bi *domain.BookingItem) { bi.ID = id }
}

// WithBookingItemBookingID overrides the booking ID.
func WithBookingItemBookingID(bookingID string) BookingItemOption {
	return func(bi *domain.BookingItem) { bi.BookingID = bookingID }
}

// WithBookingItemTicketTypeID overrides the ticket type ID.
func WithBookingItemTicketTypeID(ttID string) BookingItemOption {
	return func(bi *domain.BookingItem) { bi.TicketTypeID = ttID }
}

// WithBookingItemQuantity overrides the quantity.
func WithBookingItemQuantity(qty int) BookingItemOption {
	return func(bi *domain.BookingItem) { bi.Quantity = qty }
}

// WithBookingItemUnitPrice overrides the unit price.
func WithBookingItemUnitPrice(cents int) BookingItemOption {
	return func(bi *domain.BookingItem) { bi.UnitPriceCents = cents }
}

// NewTestBookingItem creates a BookingItem with sensible defaults.
func NewTestBookingItem(opts ...BookingItemOption) *domain.BookingItem {
	bi := &domain.BookingItem{
		ID:             uuid.NewString(),
		BookingID:      uuid.NewString(),
		TicketTypeID:   uuid.NewString(),
		Quantity:       1,
		UnitPriceCents: 5000,
	}
	for _, o := range opts {
		o(bi)
	}
	return bi
}

// ReservationOption is a functional option for NewTestReservation.
type ReservationOption func(*domain.Reservation)

// WithReservationBookingID overrides the booking ID.
func WithReservationBookingID(id string) ReservationOption {
	return func(r *domain.Reservation) { r.BookingID = id }
}

// WithReservationUserID overrides the user ID.
func WithReservationUserID(userID string) ReservationOption {
	return func(r *domain.Reservation) { r.UserID = userID }
}

// WithReservationEventID overrides the event ID.
func WithReservationEventID(eventID string) ReservationOption {
	return func(r *domain.Reservation) { r.EventID = eventID }
}

// WithReservationItems overrides the items.
func WithReservationItems(items []domain.BookingItem) ReservationOption {
	return func(r *domain.Reservation) { r.Items = items }
}

// WithReservationTotalCents overrides the total.
func WithReservationTotalCents(cents int) ReservationOption {
	return func(r *domain.Reservation) { r.TotalCents = cents }
}

// WithReservationStatus overrides the status (default: held).
func WithReservationStatus(status string) ReservationOption {
	return func(r *domain.Reservation) { r.Status = status }
}

// NewTestReservation creates a Reservation with sensible defaults.
func NewTestReservation(opts ...ReservationOption) *domain.Reservation {
	now := time.Now().Truncate(time.Second)
	r := &domain.Reservation{
		BookingID:  uuid.NewString(),
		UserID:     uuid.NewString(),
		EventID:    uuid.NewString(),
		Items: []domain.BookingItem{
			*NewTestBookingItem(WithBookingItemUnitPrice(5000), WithBookingItemQuantity(2)),
		},
		TotalCents: 10000,
		Status:     "held",
		CreatedAt:  now,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

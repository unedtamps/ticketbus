package booking_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sdto "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/pkg/outbox"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/application/booking"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain/mocks"
	fixtures "github.com/unedtamps/ticketbus/service/ticketing/internal/testfixtures"
)

// windowService builds a booking service whose event window is the given one.
func windowService(
	t *testing.T,
	status string,
	startAt, endAt time.Time,
) (*bookingServiceUnderTest, *mocks.MockEventStatusRepository) {
	t.Helper()
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	eventStatus := mocks.NewMockEventStatusRepository(t)
	eventStatus.EXPECT().EventWindow(mock.Anything, mock.Anything).
		Return(status, startAt, endAt, nil).Maybe()
	svc := booking.NewBookingService(
		bookingRepo, seatCounter, consumer, eventStatus,
		outbox.NoopStore{}, invLogger, 300,
	)
	return &bookingServiceUnderTest{svc: svc, seatCounter: seatCounter}, eventStatus
}

type bookingServiceUnderTest struct {
	svc         *booking.BookingService
	seatCounter *mocks.MockSeatCounter
}

func TestReserve_EventNotPublished(t *testing.T) {
	now := time.Now()
	svc, _ := windowService(t, string(sdto.EventStatusCancelled), now.Add(-time.Hour), now.Add(time.Hour))

	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(1), fixtures.WithBookingItemUnitPrice(10000)),
	}
	_, err := svc.svc.Reserve(context.Background(), "user-1", "user@example.com", "event-1", items)
	assert.ErrorIs(t, err, domain.ErrEventNotActive)
}

func TestReserve_EventNotStarted(t *testing.T) {
	now := time.Now()
	svc, _ := windowService(t, string(sdto.EventStatusPublished), now.Add(time.Hour), now.Add(2*time.Hour))

	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(1), fixtures.WithBookingItemUnitPrice(10000)),
	}
	_, err := svc.svc.Reserve(context.Background(), "user-1", "user@example.com", "event-1", items)
	assert.ErrorIs(t, err, domain.ErrEventNotStarted)
}

func TestReserve_EventEnded(t *testing.T) {
	now := time.Now()
	svc, _ := windowService(t, string(sdto.EventStatusPublished), now.Add(-2*time.Hour), now.Add(-time.Hour))

	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(1), fixtures.WithBookingItemUnitPrice(10000)),
	}
	_, err := svc.svc.Reserve(context.Background(), "user-1", "user@example.com", "event-1", items)
	assert.ErrorIs(t, err, domain.ErrEventEnded)
}

// TestReserve_HoldOutlivesEvent covers a reservation made while the event is
// still live: the hold must not be clamped to the event's end, so the customer
// can still pay after the event ended.
func TestReserve_HoldOutlivesEvent(t *testing.T) {
	now := time.Now()
	endAt := now.Add(time.Minute) // the event ends in a minute
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	eventStatus := mocks.NewMockEventStatusRepository(t)
	eventStatus.EXPECT().EventWindow(mock.Anything, mock.Anything).
		Return(string(sdto.EventStatusPublished), now.Add(-time.Hour), endAt, nil)

	ctx := context.Background()
	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(1), fixtures.WithBookingItemUnitPrice(10000)),
	}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	bookingRepo.EXPECT().CreateReservation(ctx, mock.Anything, mock.Anything).Return(nil)

	svc := booking.NewBookingService(
		bookingRepo, seatCounter, consumer, eventStatus,
		outbox.NoopStore{}, invLogger, 300,
	)
	res, err := svc.Reserve(ctx, "user-1", "user@example.com", "event-1", items)
	require.NoError(t, err, "a reservation one minute before the end must be allowed")
	assert.True(t, res.ExpiresAt.After(endAt),
		"the hold must not be clamped to the event end: %s vs %s",
		res.ExpiresAt, endAt)
}

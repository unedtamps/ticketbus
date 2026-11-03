package booking_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

var invLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

// reservableWindow is the default event window in tests: published, started
// an hour ago and ending in a day, so reservations are allowed.
func reservableWindow() (string, time.Time, time.Time) {
	return string(sdto.EventStatusPublished),
		time.Now().Add(-time.Hour),
		time.Now().Add(24 * time.Hour)
}

func newBookingService(
	t testing.TB,
	bookingRepo domain.BookingRepository,
	seatCounter domain.SeatCounter,
	consumer domain.EventConsumer,
	ob outbox.StoreInterface,
) (*booking.BookingService, *mocks.MockEventStatusRepository) {
	t.Helper()
	eventStatus := mocks.NewMockEventStatusRepository(t)
	// Default: the event is open for reservations. Tests that exercise the
	// window override this expectation.
	status, startAt, endAt := reservableWindow()
	eventStatus.EXPECT().EventWindow(mock.Anything, mock.Anything).
		Return(status, startAt, endAt, nil).Maybe()
	return booking.NewBookingService(bookingRepo, seatCounter, consumer, eventStatus, ob, invLogger, 300), eventStatus
}

func newBookingServiceDefaults(
	t testing.TB,
	bookingRepo domain.BookingRepository,
	seatCounter domain.SeatCounter,
	consumer domain.EventConsumer,
) (*booking.BookingService, *mocks.MockEventStatusRepository) {
	t.Helper()
	return newBookingService(t, bookingRepo, seatCounter, consumer, outbox.NoopStore{})
}

func reserveArgs(items []domain.BookingItem) (userID, email, eventID string, itemsOut []domain.BookingItem) {
	return "user-1", "user@example.com", "event-1", items
}

// ── Reserve ──────────────────────────────────────────────────────────────────

func TestReserve_Success_SingleItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000))}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	bookingRepo.EXPECT().CreateReservation(ctx, mock.MatchedBy(func(b *domain.Booking) bool {
		return b.Status == "pending" && b.ExpiresAt != nil && len(b.Items) == 1
	}), mock.MatchedBy(func(ev domain.OutboxEvent) bool {
		if ev.Topic != sdto.TopicPaymentInitiate {
			return false
		}
		req, ok := ev.Payload.(sdto.PaymentInitiateRequested)
		return ok && req.AmountRupiah == 20000 &&
			req.Email == "user@example.com" &&
			req.UserID == "user-1" && req.EventID == "event-1" &&
			!req.ExpiresAt.IsZero()
	})).Return(nil)
	svc, _ := newBookingService(t, bookingRepo, seatCounter, consumer, outbox.NoopStore{})
	userID, email, eventID, itemsOut := reserveArgs(items)
	res, err := svc.Reserve(ctx, userID, email, eventID, itemsOut)
	require.NoError(t, err)
	assert.Equal(t, "pending", res.Status)
	assert.Equal(t, 20000, res.TotalRupiah)
	assert.False(t, res.ExpiresAt.IsZero())
	assert.NotEmpty(t, res.BookingID)
}

func TestReserve_Success_MultiItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("reg"), fixtures.WithBookingItemQuantity(1), fixtures.WithBookingItemUnitPrice(5000)),
	}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "reg").Return(5000, nil)
	bookingRepo.EXPECT().CreateReservation(ctx, mock.Anything, mock.MatchedBy(func(ev domain.OutboxEvent) bool {
		req, ok := ev.Payload.(sdto.PaymentInitiateRequested)
		return ok && req.AmountRupiah == 25000 && req.Email == "user@example.com"
	})).Return(nil)
	svc, _ := newBookingService(t, bookingRepo, seatCounter, consumer, outbox.NoopStore{})
	userID, email, eventID, itemsOut := reserveArgs(items)
	res, err := svc.Reserve(ctx, userID, email, eventID, itemsOut)
	require.NoError(t, err)
	assert.Equal(t, 25000, res.TotalRupiah)
	assert.Len(t, res.Items, 2)
}

func TestReserve_EmptyItems(t *testing.T) {
	svc, _ := newBookingServiceDefaults(t, mocks.NewMockBookingRepository(t),
		mocks.NewMockSeatCounter(t), mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	_, err := svc.Reserve(ctx, "user-1", "u@e.com", "event-1", nil)
	assert.ErrorIs(t, err, domain.ErrInvalidQuantity)
}

func TestReserve_NoSeats_Fails(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, seatCounter, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000))}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	// The seat decrement happens inside the reservation transaction, so a
	// seat failure aborts the whole thing: no booking, no outbox row, and no
	// manual seat compensation. Atomicity is asserted in the integration
	// suite; here CreateReservation is one mocked call.
	bookingRepo.EXPECT().CreateReservation(ctx, mock.Anything, mock.Anything).Return(domain.ErrNoSeatsAvailable)
	_, err := svc.Reserve(ctx, "user-1", "u@e.com", "event-1", items)
	assert.ErrorIs(t, err, domain.ErrNoSeatsAvailable)
}

func TestReserve_WrongPrice_Fails(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, seatCounter, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(1), fixtures.WithBookingItemUnitPrice(9999))}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	_, err := svc.Reserve(ctx, "user-1", "u@e.com", "event-1", items)
	assert.ErrorIs(t, err, domain.ErrPriceMismatch)
}

func TestReserve_ReservationTxError_Fails(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, seatCounter, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000))}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	// CreateReservation is atomic: if the outbox insert fails the whole
	// transaction (seats + booking + outbox) rolls back and Reserve errors.
	bookingRepo.EXPECT().CreateReservation(ctx, mock.Anything, mock.Anything).Return(errors.New("db error"))
	userID, email, eventID, itemsOut := reserveArgs(items)
	_, err := svc.Reserve(ctx, userID, email, eventID, itemsOut)
	assert.Error(t, err)
}

// ── Confirm ──────────────────────────────────────────────────────────────────

func TestConfirm_Success(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingFixture := fixtures.NewTestBooking(
		fixtures.WithBookingID("book-1"),
		fixtures.WithBookingEventID("event-1"),
	)
	bookingFixture.Status = "pending"
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(bookingFixture, nil)
	bookingRepo.EXPECT().ConfirmWithTickets(ctx, "book-1", mock.Anything).Return(true, nil)
	svc, es := newBookingServiceDefaults(t, bookingRepo, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(true, nil)
	err := svc.Confirm(ctx, "book-1", "txn-1")
	require.NoError(t, err)
}

func TestConfirm_WithItems(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingFixture := fixtures.NewTestBooking(
		fixtures.WithBookingID("book-1"),
		fixtures.WithBookingEventID("event-1"),
	)
	bookingFixture.Status = "pending"
	bookingFixture.Items = []domain.BookingItem{
		{ID: "item-1", TicketTypeID: "vip", Quantity: 1},
		{ID: "item-2", TicketTypeID: "reg", Quantity: 2},
	}
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(bookingFixture, nil)
	bookingRepo.EXPECT().ConfirmWithTickets(ctx, "book-1", mock.Anything).Return(true, nil)
	svc, es := newBookingServiceDefaults(t, bookingRepo, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(true, nil)
	err := svc.Confirm(ctx, "book-1", "txn-1")
	require.NoError(t, err)
}

func TestConfirm_SkipsNonPending(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, mocks.NewMockSeatCounter(t), mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	bookingFixture := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"))
	bookingFixture.Status = "expired"
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(bookingFixture, nil)
	err := svc.Confirm(ctx, "book-1", "txn-1")
	require.NoError(t, err)
}

func TestConfirm_UpdateFails(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingFixture := fixtures.NewTestBooking(
		fixtures.WithBookingID("book-1"),
		fixtures.WithBookingEventID("event-1"),
	)
	bookingFixture.Status = "pending"
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(bookingFixture, nil)
	bookingRepo.EXPECT().ConfirmWithTickets(ctx, "book-1", mock.Anything).Return(false, errors.New("db error"))
	svc, es := newBookingServiceDefaults(t, bookingRepo, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(true, nil)
	err := svc.Confirm(ctx, "book-1", "txn-1")
	require.Error(t, err)
}

func TestConfirm_EventCancelled(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingFixture := fixtures.NewTestBooking(
		fixtures.WithBookingID("book-1"),
		fixtures.WithBookingEventID("event-1"),
	)
	bookingFixture.Status = "pending"
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(bookingFixture, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.Anything, "cancelled").Return(true, nil)
	svc, es := newBookingServiceDefaults(t, bookingRepo, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(false, nil)
	err := svc.Confirm(ctx, "book-1", "txn-1")
	require.NoError(t, err)
}

func TestConfirm_StatusCheckError(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingFixture := fixtures.NewTestBooking(
		fixtures.WithBookingID("book-1"),
		fixtures.WithBookingEventID("event-1"),
	)
	bookingFixture.Status = "pending"
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(bookingFixture, nil)
	svc, es := newBookingServiceDefaults(t, bookingRepo, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(false, errors.New("db error"))
	err := svc.Confirm(ctx, "book-1", "txn-1")
	require.Error(t, err)
}

// ── CancelBookings ───────────────────────────────────────────────────────────

func TestCancelBookings_Success(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	pending := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"))
	pending.Status = "pending"
	confirmed := fixtures.NewTestBooking(fixtures.WithBookingID("book-2"))
	confirmed.Status = "confirmed"
	bookingRepo.EXPECT().ListByEventID(ctx, "event-1").Return([]domain.Booking{*pending, *confirmed}, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.Anything, "cancelled").Return(true, nil).Twice()
	svc, _ := newBookingServiceDefaults(t, bookingRepo, seatCounter, consumer)
	err := svc.CancelBookings(ctx, "event-1")
	require.NoError(t, err)
}

func TestCancelBookings_ListByEventIDError(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, mocks.NewMockSeatCounter(t), mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	bookingRepo.EXPECT().ListByEventID(ctx, "event-1").Return(nil, errors.New("db error"))
	err := svc.CancelBookings(ctx, "event-1")
	require.Error(t, err)
}

func TestCancelBookings_SkipsNonConfirmed(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	expired := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"))
	expired.Status = "expired"
	bookingRepo.EXPECT().ListByEventID(ctx, "event-1").Return([]domain.Booking{*expired}, nil)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, seatCounter, consumer)
	err := svc.CancelBookings(ctx, "event-1")
	require.NoError(t, err)
}

// ── ExpireOnPaymentExpired ───────────────────────────────────────────────────

func TestExpireOnPaymentExpired_SetsExpired(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingFixture := fixtures.NewTestBooking(
		fixtures.WithBookingID("book-1"),
		fixtures.WithBookingEventID("event-1"),
	)
	bookingFixture.Status = "pending"
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(bookingFixture, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.Anything, "expired").Return(true, nil)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, seatCounter, consumer)
	err := svc.ExpireOnPaymentExpired(ctx, "book-1")
	require.NoError(t, err)
}

// ── GetBooking / ListMyBookings ──────────────────────────────────────────────

func TestGetBooking_Found(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, mocks.NewMockSeatCounter(t), mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	bookingFixture := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(bookingFixture, nil)
	got, err := svc.GetBooking(ctx, "book-1")
	require.NoError(t, err)
	assert.Equal(t, "book-1", got.ID)
}

func TestGetBooking_NotFound(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, mocks.NewMockSeatCounter(t), mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(nil, pgx.ErrNoRows)
	_, err := svc.GetBooking(ctx, "book-1")
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestListMyBookings(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, mocks.NewMockSeatCounter(t), mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	b := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"))
	bookingRepo.EXPECT().ListByUser(ctx, "user-1").Return([]domain.Booking{*b}, nil)
	got, err := svc.ListMyBookings(ctx, "user-1")
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

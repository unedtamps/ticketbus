package booking_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nedo/TicketSaas/pkg/outbox"
	"github.com/nedo/TicketSaas/service/ticketing/internal/application/booking"
	"github.com/nedo/TicketSaas/service/ticketing/internal/domain"
	"github.com/nedo/TicketSaas/service/ticketing/internal/domain/mocks"
	fixtures "github.com/nedo/TicketSaas/service/ticketing/internal/testfixtures"
)

var invLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

func newBookingService(
	t testing.TB,
	bookingRepo domain.BookingRepository,
	paymentClient domain.PaymentClient,
	seatCounter domain.SeatCounter,
	consumer domain.EventConsumer,
) (*booking.BookingService, *mocks.MockEventStatusRepository) {
	t.Helper()
	eventStatus := mocks.NewMockEventStatusRepository(t)
	return booking.NewBookingService(bookingRepo, paymentClient, seatCounter, consumer, eventStatus, outbox.NoopStore{}, invLogger, 300), eventStatus
}

func newBookingServiceDefaults(
	t testing.TB,
	bookingRepo domain.BookingRepository,
	seatCounter domain.SeatCounter,
	consumer domain.EventConsumer,
) (*booking.BookingService, *mocks.MockEventStatusRepository) {
	t.Helper()
	return newBookingService(t, bookingRepo, mocks.NewMockPaymentClient(t), seatCounter, consumer)
}

func reserveArgs(items []domain.BookingItem) (userID, email, eventID string, itemsOut []domain.BookingItem) {
	return "user-1", "user@example.com", "event-1", items
}

// ── Reserve ──────────────────────────────────────────────────────────────────

func TestReserve_Success_SingleItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	paymentClient := mocks.NewMockPaymentClient(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000))}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	bookingRepo.EXPECT().Create(ctx, mock.MatchedBy(func(b *domain.Booking) bool {
		return b.Status == "pending" && b.ExpiresAt != nil && len(b.Items) == 1
	})).Return(nil)
	paymentClient.EXPECT().InitiateTxnForBooking(ctx, mock.AnythingOfType("string"), "event-1", "user-1", "user@example.com", 20000, mock.Anything).Return(nil)
	svc, _ := newBookingService(t, bookingRepo, paymentClient, seatCounter, consumer)
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
	paymentClient := mocks.NewMockPaymentClient(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("reg"), fixtures.WithBookingItemQuantity(1), fixtures.WithBookingItemUnitPrice(5000)),
	}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "reg").Return(5000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "reg", 1).Return(nil)
	bookingRepo.EXPECT().Create(ctx, mock.Anything).Return(nil)
	paymentClient.EXPECT().InitiateTxnForBooking(ctx, mock.Anything, "event-1", "user-1", "user@example.com", 25000, mock.Anything).Return(nil)
	svc, _ := newBookingService(t, bookingRepo, paymentClient, seatCounter, consumer)
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

func TestReserve_FirstItemNoSeats(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, seatCounter, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000))}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(errors.New("no seats"))
	_, err := svc.Reserve(ctx, "user-1", "u@e.com", "event-1", items)
	assert.ErrorIs(t, err, domain.ErrNoSeatsAvailable)
}

func TestReserve_SecondItemNoSeats_RollbackFirst(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	svc, _ := newBookingServiceDefaults(t, bookingRepo, seatCounter, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("reg"), fixtures.WithBookingItemQuantity(1), fixtures.WithBookingItemUnitPrice(5000)),
	}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "reg").Return(5000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "reg", 1).Return(errors.New("no seats"))
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	_, err := svc.Reserve(ctx, "user-1", "u@e.com", "event-1", items)
	assert.ErrorIs(t, err, domain.ErrNoSeatsAvailable)
}

func TestReserve_PaymentFails_RollbackAll(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	paymentClient := mocks.NewMockPaymentClient(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	svc, _ := newBookingService(t, bookingRepo, paymentClient, seatCounter, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000))}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	bookingRepo.EXPECT().Create(ctx, mock.Anything).Return(nil)
	bookingRepo.EXPECT().Delete(ctx, mock.AnythingOfType("string")).Return(nil)
	paymentClient.EXPECT().InitiateTxnForBooking(ctx, mock.Anything, "event-1", "user-1", "user@example.com", 20000, mock.Anything).Return(domain.ErrPaymentUnavailable)
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	userID, email, eventID, itemsOut := reserveArgs(items)
	_, err := svc.Reserve(ctx, userID, email, eventID, itemsOut)
	assert.ErrorIs(t, err, domain.ErrPaymentUnavailable)
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
	bookingRepo.EXPECT().UpdateStatusIfPending(ctx, "book-1", "confirmed").Return(true, nil)
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
	bookingRepo.EXPECT().UpdateStatusIfPending(ctx, "book-1", "confirmed").Return(true, nil)
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
	bookingRepo.EXPECT().UpdateStatusIfPending(ctx, "book-1", "confirmed").Return(false, errors.New("db error"))
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

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

	"github.com/nedo/TicketSaas/internal/shared/outbox"
	"github.com/nedo/TicketSaas/internal/ticketing/application/booking"
	"github.com/nedo/TicketSaas/internal/ticketing/domain"
	"github.com/nedo/TicketSaas/internal/ticketing/domain/mocks"
	"github.com/nedo/TicketSaas/tests/fixtures"
)

var invLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

func newBookingService(
	t testing.TB,
	bookingRepo domain.BookingRepository,
	cache domain.ReservationCache,
	seatCounter domain.SeatCounter,
	consumer domain.EventConsumer,
) (*booking.BookingService, *mocks.MockEventStatusRepository) {
	t.Helper()
	eventStatus := mocks.NewMockEventStatusRepository(t)
	return booking.NewBookingService(bookingRepo, cache, seatCounter, consumer, eventStatus, outbox.NoopStore{}, invLogger, 300), eventStatus
}

func TestReserve_Success_SingleItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000))}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	cache.EXPECT().Save(ctx, mock.AnythingOfType("string"), 300).Return(nil)
	bookingRepo.EXPECT().Create(ctx, mock.MatchedBy(func(b *domain.Booking) bool {
		return b.Status == "pending" && b.ExpiresAt != nil && len(b.Items) == 1
	})).Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	res, err := svc.Reserve(ctx, "user-1", "event-1", items)
	require.NoError(t, err)
	assert.Equal(t, "pending", res.Status)
	assert.Equal(t, 20000, res.TotalCents)
	assert.False(t, res.ExpiresAt.IsZero())
	assert.NotEmpty(t, res.BookingID)
}

func TestReserve_Success_MultiItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("ga"), fixtures.WithBookingItemQuantity(3), fixtures.WithBookingItemUnitPrice(5000)),
	}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "ga").Return(5000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "ga", 3).Return(nil)
	cache.EXPECT().Save(ctx, mock.AnythingOfType("string"), 300).Return(nil)
	bookingRepo.EXPECT().Create(ctx, mock.MatchedBy(func(b *domain.Booking) bool {
		return b.Status == "pending" && len(b.Items) == 2
	})).Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	res, err := svc.Reserve(ctx, "user-1", "event-1", items)
	require.NoError(t, err)
	assert.Equal(t, 35000, res.TotalCents)
}

func TestReserve_EmptyItems(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	_, err := svc.Reserve(ctx, "user-1", "event-1", nil)
	assert.ErrorIs(t, err, domain.ErrInvalidQuantity)
}

func TestReserve_FirstItemNoSeats(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(5))}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(5000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 5).Return(domain.ErrNoSeatsAvailable)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	_, err := svc.Reserve(ctx, "user-1", "event-1", items)
	assert.ErrorIs(t, err, domain.ErrNoSeatsAvailable)
}

func TestReserve_SecondItemNoSeats_RollbackFirst(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("ga"), fixtures.WithBookingItemQuantity(10), fixtures.WithBookingItemUnitPrice(5000)),
	}
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(10000, nil)
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "ga").Return(5000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "ga", 10).Return(domain.ErrNoSeatsAvailable)
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	_, err := svc.Reserve(ctx, "user-1", "event-1", items)
	assert.ErrorIs(t, err, domain.ErrNoSeatsAvailable)
}

func TestReserve_CacheSaveFails_RollbackAll(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("ga"), fixtures.WithBookingItemQuantity(3)),
	}
	saveErr := errors.New("redis connection lost")
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "vip").Return(5000, nil)
	seatCounter.EXPECT().GetPrice(ctx, "event-1", "ga").Return(5000, nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "ga", 3).Return(nil)
	cache.EXPECT().Save(ctx, mock.AnythingOfType("string"), 300).Return(saveErr)
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Release(ctx, "event-1", "ga", 3).Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	_, err := svc.Reserve(ctx, "user-1", "event-1", items)
	assert.Equal(t, saveErr, err)
}

func TestConfirm_Success(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	booking := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingUserID("user-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending"))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(booking, nil)
	bookingRepo.EXPECT().UpdateStatusIfPending(ctx, "book-1", "confirmed").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, es := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(true, nil)
	err := svc.Confirm(ctx, "book-1", "pay-1")
	require.NoError(t, err)
}

func TestConfirm_WithItems(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	booking := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending"), fixtures.WithBookingItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemID("item-1"), fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(1)),
	}))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(booking, nil)
	bookingRepo.EXPECT().UpdateStatusIfPending(ctx, "book-1", "confirmed").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, es := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(true, nil)
	err := svc.Confirm(ctx, "book-1", "pay-1")
	require.NoError(t, err)
}

func TestConfirm_SkipsNonPending(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingRepo.EXPECT().FindByID(ctx, "book-expired").Return(nil, pgx.ErrNoRows)
	bookingRepo.EXPECT().FindByID(ctx, "book-confirmed").Return(fixtures.NewTestBooking(fixtures.WithBookingID("book-confirmed"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("confirmed")), nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.Confirm(ctx, "book-expired", "pay-1")
	require.NoError(t, err)
	err = svc.Confirm(ctx, "book-confirmed", "pay-1")
	require.NoError(t, err)
}

func TestConfirm_UpdateFails(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending")), nil)
	bookingRepo.EXPECT().UpdateStatusIfPending(ctx, "book-1", "confirmed").Return(false, errors.New("duplicate key"))
	svc, es := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(true, nil)
	err := svc.Confirm(ctx, "book-1", "pay-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate key")
}

func TestRelease_Success(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	booking := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending"), fixtures.WithBookingItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
	}))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(booking, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.AnythingOfType("*domain.Booking"), "cancelled").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.Release(ctx, "book-1")
	require.NoError(t, err)
}

func TestRelease_MultiItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	booking := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending"), fixtures.WithBookingItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("ga"), fixtures.WithBookingItemQuantity(5)),
	}))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(booking, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.AnythingOfType("*domain.Booking"), "cancelled").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.Release(ctx, "book-1")
	require.NoError(t, err)
}

func TestRelease_SkipsNonPending(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingRepo.EXPECT().FindByID(ctx, "book-stale").Return(nil, pgx.ErrNoRows)
	bookingRepo.EXPECT().FindByID(ctx, "book-confirmed").Return(fixtures.NewTestBooking(fixtures.WithBookingID("book-confirmed"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("confirmed")), nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.Release(ctx, "book-stale")
	require.NoError(t, err)
	err = svc.Release(ctx, "book-confirmed")
	require.NoError(t, err)
}

func TestHandleExpiry_ReleasesSeats(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	booking := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending"), fixtures.WithBookingItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
	}))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(booking, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.AnythingOfType("*domain.Booking"), "expired").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	svc.HandleExpiry(ctx, "book-1")
}

func TestHandleExpiry_MultiItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	booking := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending"), fixtures.WithBookingItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(1)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("ga"), fixtures.WithBookingItemQuantity(3)),
	}))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(booking, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.AnythingOfType("*domain.Booking"), "expired").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	svc.HandleExpiry(ctx, "book-1")
}

func TestHandleExpiry_NotApplied(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending")), nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.AnythingOfType("*domain.Booking"), "expired").Return(false, nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	svc.HandleExpiry(ctx, "book-1")
}

func TestExpireDueBookings(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	b1 := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending"), fixtures.WithBookingItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
	}))
	bookingRepo.EXPECT().ListExpiredPending(ctx, mock.AnythingOfType("time.Time"), 1000).Return([]domain.Booking{*b1}, nil)
	bookingRepo.EXPECT().ListExpiredPending(ctx, mock.AnythingOfType("time.Time"), 1000).Return(nil, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.AnythingOfType("*domain.Booking"), "expired").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	svc.ExpireDueBookings(ctx)
}

func TestConfirm_EventCancelled(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	booking := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingUserID("user-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending"), fixtures.WithBookingItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
	}))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(booking, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.AnythingOfType("*domain.Booking"), "cancelled").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, es := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(false, nil)
	err := svc.Confirm(ctx, "book-1", "pay-1")
	require.NoError(t, err)
}

func TestConfirm_StatusCheckError(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending")), nil)
	svc, es := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(false, errors.New("db error"))
	err := svc.Confirm(ctx, "book-1", "pay-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

func TestCancelBookings_Success(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	b1 := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingStatus("confirmed"), fixtures.WithBookingItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
	}))
	bookingRepo.EXPECT().ListByEventID(ctx, "event-1").Return([]domain.Booking{*b1}, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.AnythingOfType("*domain.Booking"), "cancelled").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.CancelBookings(ctx, "event-1")
	require.NoError(t, err)
}

func TestCancelBookings_ListByEventIDError(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingRepo.EXPECT().ListByEventID(ctx, "event-1").Return(nil, errors.New("db error"))
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.CancelBookings(ctx, "event-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

func TestCancelBookings_SkipsNonConfirmed(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	b1 := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingStatus("cancelled"))
	bookingRepo.EXPECT().ListByEventID(ctx, "event-1").Return([]domain.Booking{*b1}, nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.CancelBookings(ctx, "event-1")
	require.NoError(t, err)
}

func TestGetBooking_Found(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	expected := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(expected, nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	booking, err := svc.GetBooking(ctx, "book-1")
	require.NoError(t, err)
	assert.Equal(t, "book-1", booking.ID)
}

func TestGetBooking_NotFound(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	bookingRepo.EXPECT().FindByID(ctx, "bad-id").Return(nil, pgx.ErrNoRows)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	_, err := svc.GetBooking(ctx, "bad-id")
	require.Error(t, err)
}

func TestListMyBookings(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	expected := []domain.Booking{*fixtures.NewTestBooking(fixtures.WithBookingID("book-1")), *fixtures.NewTestBooking(fixtures.WithBookingID("book-2"))}
	bookingRepo.EXPECT().ListByUser(ctx, "user-1").Return(expected, nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	bookings, err := svc.ListMyBookings(ctx, "user-1")
	require.NoError(t, err)
	assert.Len(t, bookings, 2)
}

func TestExpireOnPaymentFailed_SetsExpired(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	booking := fixtures.NewTestBooking(fixtures.WithBookingID("book-1"), fixtures.WithBookingEventID("event-1"), fixtures.WithBookingStatus("pending"), fixtures.WithBookingItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
	}))
	bookingRepo.EXPECT().FindByID(ctx, "book-1").Return(booking, nil)
	bookingRepo.EXPECT().TransitionAndReleaseSeats(ctx, mock.AnythingOfType("*domain.Booking"), "expired").Return(true, nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, _ := newBookingService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.ExpireOnPaymentFailed(ctx, "book-1")
	require.NoError(t, err)
}

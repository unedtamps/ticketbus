package application_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nedo/TicketSaas/internal/inventory/application"
	"github.com/nedo/TicketSaas/internal/inventory/domain"
	"github.com/nedo/TicketSaas/internal/inventory/domain/mocks"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
	"github.com/nedo/TicketSaas/tests/fixtures"
)

var invLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

func newInventoryService(
	t testing.TB,
	bookingRepo domain.BookingRepository,
	cache domain.ReservationCache,
	seatCounter domain.SeatCounter,
	consumer domain.EventConsumer,
) (*application.InventoryService, *mocks.MockEventStatusRepository) {
	t.Helper()
	eventStatus := mocks.NewMockEventStatusRepository(t)
	return application.NewInventoryService(bookingRepo, cache, seatCounter, consumer, eventStatus, outbox.NoopStore{}, invLogger, 300), eventStatus
}

func TestReserve_Success_SingleItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	items := []domain.BookingItem{*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2), fixtures.WithBookingItemUnitPrice(10000))}
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	cache.EXPECT().Save(ctx, mock.AnythingOfType("*domain.Reservation"), 300).Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	res, err := svc.Reserve(ctx, "user-1", "event-1", items)
	require.NoError(t, err)
	assert.Equal(t, "held", res.Status)
	assert.Equal(t, 20000, res.TotalCents)
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
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "ga", 3).Return(nil)
	cache.EXPECT().Save(ctx, mock.AnythingOfType("*domain.Reservation"), 300).Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 5).Return(domain.ErrNoSeatsAvailable)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "ga", 10).Return(domain.ErrNoSeatsAvailable)
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	seatCounter.EXPECT().Reserve(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Reserve(ctx, "event-1", "ga", 3).Return(nil)
	cache.EXPECT().Save(ctx, mock.AnythingOfType("*domain.Reservation"), 300).Return(saveErr)
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Release(ctx, "event-1", "ga", 3).Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	_, err := svc.Reserve(ctx, "user-1", "event-1", items)
	assert.Equal(t, saveErr, err)
}

func TestConfirm_Success(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	res := fixtures.NewTestReservation(fixtures.WithReservationBookingID("book-1"), fixtures.WithReservationUserID("user-1"), fixtures.WithReservationEventID("event-1"), fixtures.WithReservationTotalCents(15000))
	cache.EXPECT().Find(ctx, "book-1").Return(res, nil)
	bookingRepo.EXPECT().Create(ctx, mock.MatchedBy(func(b *domain.Booking) bool {
		return b.ID == "book-1" && b.PaymentID == "pay-1" && b.Status == "confirmed"
	})).Return(nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, es := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(true, nil)
	err := svc.Confirm(ctx, "book-1", "pay-1")
	require.NoError(t, err)
}

func TestConfirm_BookingItemsGetUUIDs(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	res := fixtures.NewTestReservation(fixtures.WithReservationBookingID("book-1"), fixtures.WithReservationEventID("event-1"), fixtures.WithReservationItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(1)),
	}))
	cache.EXPECT().Find(ctx, "book-1").Return(res, nil)
	bookingRepo.EXPECT().Create(ctx, mock.MatchedBy(func(b *domain.Booking) bool {
		return len(b.Items) == 1 && b.Items[0].ID != "" && b.Items[0].BookingID == "book-1"
	})).Return(nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, es := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	es.EXPECT().IsPublished(ctx, "event-1").Return(true, nil)
	err := svc.Confirm(ctx, "book-1", "pay-1")
	require.NoError(t, err)
}

func TestConfirm_ReservationAlreadyRemoved(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	cache.EXPECT().Find(ctx, "book-expired").Return(nil, redis.Nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.Confirm(ctx, "book-expired", "pay-1")
	require.NoError(t, err)
}

func TestConfirm_BookingCreateFails(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	res := fixtures.NewTestReservation(fixtures.WithReservationBookingID("book-1"), fixtures.WithReservationEventID("event-1"))
	cache.EXPECT().Find(ctx, "book-1").Return(res, nil)
	bookingRepo.EXPECT().Create(ctx, mock.AnythingOfType("*domain.Booking")).Return(errors.New("duplicate key"))
	svc, es := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	res := fixtures.NewTestReservation(fixtures.WithReservationBookingID("book-1"), fixtures.WithReservationEventID("event-1"), fixtures.WithReservationItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
	}))
	cache.EXPECT().Find(ctx, "book-1").Return(res, nil)
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.Release(ctx, "book-1")
	require.NoError(t, err)
}

func TestRelease_MultiItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	res := fixtures.NewTestReservation(fixtures.WithReservationBookingID("book-1"), fixtures.WithReservationEventID("event-1"), fixtures.WithReservationItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("ga"), fixtures.WithBookingItemQuantity(5)),
	}))
	cache.EXPECT().Find(ctx, "book-1").Return(res, nil)
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	seatCounter.EXPECT().Release(ctx, "event-1", "ga", 5).Return(nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.Release(ctx, "book-1")
	require.NoError(t, err)
}

func TestRelease_ReservationAlreadyRemoved(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	cache.EXPECT().Find(ctx, "book-stale").Return(nil, redis.Nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.Release(ctx, "book-stale")
	require.NoError(t, err)
}

func TestHandleExpiry_ReleasesSeats(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	res := fixtures.NewTestReservation(fixtures.WithReservationBookingID("book-1"), fixtures.WithReservationEventID("event-1"), fixtures.WithReservationItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
	}))
	seatCounter.EXPECT().Release(context.Background(), "event-1", "vip", 2).Return(nil)
	cache.EXPECT().Delete(context.Background(), "book-1").Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	svc.HandleExpiry(context.Background(), res)
}

func TestHandleExpiry_MultiItem(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	res := fixtures.NewTestReservation(fixtures.WithReservationBookingID("book-1"), fixtures.WithReservationEventID("event-1"), fixtures.WithReservationItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(1)),
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("ga"), fixtures.WithBookingItemQuantity(3)),
	}))
	seatCounter.EXPECT().Release(context.Background(), "event-1", "vip", 1).Return(nil)
	seatCounter.EXPECT().Release(context.Background(), "event-1", "ga", 3).Return(nil)
	cache.EXPECT().Delete(context.Background(), "book-1").Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	svc.HandleExpiry(context.Background(), res)
}

func TestInitSeats_Single(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	types := []domain.TicketTypeInfo{{TicketTypeID: "tt-vip", Name: "VIP", Quantity: 50}}
	seatCounter.EXPECT().Init(ctx, "event-1", "tt-vip", 50).Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.InitSeats(ctx, "event-1", types)
	require.NoError(t, err)
}

func TestInitSeats_Multiple(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	types := []domain.TicketTypeInfo{
		{TicketTypeID: "tt-vip", Name: "VIP", Quantity: 10},
		{TicketTypeID: "tt-ga", Name: "GA", Quantity: 100},
	}
	seatCounter.EXPECT().Init(ctx, "event-1", "tt-vip", 10).Return(nil)
	seatCounter.EXPECT().Init(ctx, "event-1", "tt-ga", 100).Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.InitSeats(ctx, "event-1", types)
	require.NoError(t, err)
}

func TestInitSeats_InitFails(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	types := []domain.TicketTypeInfo{
		{TicketTypeID: "tt-vip", Name: "VIP", Quantity: 50},
		{TicketTypeID: "tt-ga", Name: "GA", Quantity: 100},
	}
	seatCounter.EXPECT().Init(ctx, "event-1", "tt-vip", 50).Return(nil)
	seatCounter.EXPECT().Init(ctx, "event-1", "tt-ga", 100).Return(errors.New("redis error"))
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	err := svc.InitSeats(ctx, "event-1", types)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis error")
}

func TestConfirm_EventCancelled_CreatesBookingWithRefundPending(t *testing.T) {
	bookingRepo := mocks.NewMockBookingRepository(t)
	cache := mocks.NewMockReservationCache(t)
	seatCounter := mocks.NewMockSeatCounter(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	res := fixtures.NewTestReservation(fixtures.WithReservationBookingID("book-1"), fixtures.WithReservationUserID("user-1"), fixtures.WithReservationEventID("event-1"), fixtures.WithReservationTotalCents(15000), fixtures.WithReservationItems([]domain.BookingItem{
		*fixtures.NewTestBookingItem(fixtures.WithBookingItemTicketTypeID("vip"), fixtures.WithBookingItemQuantity(2)),
	}))
	cache.EXPECT().Find(ctx, "book-1").Return(res, nil)
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	bookingRepo.EXPECT().Create(ctx, mock.MatchedBy(func(b *domain.Booking) bool {
		return b.ID == "book-1" && b.Status == "cancelled" && b.RefundStatus == "pending"
	})).Return(nil)
	cache.EXPECT().Delete(ctx, "book-1").Return(nil)
	svc, es := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	res := fixtures.NewTestReservation(fixtures.WithReservationBookingID("book-1"), fixtures.WithReservationEventID("event-1"))
	cache.EXPECT().Find(ctx, "book-1").Return(res, nil)
	svc, es := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	seatCounter.EXPECT().Release(ctx, "event-1", "vip", 2).Return(nil)
	bookingRepo.EXPECT().CancelByEventID(ctx, "event-1").Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	bookingRepo.EXPECT().CancelByEventID(ctx, "event-1").Return(nil)
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
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
	svc, _ := newInventoryService(t, bookingRepo, cache, seatCounter, consumer)
	bookings, err := svc.ListMyBookings(ctx, "user-1")
	require.NoError(t, err)
	assert.Len(t, bookings, 2)
}

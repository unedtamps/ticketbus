package application_test

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

	"github.com/nedo/TicketSaas/internal/payment/application"
	"github.com/nedo/TicketSaas/internal/payment/domain"
	"github.com/nedo/TicketSaas/internal/payment/domain/mocks"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
	"github.com/nedo/TicketSaas/tests/fixtures"
)

var payLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

func newPaymentService(
	t testing.TB,
	txnRepo domain.TransactionRepository,
	processor domain.PaymentProcessor,
	consumer domain.EventConsumer,
) *application.PaymentService {
	t.Helper()
	refundRepo := mocks.NewMockRefundRepository(t)
	return application.NewPaymentService(txnRepo, refundRepo, processor, consumer, outbox.NoopStore{}, payLogger, "http://localhost:8000/api/payments/webhook")
}

func newPaymentServiceWithRefundRepo(
	t testing.TB,
	txnRepo domain.TransactionRepository,
	refundRepo domain.RefundRepository,
	processor domain.PaymentProcessor,
	consumer domain.EventConsumer,
) *application.PaymentService {
	t.Helper()
	return application.NewPaymentService(txnRepo, refundRepo, processor, consumer, outbox.NoopStore{}, payLogger, "http://localhost:8000/api/payments/webhook")
}

func TestInitiatePayment_Success(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txnRepo.EXPECT().Create(ctx, mock.MatchedBy(func(tx *domain.Transaction) bool {
		return tx.BookingID == "book-1" && tx.Status == domain.StatusInitiated
	})).Return(nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	txn, err := svc.InitiatePayment(ctx, "book-1", 10000, "user-1")
	require.NoError(t, err)
	assert.Equal(t, "book-1", txn.BookingID)
	assert.Equal(t, domain.StatusInitiated, txn.Status)
	assert.NotEmpty(t, txn.ID)
}

func TestInitiatePayment_CreateFails(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txnRepo.EXPECT().Create(ctx, mock.AnythingOfType("*domain.Transaction")).Return(errors.New("duplicate"))
	svc := newPaymentService(t, txnRepo, processor, consumer)
	_, err := svc.InitiatePayment(ctx, "book-1", 10000, "user-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestCheckout_Success(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionStatus(domain.StatusInitiated), fixtures.WithTransactionAmount(10000))
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	processor.EXPECT().Charge(ctx, "txn-1", 10000, "USD").Return("ref-123", nil)
	txnRepo.EXPECT().UpdateStatus(ctx, "txn-1", domain.StatusProcessing, "ref-123").Return(nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	result, err := svc.Checkout(ctx, "txn-1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusProcessing, result.Status)
}

func TestCheckout_NotFound(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txnRepo.EXPECT().FindByID(ctx, "bad-id").Return(nil, pgx.ErrNoRows)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	_, err := svc.Checkout(ctx, "bad-id")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

func TestCheckout_AlreadyProcessing(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionStatus(domain.StatusSuccess))
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	_, err := svc.Checkout(ctx, "txn-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestCheckout_AlreadyCompleted(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionStatus(domain.StatusCompleted))
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	_, err := svc.Checkout(ctx, "txn-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestCheckout_ChargeFails(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionStatus(domain.StatusInitiated), fixtures.WithTransactionAmount(10000))
	chargeErr := errors.New("card declined")
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	processor.EXPECT().Charge(ctx, "txn-1", 10000, "USD").Return("", chargeErr)
	txnRepo.EXPECT().UpdateStatus(ctx, "txn-1", domain.StatusFailed, "").Return(nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	_, err := svc.Checkout(ctx, "txn-1")
	assert.ErrorIs(t, err, chargeErr)
}

func TestConfirmPayment_Success(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionStatus(domain.StatusProcessing))
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	txnRepo.EXPECT().UpdateStatus(ctx, "txn-1", domain.StatusCompleted, txn.ProviderRef).Return(nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	err := svc.ConfirmPayment(ctx, "txn-1")
	require.NoError(t, err)
}

func TestConfirmPayment_NotFound(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txnRepo.EXPECT().FindByID(ctx, "bad-id").Return(nil, pgx.ErrNoRows)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	err := svc.ConfirmPayment(ctx, "bad-id")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

func TestConfirmPayment_AlreadyCompleted(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionStatus(domain.StatusCompleted))
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	err := svc.ConfirmPayment(ctx, "txn-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestConfirmPayment_StillInitiated(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionStatus(domain.StatusExpired))
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	err := svc.ConfirmPayment(ctx, "txn-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestConfirmPayment_UpdateStatusFails(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionStatus(domain.StatusProcessing))
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	txnRepo.EXPECT().UpdateStatus(ctx, "txn-1", domain.StatusCompleted, txn.ProviderRef).Return(errors.New("db error"))
	svc := newPaymentService(t, txnRepo, processor, consumer)
	err := svc.ConfirmPayment(ctx, "txn-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

func TestHandleReservationCancelled_Success_Pending(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionBookingID("book-1"), fixtures.WithTransactionStatus(domain.StatusPending))
	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	txnRepo.EXPECT().UpdateStatus(ctx, "txn-1", domain.StatusExpired, "reservation_cancelled").Return(nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	err := svc.HandleReservationCancelled(ctx, "book-1")
	require.NoError(t, err)
}

func TestHandleReservationCancelled_AlreadyProcessed(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionBookingID("book-1"), fixtures.WithTransactionStatus(domain.StatusExpired))
	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	err := svc.HandleReservationCancelled(ctx, "book-1")
	require.NoError(t, err)
}

func TestHandleReservationCancelled_NoTransaction(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(nil, pgx.ErrNoRows)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	err := svc.HandleReservationCancelled(ctx, "book-1")
	require.NoError(t, err)
}

func TestHandleReservationCancelled_UpdateStatusFails(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionBookingID("book-1"), fixtures.WithTransactionStatus(domain.StatusPending))
	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	txnRepo.EXPECT().UpdateStatus(ctx, "txn-1", domain.StatusExpired, "reservation_cancelled").Return(errors.New("db error"))
	svc := newPaymentService(t, txnRepo, processor, consumer)
	err := svc.HandleReservationCancelled(ctx, "book-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

func TestCheckoutByBooking_Found(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"), fixtures.WithTransactionBookingID("book-1"), fixtures.WithTransactionStatus(domain.StatusInitiated), fixtures.WithTransactionAmount(10000))
	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	processor.EXPECT().Charge(ctx, "txn-1", 10000, "USD").Return("ref-123", nil)
	txnRepo.EXPECT().UpdateStatus(ctx, "txn-1", domain.StatusProcessing, "ref-123").Return(nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	result, err := svc.CheckoutByBooking(ctx, "book-1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusProcessing, result.Status)
}

func TestCheckoutByBooking_NotFound(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txnRepo.EXPECT().FindByBookingID(ctx, "book-bad").Return(nil, pgx.ErrNoRows)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	_, err := svc.CheckoutByBooking(ctx, "book-bad")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

func TestGetTransaction_Found(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	expected := fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1"))
	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(expected, nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	txn, err := svc.GetTransaction(ctx, "txn-1")
	require.NoError(t, err)
	assert.Equal(t, "txn-1", txn.ID)
}

func TestGetTransaction_NotFound(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txnRepo.EXPECT().FindByID(ctx, "bad-id").Return(nil, pgx.ErrNoRows)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	_, err := svc.GetTransaction(ctx, "bad-id")
	require.Error(t, err)
}

func TestListMyTransactions_Success(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	expected := []domain.Transaction{*fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-1")), *fixtures.NewTestTransaction(fixtures.WithTransactionID("txn-2"))}
	txnRepo.EXPECT().ListByUser(ctx, "user-1").Return(expected, nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	txns, err := svc.ListMyTransactions(ctx, "user-1")
	require.NoError(t, err)
	assert.Len(t, txns, 2)
}

func TestListMyTransactions_Empty(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()
	txnRepo.EXPECT().ListByUser(ctx, "new-user").Return(nil, nil)
	svc := newPaymentService(t, txnRepo, processor, consumer)
	txns, err := svc.ListMyTransactions(ctx, "new-user")
	require.NoError(t, err)
	assert.Empty(t, txns)
}

func TestHandleEventCancelled_CreatesRefundForSuccessOnly(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	refundRepo := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()

	txns := []domain.Transaction{
		{ID: "txn-1", EventID: "event-1", BookingID: "book-1", AmountCents: 10000, Currency: "USD", Status: domain.StatusSuccess},
		{ID: "txn-2", EventID: "event-1", BookingID: "book-2", AmountCents: 5000, Currency: "USD", Status: domain.StatusPending},
	}
	txnRepo.EXPECT().ListByEventID(ctx, "event-1").Return(txns, nil)
	refundRepo.EXPECT().Create(ctx, mock.MatchedBy(func(r *domain.RefundRequest) bool {
		return r.TransactionID == "txn-1" &&
			r.Status == domain.RefundPending &&
			r.Reason == "event_cancelled" &&
			r.IdempotencyKey == "event-cancelled:txn-1" &&
			r.AmountCents == 10000
	})).Return(nil)
	txnRepo.EXPECT().UpdateRefundStatus(ctx, "txn-1", "pending").Return(nil)

	svc := newPaymentServiceWithRefundRepo(t, txnRepo, refundRepo, processor, consumer)
	err := svc.HandleEventCancelled(ctx, "event-1")
	require.NoError(t, err)
}

func TestHandleEventCancelled_IdempotentAcrossCalls(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	refundRepo := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()

	txns := []domain.Transaction{
		{ID: "txn-1", EventID: "event-1", BookingID: "book-1", AmountCents: 10000, Currency: "USD", Status: domain.StatusSuccess},
	}
	txnRepo.EXPECT().ListByEventID(ctx, "event-1").Return(txns, nil).Twice()
	refundRepo.EXPECT().Create(ctx, mock.AnythingOfType("*domain.RefundRequest")).Return(nil).Twice()
	txnRepo.EXPECT().UpdateRefundStatus(ctx, "txn-1", "pending").Return(nil).Twice()

	svc := newPaymentServiceWithRefundRepo(t, txnRepo, refundRepo, processor, consumer)
	require.NoError(t, svc.HandleEventCancelled(ctx, "event-1"))
	require.NoError(t, svc.HandleEventCancelled(ctx, "event-1"))
}

func TestHandleEventCancelled_ListError(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	refundRepo := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	consumer := mocks.NewMockEventConsumer(t)
	ctx := context.Background()

	txnRepo.EXPECT().ListByEventID(ctx, "event-1").Return(nil, errors.New("db error"))
	svc := newPaymentServiceWithRefundRepo(t, txnRepo, refundRepo, processor, consumer)
	err := svc.HandleEventCancelled(ctx, "event-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

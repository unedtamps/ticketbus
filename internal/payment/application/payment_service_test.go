package application_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	refundRepo domain.RefundRepository,
	processor domain.PaymentProcessor,
	consumer domain.EventConsumer,
) *application.PaymentService {
	t.Helper()
	return application.NewPaymentService(
		txnRepo, refundRepo, processor, consumer,
		outbox.NoopStore{}, payLogger, "", "mock", 5, []string{"ID_QRIS"},
	)
}

func newPaymentServiceDefaults(
	t testing.TB,
	txnRepo domain.TransactionRepository,
	processor domain.PaymentProcessor,
	consumer domain.EventConsumer,
) *application.PaymentService {
	t.Helper()
	return newPaymentService(t, txnRepo, mocks.NewMockRefundRepository(t), processor, consumer)
}

// ── InitiateTxnForBooking (internal) ──────────────────────────────────────────────────────

func TestInitiateTxnForBooking_Success(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	expiresAt := time.Now().Add(15 * time.Minute)

	txnRepo.EXPECT().Create(ctx, mock.MatchedBy(func(tx *domain.Transaction) bool {
		return tx.BookingID == "book-1" && tx.EventID == "event-1" && tx.UserID == "user-1" &&
			tx.CustomerEmail == "user@example.com" && tx.AmountRupiah == 10000 &&
			tx.Currency == "IDR" && tx.Status == domain.StatusInitiated &&
			tx.ExpiresAt != nil && tx.ExpiresAt.Equal(expiresAt)
	})).Return(nil)

	txn, err := svc.InitiateTxnForBooking(ctx, "book-1", "event-1", "user-1", "user@example.com", 10000, expiresAt)
	require.NoError(t, err)
	assert.Equal(t, "book-1", txn.BookingID)
	assert.Equal(t, "IDR", txn.Currency)
}

func TestInitiateTxnForBooking_Idempotent(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	existing := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
	)

	txnRepo.EXPECT().Create(ctx, mock.Anything).Return(&pgconn.PgError{Code: "23505", Message: "duplicate key"})
	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(existing, nil)

	txn, err := svc.InitiateTxnForBooking(ctx, "book-1", "event-1", "user-1", "", 10000, time.Now().Add(15*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, "txn-1", txn.ID)
}

// ── ProcessPayment ──────────────────────────────────────────────────────────

func TestProcessPayment_Success(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	svc := newPaymentServiceDefaults(t, txnRepo, processor, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	expiresAt := time.Now().Add(60 * time.Minute)
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionUserID("user-1"),
		fixtures.WithTransactionAmount(10000),
		fixtures.WithTransactionCurrency("IDR"),
		fixtures.WithTransactionCustomerEmail("user@example.com"),
		fixtures.WithTransactionStatus(domain.StatusInitiated),
		fixtures.WithTransactionExpiresAt(expiresAt),
	)
	result := &domain.SessionResult{
		ProviderRef:    "ps-1",
		PaymentLinkURL: "https://checkout.example/ps-1",
		ExpiresAt:      expiresAt.Add(-5 * time.Minute),
	}

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	processor.EXPECT().CreateSession(ctx, "book-1", 10000, "IDR", expiresAt.Add(-5*time.Minute),
		[]string{"ID_QRIS"}, "user@example.com").Return(result, nil)
	txnRepo.EXPECT().TransitionIfInitiated(ctx, "txn-1", domain.StatusPending, "ps-1").Return(true, nil)
	txnRepo.EXPECT().UpdateSession(ctx, "txn-1", "ps-1", "https://checkout.example/ps-1").Return(nil)

	gotTxn, gotResult, err := svc.ProcessPayment(ctx, "book-1", "user-1")
	require.NoError(t, err)
	assert.Equal(t, "txn-1", gotTxn.ID)
	assert.Equal(t, "ps-1", gotResult.ProviderRef)
	assert.Equal(t, "https://checkout.example/ps-1", gotResult.PaymentLinkURL)
}

func TestProcessPayment_NotFound(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(nil, pgx.ErrNoRows)
	_, _, err := svc.ProcessPayment(ctx, "book-1", "user-1")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

func TestProcessPayment_AlreadyInitiated(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	_, _, err := svc.ProcessPayment(ctx, "book-1", "user-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestProcessPayment_AlreadyCompleted(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionStatus(domain.StatusCompleted),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	_, _, err := svc.ProcessPayment(ctx, "book-1", "user-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestProcessPayment_TransitionRace_CancelsSession(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	svc := newPaymentServiceDefaults(t, txnRepo, processor, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	expiresAt := time.Now().Add(60 * time.Minute)
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionUserID("user-1"),
		fixtures.WithTransactionCurrency("IDR"),
		fixtures.WithTransactionStatus(domain.StatusInitiated),
		fixtures.WithTransactionExpiresAt(expiresAt),
	)
	result := &domain.SessionResult{ProviderRef: "ps-1", PaymentLinkURL: "https://checkout.example/ps-1"}

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	processor.EXPECT().CreateSession(ctx, "book-1", 10000, "IDR", expiresAt.Add(-5*time.Minute),
		[]string{"ID_QRIS"}, "").Return(result, nil)
	txnRepo.EXPECT().TransitionIfInitiated(ctx, "txn-1", domain.StatusPending, "ps-1").Return(false, nil)
	processor.EXPECT().CancelSession(ctx, "ps-1").Return(nil)

	_, _, err := svc.ProcessPayment(ctx, "book-1", "user-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestProcessPayment_Expired(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	past := time.Now().Add(-1 * time.Minute)
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionUserID("user-1"),
		fixtures.WithTransactionStatus(domain.StatusInitiated),
		fixtures.WithTransactionExpiresAt(past),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	_, _, err := svc.ProcessPayment(ctx, "book-1", "user-1")
	assert.ErrorIs(t, err, domain.ErrTransactionExpired)
}

func TestProcessPayment_LessThanOneMinuteRemaining(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	soon := time.Now().Add(30 * time.Second)
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionUserID("user-1"),
		fixtures.WithTransactionStatus(domain.StatusInitiated),
		fixtures.WithTransactionExpiresAt(soon),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	_, _, err := svc.ProcessPayment(ctx, "book-1", "user-1")
	assert.ErrorIs(t, err, domain.ErrTransactionExpired)
}

func TestProcessPayment_WrongOwner(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionUserID("user-other"),
		fixtures.WithTransactionStatus(domain.StatusInitiated),
		fixtures.WithTransactionExpiresAt(time.Now().Add(15*time.Minute)),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	_, _, err := svc.ProcessPayment(ctx, "book-1", "user-1")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

// ── GetPaymentStatus ─────────────────────────────────────────────────────────

func TestGetPaymentStatus_Success(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionPaymentLinkURL("https://checkout.example/ps-1"),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	got, err := svc.GetPaymentStatus(ctx, "book-1")
	require.NoError(t, err)
	assert.Equal(t, "https://checkout.example/ps-1", got.PaymentLinkURL)
}

func TestGetPaymentStatus_NotFound(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(nil, pgx.ErrNoRows)
	_, err := svc.GetPaymentStatus(ctx, "book-1")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

// ── HandleSessionWebhook ─────────────────────────────────────────────────────

func webhookPayload(event, referenceID, sessionID, status string) []byte {
	return []byte(`{"event":"` + event + `","data":{"reference_id":"` + referenceID +
		`","payment_session_id":"` + sessionID + `","status":"` + status + `"}}`)
}

func TestHandleSessionWebhook_Completed(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	txnRepo.EXPECT().TransitionIfActive(ctx, "txn-1", domain.StatusCompleted, "ps-1").Return(true, nil)

	err := svc.HandleSessionWebhook(ctx, "mock", webhookPayload("payment_session.completed", "book-1", "ps-1", "COMPLETED"))
	require.NoError(t, err)
}

func TestHandleSessionWebhook_CompletedAfterExpired_Refund(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	refundRepo := mocks.NewMockRefundRepository(t)
	svc := newPaymentService(t, txnRepo, refundRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusExpired),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	txnRepo.EXPECT().TransitionIfActive(ctx, "txn-1", domain.StatusCompleted, "ps-1").Return(false, nil)
	refundRepo.EXPECT().Create(ctx, mock.MatchedBy(func(r *domain.RefundRequest) bool {
		return r.Reason == "late_payment" && r.IdempotencyKey == "late-payment:txn-1"
	})).Return(nil)
	txnRepo.EXPECT().UpdateRefundStatus(ctx, "txn-1", "pending").Return(nil)

	err := svc.HandleSessionWebhook(ctx, "mock", webhookPayload("payment_session.completed", "book-1", "ps-1", "COMPLETED"))
	require.NoError(t, err)
}

func TestHandleSessionWebhook_CompletedDuplicate(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusCompleted),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	txnRepo.EXPECT().TransitionIfActive(ctx, "txn-1", domain.StatusCompleted, "ps-1").Return(false, nil)

	err := svc.HandleSessionWebhook(ctx, "mock", webhookPayload("payment_session.completed", "book-1", "ps-1", "COMPLETED"))
	require.NoError(t, err)
}

func TestHandleSessionWebhook_Expired(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	txnRepo.EXPECT().TransitionIfActive(ctx, "txn-1", domain.StatusExpired, "ps-1").Return(true, nil)

	err := svc.HandleSessionWebhook(ctx, "mock", webhookPayload("payment_session.expired", "book-1", "ps-1", "EXPIRED"))
	require.NoError(t, err)
}

// TestHandleSessionWebhook_RealXenditPayload verifies the handler accepts a
// payload in the shape Xendit actually delivers: the session id lives in
// data.id, status is uppercase, and amount is a JSON number.
func TestHandleSessionWebhook_RealXenditPayload(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionProviderRef("ps-579c8d61f23fa4ca35e52da4"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)
	txnRepo.EXPECT().TransitionIfActive(ctx, "txn-1", domain.StatusCompleted, "ps-579c8d61f23fa4ca35e52da4").Return(true, nil)

	payload := []byte(`{
		"event": "payment_session.completed",
		"business_id": "5781d19b2e2385880609791c",
		"created": "2020-04-20T16:25:52Z",
		"data": {
			"payment_session_id": "ps-579c8d61f23fa4ca35e52da4",
			"allowed_payment_channels": ["CARD", "OVO", "DANA"],
			"amount": 100000,
			"country": "ID",
			"created": "2020-04-20T16:25:52Z",
			"currency": "IDR",
			"customer_id": "cust-e2878b4c-d57e-4a2c-922d-c0313c2800a3",
			"expires_at": "2030-02-15T16:23:52Z",
			"mode": "PAYMENT_LINK",
			"payment_link_url": "https://checkout.xendit.co/latest/65c8b6916b68a555078489a5",
			"payment_request_id": "pr-8363892-f4d9-421c-dhdy-jdh9e30380",
			"reference_id": "book-1",
			"session_type": "SAVE",
			"status": "COMPLETED",
			"updated": "2020-04-20T16:25:52Z"
		}
	}`)

	err := svc.HandleSessionWebhook(ctx, "xendit", payload)
	require.NoError(t, err)
}

// TestHandleSessionWebhook_SessionMismatch_Ignored verifies a webhook whose
// session id does not match the stored transaction is acknowledged but not
// applied (no state transition happens).
func TestHandleSessionWebhook_SessionMismatch_Ignored(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionProviderRef("ps-abc"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "book-1").Return(txn, nil)

	payload := []byte(`{"event":"payment_session.completed","data":{"payment_session_id":"ps-some-other-session","reference_id":"book-1","status":"COMPLETED"}}`)
	err := svc.HandleSessionWebhook(ctx, "xendit", payload)
	require.NoError(t, err)
}

func TestHandleSessionWebhook_TxnMissing_ReturnsError(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()

	txnRepo.EXPECT().FindByBookingID(ctx, "ghost-booking").Return(nil, pgx.ErrNoRows)
	err := svc.HandleSessionWebhook(ctx, "mock", webhookPayload("payment_session.completed", "ghost-booking", "ps-1", "COMPLETED"))
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestHandleSessionWebhook_MissingReference(t *testing.T) {
	svc := newPaymentServiceDefaults(t, mocks.NewMockTransactionRepository(t),
		mocks.NewMockPaymentProcessor(t), mocks.NewMockEventConsumer(t))
	ctx := context.Background()

	err := svc.HandleSessionWebhook(ctx, "mock", []byte(`{"event":"payment_session.completed","data":{"status":"COMPLETED"}}`))
	require.Error(t, err)
}

// ── ProcessExpired ───────────────────────────────────────────────────────────

func TestProcessExpired_WithSessionRef(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	svc := newPaymentServiceDefaults(t, txnRepo, processor, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	past := time.Now().Add(-1 * time.Minute)
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusPending),
		fixtures.WithTransactionExpiresAt(past),
	)

	txnRepo.EXPECT().ListActiveExpired(ctx, mock.Anything, 100).Return([]domain.Transaction{*txn}, nil)
	processor.EXPECT().CancelSession(ctx, "ps-1").Return(nil)
	txnRepo.EXPECT().TransitionIfActive(ctx, "txn-1", domain.StatusExpired, "ps-1").Return(true, nil)

	err := svc.ProcessExpired(ctx, 100)
	require.NoError(t, err)
}

func TestProcessExpired_WithoutSessionRef_SkipsGateway(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	svc := newPaymentServiceDefaults(t, txnRepo, processor, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	past := time.Now().Add(-1 * time.Minute)
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionStatus(domain.StatusPending),
		fixtures.WithTransactionExpiresAt(past),
	)

	txnRepo.EXPECT().ListActiveExpired(ctx, mock.Anything, 100).Return([]domain.Transaction{*txn}, nil)
	processor.EXPECT().CancelSession(mock.Anything, mock.Anything).Maybe().Return(nil)
	txnRepo.EXPECT().TransitionIfActive(ctx, "txn-1", domain.StatusExpired, "").Return(true, nil)

	err := svc.ProcessExpired(ctx, 100)
	require.NoError(t, err)
}

// ── HandleEventCancelled ─────────────────────────────────────────────────────

func TestHandleEventCancelled_RefundsCompletedLeavesPending(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	refundRepo := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	svc := newPaymentService(t, txnRepo, refundRepo, processor, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	completedTxn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-completed"),
		fixtures.WithTransactionBookingID("book-1"),
		fixtures.WithTransactionStatus(domain.StatusCompleted),
	)
	pendingTxn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-pending"),
		fixtures.WithTransactionBookingID("book-2"),
		fixtures.WithTransactionProviderRef("ps-2"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().ListByEventID(ctx, "event-1").Return([]domain.Transaction{*completedTxn, *pendingTxn}, nil)
	refundRepo.EXPECT().Create(ctx, mock.MatchedBy(func(r *domain.RefundRequest) bool {
		return r.Reason == "event_cancelled" && r.IdempotencyKey == "event-cancelled:txn-completed"
	})).Return(nil)
	txnRepo.EXPECT().UpdateRefundStatus(ctx, "txn-completed", "pending").Return(nil)
	// Pending transactions are intentionally untouched.
	processor.EXPECT().CancelSession(mock.Anything, mock.Anything).Maybe().Return(nil)
	txnRepo.EXPECT().TransitionIfActive(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe().Return(false, nil)

	err := svc.HandleEventCancelled(ctx, "event-1")
	require.NoError(t, err)
}

// ── Checkout ─────────────────────────────────────────────────────────────────

func TestCheckout_AlreadyProcessed(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionStatus(domain.StatusCompleted),
	)

	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(txn, nil)
	_, err := svc.Checkout(ctx, "txn-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestCheckout_NotFound(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()

	txnRepo.EXPECT().FindByID(ctx, "txn-1").Return(nil, pgx.ErrNoRows)
	_, err := svc.Checkout(ctx, "txn-1")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

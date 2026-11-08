package application_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/unedtamps/ticketbus/service/payment/internal/application"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain/mocks"
	fixtures "github.com/unedtamps/ticketbus/service/payment/internal/testfixtures"
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
		txnRepo, refundRepo, mocks.NewMockRefundDestinationRepository(t),
		mocks.NewMockPayoutRepository(t), processor, consumer,
		payLogger, "mock", []string{"QRIS", "GOPAY"},
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
		return tx.BookingID == "11111111-1111-4111-8111-111111111111" && tx.EventID == "event-1" && tx.UserID == "user-1" &&
			tx.CustomerEmail == "user@example.com" && tx.AmountRupiah == 10000 &&
			tx.Currency == "IDR" && tx.Status == domain.StatusInitiated &&
			tx.ExpiresAt != nil && tx.ExpiresAt.Equal(expiresAt)
	})).Return(nil)

	txn, created, err := svc.InitiateTxnForBooking(ctx, "11111111-1111-4111-8111-111111111111", "event-1", "user-1", "user@example.com", 10000, expiresAt)
	require.NoError(t, err)
	assert.True(t, created, "first call for a booking must report created=true")
	assert.Equal(t, "11111111-1111-4111-8111-111111111111", txn.BookingID)
	assert.Equal(t, "IDR", txn.Currency)
}

func TestInitiateTxnForBooking_Idempotent(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	existing := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
	)

	txnRepo.EXPECT().Create(ctx, mock.Anything).Return(&pgconn.PgError{Code: "23505", Message: "duplicate key"})
	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(existing, nil)

	txn, created, err := svc.InitiateTxnForBooking(ctx, "11111111-1111-4111-8111-111111111111", "event-1", "user-1", "", 10000, time.Now().Add(15*time.Minute))
	require.NoError(t, err)
	assert.False(t, created, "duplicate booking must report created=false (idempotent success)")
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
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
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
		ExpiresAt:      expiresAt,
	}

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	processor.EXPECT().CreateSession(ctx, "11111111-1111-4111-8111-111111111111", 10000, "IDR", expiresAt,
		[]string{"QRIS", "GOPAY"}, "user@example.com").Return(result, nil)
	txnRepo.EXPECT().TransitionIfInitiated(ctx, "txn-1", domain.StatusPending, "ps-1").Return(true, nil)
	txnRepo.EXPECT().UpdateSession(ctx, "txn-1", "ps-1", "https://checkout.example/ps-1").Return(nil)

	gotTxn, gotResult, err := svc.ProcessPayment(ctx, "11111111-1111-4111-8111-111111111111", "user-1")
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

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(nil, pgx.ErrNoRows)
	_, _, err := svc.ProcessPayment(ctx, "11111111-1111-4111-8111-111111111111", "user-1")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

func TestProcessPayment_AlreadyInitiated(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	_, _, err := svc.ProcessPayment(ctx, "11111111-1111-4111-8111-111111111111", "user-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestProcessPayment_AlreadyCompleted(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionStatus(domain.StatusCompleted),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	_, _, err := svc.ProcessPayment(ctx, "11111111-1111-4111-8111-111111111111", "user-1")
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
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionUserID("user-1"),
		fixtures.WithTransactionCurrency("IDR"),
		fixtures.WithTransactionStatus(domain.StatusInitiated),
		fixtures.WithTransactionExpiresAt(expiresAt),
	)
	result := &domain.SessionResult{ProviderRef: "ps-1", PaymentLinkURL: "https://checkout.example/ps-1"}

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	processor.EXPECT().CreateSession(ctx, "11111111-1111-4111-8111-111111111111", 10000, "IDR", expiresAt,
		[]string{"QRIS", "GOPAY"}, "").Return(result, nil)
	txnRepo.EXPECT().TransitionIfInitiated(ctx, "txn-1", domain.StatusPending, "ps-1").Return(false, nil)
	processor.EXPECT().CancelSession(ctx, "ps-1").Return(nil)

	_, _, err := svc.ProcessPayment(ctx, "11111111-1111-4111-8111-111111111111", "user-1")
	assert.ErrorIs(t, err, domain.ErrAlreadyProcessed)
}

func TestProcessPayment_Expired(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	past := time.Now().Add(-1 * time.Minute)
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionUserID("user-1"),
		fixtures.WithTransactionStatus(domain.StatusInitiated),
		fixtures.WithTransactionExpiresAt(past),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	_, _, err := svc.ProcessPayment(ctx, "11111111-1111-4111-8111-111111111111", "user-1")
	assert.ErrorIs(t, err, domain.ErrTransactionExpired)
}

// TestProcessPayment_SessionShorterThanGatewayMinimum covers the guard that
// keeps the booking hold and the gateway session aligned: whatever is left must
// still cover a full ten-minute gateway session, so a customer is never handed
// a session that outlives their seats — and never one Xendit would reject.
func TestProcessPayment_SessionShorterThanGatewayMinimum(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remaining time.Duration
	}{
		{"nine minutes", 9 * time.Minute},
		{"exactly ten minutes", 10 * time.Minute},
		{"thirty seconds", 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			txnRepo := mocks.NewMockTransactionRepository(t)
			svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
				mocks.NewMockEventConsumer(t))
			ctx := context.Background()
			txn := fixtures.NewTestTransaction(
				fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
				fixtures.WithTransactionUserID("user-1"),
				fixtures.WithTransactionStatus(domain.StatusInitiated),
				fixtures.WithTransactionExpiresAt(time.Now().Add(tc.remaining)),
			)

			txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
			_, _, err := svc.ProcessPayment(ctx, "11111111-1111-4111-8111-111111111111", "user-1")
			assert.ErrorIs(t, err, domain.ErrTransactionExpired)
		})
	}
}

func TestProcessPayment_WrongOwner(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionUserID("user-other"),
		fixtures.WithTransactionStatus(domain.StatusInitiated),
		fixtures.WithTransactionExpiresAt(time.Now().Add(15*time.Minute)),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	_, _, err := svc.ProcessPayment(ctx, "11111111-1111-4111-8111-111111111111", "user-1")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

// ── GetPaymentStatus ─────────────────────────────────────────────────────────

func TestGetPaymentStatus_Success(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionPaymentLinkURL("https://checkout.example/ps-1"),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	got, err := svc.GetPaymentStatus(ctx, "11111111-1111-4111-8111-111111111111")
	require.NoError(t, err)
	assert.Equal(t, "https://checkout.example/ps-1", got.PaymentLinkURL)
}

func TestGetPaymentStatus_NotFound(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(nil, pgx.ErrNoRows)
	_, err := svc.GetPaymentStatus(ctx, "11111111-1111-4111-8111-111111111111")
	assert.ErrorIs(t, err, domain.ErrTransactionNotFound)
}

// ── HandleSessionWebhook ─────────────────────────────────────────────────────

// webhookPayload builds a session webhook body. referenceID must be a UUID —
// the handler acknowledges non-UUID references before querying, mirroring how
// Postgres would reject them against the UUID booking_id column.
func webhookPayload(event, referenceID, sessionID, status string) []byte {
	return []byte(`{"event":"` + event + `","data":{"reference_id":"` + referenceID +
		`","payment_session_id":"` + sessionID + `","status":"` + status + `"}}`)
}

// TestHandleSessionWebhook_Completed covers the happy path: the transition
// and its payment.completed outbox row are written by one repository call
// (CompleteIfActive), so the two cannot drift apart.
func TestHandleSessionWebhook_Completed(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	txnRepo.EXPECT().CompleteIfActive(ctx, txn).Return(true, nil)

	err := svc.HandleSessionWebhook(ctx, webhookPayload("payment_session.completed", "11111111-1111-4111-8111-111111111111", "ps-1", "COMPLETED"))
	require.NoError(t, err)
}

// TestHandleSessionWebhook_Completed_PropagatesDBError pins the contract that
// makes the atomic write useful: if the transition+outbox transaction fails,
// the error reaches the handler, which answers 5xx so the gateway redelivers.
// Swallowing it here would leave a paid customer without tickets forever.
func TestHandleSessionWebhook_Completed_PropagatesDBError(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	txnRepo.EXPECT().CompleteIfActive(ctx, txn).Return(false, errors.New("db down"))

	err := svc.HandleSessionWebhook(ctx, webhookPayload("payment_session.completed", "11111111-1111-4111-8111-111111111111", "ps-1", "COMPLETED"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestHandleSessionWebhook_CompletedAfterExpired_Refund(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	refundRepo := mocks.NewMockRefundRepository(t)
	svc := newPaymentService(t, txnRepo, refundRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusExpired),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	txnRepo.EXPECT().CompleteIfActive(ctx, txn).Return(false, nil)
	refundRepo.EXPECT().Create(ctx, mock.MatchedBy(func(r *domain.RefundRequest) bool {
		return r.Reason == "late_payment" && r.IdempotencyKey == "late-payment:txn-1"
	})).Return(nil)
	txnRepo.EXPECT().UpdateRefundStatus(ctx, "txn-1", "pending").Return(nil)

	err := svc.HandleSessionWebhook(ctx, webhookPayload("payment_session.completed", "11111111-1111-4111-8111-111111111111", "ps-1", "COMPLETED"))
	require.NoError(t, err)
}

func TestHandleSessionWebhook_CompletedDuplicate(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusCompleted),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	txnRepo.EXPECT().CompleteIfActive(ctx, txn).Return(false, nil)

	err := svc.HandleSessionWebhook(ctx, webhookPayload("payment_session.completed", "11111111-1111-4111-8111-111111111111", "ps-1", "COMPLETED"))
	require.NoError(t, err)
}

func TestHandleSessionWebhook_Expired(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	txnRepo.EXPECT().ExpireIfActive(ctx, txn, domain.GatewayExpiredReason).Return(true, nil)

	err := svc.HandleSessionWebhook(ctx, webhookPayload("payment_session.expired", "11111111-1111-4111-8111-111111111111", "ps-1", "EXPIRED"))
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
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionProviderRef("ps-579c8d61f23fa4ca35e52da4"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)
	txnRepo.EXPECT().CompleteIfActive(ctx, txn).Return(true, nil)

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
			"reference_id": "11111111-1111-4111-8111-111111111111",
			"session_type": "SAVE",
			"status": "COMPLETED",
			"updated": "2020-04-20T16:25:52Z"
		}
	}`)

	err := svc.HandleSessionWebhook(ctx, payload)
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
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionProviderRef("ps-abc"),
		fixtures.WithTransactionStatus(domain.StatusPending),
	)

	txnRepo.EXPECT().FindByBookingID(ctx, "11111111-1111-4111-8111-111111111111").Return(txn, nil)

	payload := []byte(`{"event":"payment_session.completed","data":{"payment_session_id":"ps-some-other-session","reference_id":"11111111-1111-4111-8111-111111111111","status":"COMPLETED"}}`)
	err := svc.HandleSessionWebhook(ctx, payload)
	require.NoError(t, err)
}

func TestHandleSessionWebhook_TxnMissing_ReturnsError(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()

	txnRepo.EXPECT().FindByBookingID(ctx, "22222222-2222-4222-8222-222222222222").Return(nil, pgx.ErrNoRows)
	err := svc.HandleSessionWebhook(ctx, webhookPayload("payment_session.completed", "22222222-2222-4222-8222-222222222222", "ps-1", "COMPLETED"))
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

// TestHandleSessionWebhook_NonBookingReference_Acknowledged covers gateway
// test payloads (and sessions created outside our flow): their reference_id is
// not one of our booking UUIDs, which Postgres would reject with a syntax
// error rather than ErrNoRows. They are acknowledged — no repo call, no error.
func TestHandleSessionWebhook_NonBookingReference_Acknowledged(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	svc := newPaymentServiceDefaults(t, txnRepo, mocks.NewMockPaymentProcessor(t),
		mocks.NewMockEventConsumer(t))
	ctx := context.Background()

	err := svc.HandleSessionWebhook(ctx, webhookPayload("payment_session.expired", "test_session", "ps-1", "EXPIRED"))
	require.NoError(t, err)
}

func TestHandleSessionWebhook_MissingReference(t *testing.T) {
	svc := newPaymentServiceDefaults(t, mocks.NewMockTransactionRepository(t),
		mocks.NewMockPaymentProcessor(t), mocks.NewMockEventConsumer(t))
	ctx := context.Background()

	err := svc.HandleSessionWebhook(ctx, []byte(`{"event":"payment_session.completed","data":{"status":"COMPLETED"}}`))
	require.Error(t, err)
}

// ── ProcessExpired ───────────────────────────────────────────────────────────

// TestProcessExpired_LostWebhookCancelsSession covers the safety net for a
// pending transaction whose gateway expiry webhook never arrived: the sweeper
// cancels the orphaned session and expires the row.
func TestProcessExpired_LostWebhookCancelsSession(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	svc := newPaymentServiceDefaults(t, txnRepo, processor, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	past := time.Now().Add(-1 * time.Minute)
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionProviderRef("ps-1"),
		fixtures.WithTransactionStatus(domain.StatusPending),
		fixtures.WithTransactionExpiresAt(past),
	)

	txnRepo.EXPECT().ListSweepableExpired(ctx, mock.Anything, application.PendingExpiryGrace, 100).
		Return([]domain.Transaction{*txn}, nil)
	processor.EXPECT().CancelSession(ctx, "ps-1").Return(nil)
	txnRepo.EXPECT().ExpireIfActive(ctx, txn, application.InternalExpiryFallbackReason).Return(true, nil)

	err := svc.ProcessExpired(ctx, 100)
	require.NoError(t, err)
}

// TestProcessExpired_NeverInitiated_SkipsGateway covers the abandoned-cart
// case: no session was ever created, so there is nothing to cancel at the
// gateway and the sweeper is the only thing that frees the seats.
func TestProcessExpired_NeverInitiated_SkipsGateway(t *testing.T) {
	txnRepo := mocks.NewMockTransactionRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	svc := newPaymentServiceDefaults(t, txnRepo, processor, mocks.NewMockEventConsumer(t))
	ctx := context.Background()
	past := time.Now().Add(-1 * time.Minute)
	txn := fixtures.NewTestTransaction(
		fixtures.WithTransactionID("txn-1"),
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
		fixtures.WithTransactionStatus(domain.StatusInitiated),
		fixtures.WithTransactionExpiresAt(past),
	)

	txnRepo.EXPECT().ListSweepableExpired(ctx, mock.Anything, application.PendingExpiryGrace, 100).
		Return([]domain.Transaction{*txn}, nil)
	txnRepo.EXPECT().ExpireIfActive(ctx, txn, application.InternalExpiryFallbackReason).Return(true, nil)

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
		fixtures.WithTransactionBookingID("11111111-1111-4111-8111-111111111111"),
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

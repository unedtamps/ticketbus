//go:build integration

package integration

// payout_test.go — payment-side payout consumption, aggregation, idempotency,
// failure and retry. The gateway is the in-process MockProcessor, so the
// dispatch loop and outbox are exercised for real.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// completeTransaction seeds a transaction for eventID and drives it to
// completed through the mock gateway.
func (env *TestEnv) completeTransaction(t *testing.T, c customer, eventID string, amount int) string {
	t.Helper()
	bookingID := newBookingID()
	env.seedTxn(t, c, bookingID, eventID, amount, time.Hour)
	env.initiate(t, c, bookingID)
	env.postCompletedWebhook(t, bookingID)
	require.Equal(t, "completed", env.txnStatusByBooking(t, bookingID))
	return bookingID
}

func (env *TestEnv) emitPayoutRequested(t *testing.T, eventID string, attempt int) {
	t.Helper()
	require.NoError(t, env.infra.Producer.Produce(
		context.Background(),
		dto.TopicPayoutRequested,
		eventID,
		dto.PayoutRequested{
			EventID:               eventID,
			OrganizerID:           newEventID(),
			PayoutID:              newBookingID(),
			Attempt:               attempt,
			BankCode:              "BCA",
			BankAccountNumber:     "1234567890",
			BankAccountHolderName: "Integration EO",
			At:                    time.Now(),
		},
	))
}

type payoutRow struct {
	status        string
	amount        int64
	providerRef   string
	failureReason string
	attempt       int
}

func (env *TestEnv) payoutAttempt(t *testing.T, eventID string, attempt int) (payoutRow, error) {
	var p payoutRow
	err := env.payPool.QueryRow(context.Background(), `
		SELECT status, amount_rupiah, COALESCE(provider_ref, ''),
		       COALESCE(failure_reason, ''), attempt
		FROM payouts WHERE event_id = $1 AND attempt = $2`, eventID, attempt).
		Scan(&p.status, &p.amount, &p.providerRef, &p.failureReason, &p.attempt)
	return p, err
}

func (env *TestEnv) tryPayoutForEvent(eventID string) (payoutRow, error) {
	var p payoutRow
	err := env.payPool.QueryRow(context.Background(), `
		SELECT status, amount_rupiah, COALESCE(provider_ref, ''),
		       COALESCE(failure_reason, ''), attempt
		FROM payouts WHERE event_id = $1 ORDER BY attempt DESC LIMIT 1`, eventID).
		Scan(&p.status, &p.amount, &p.providerRef, &p.failureReason, &p.attempt)
	return p, err
}

func (env *TestEnv) payoutForEvent(t *testing.T, eventID string) payoutRow {
	t.Helper()
	p, err := env.tryPayoutForEvent(eventID)
	require.NoError(t, err)
	return p
}

func (env *TestEnv) waitPayoutStatus(t *testing.T, eventID, want string, timeout time.Duration) payoutRow {
	t.Helper()
	last := payoutRow{status: "not created"}
	var lastErr error
	pollFor(t, timeout, 500*time.Millisecond, func() bool {
		p, err := env.tryPayoutForEvent(eventID)
		if err != nil {
			lastErr = err
			return false
		}
		last = p
		return last.status == want
	}, "payout for event "+eventID+" to become "+want)
	t.Logf("last payout state: %s (err %v)", last.status, lastErr)
	return env.payoutForEvent(t, eventID)
}

func (env *TestEnv) txnPayoutStatus(t *testing.T, bookingID string) string {
	t.Helper()
	var s string
	require.NoError(t, env.payPool.QueryRow(context.Background(),
		`SELECT payout_status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&s))
	return s
}

func TestPayout_Consumer_PaysOutEligibleTransactions(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	eventID := newEventID()

	first := env.completeTransaction(t, c, eventID, 10000)
	second := env.completeTransaction(t, c, eventID, 5000)

	// Not completed: excluded.
	pendingBooking := newBookingID()
	env.seedTxn(t, c, pendingBooking, eventID, 7000, time.Hour)
	env.initiate(t, c, pendingBooking)

	// Refund in progress: excluded as well.
	refunded := env.completeTransaction(t, c, eventID, 3000)
	_, err := env.payPool.Exec(context.Background(),
		`UPDATE transactions SET refund_status='pending' WHERE booking_id=$1`, refunded)
	require.NoError(t, err)

	env.emitPayoutRequested(t, eventID, 1)

	p := env.waitPayoutStatus(t, eventID, "paid_out", 30*time.Second)
	assert.EqualValues(t, 15000, p.amount)
	assert.True(t, strings.HasPrefix(p.providerRef, "dis-mock-"), "provider_ref = %q", p.providerRef)
	assert.Equal(t, 1, p.attempt)

	assert.Equal(t, "paid_out", env.txnPayoutStatus(t, first))
	assert.Equal(t, "paid_out", env.txnPayoutStatus(t, second))
	assert.Equal(t, "none", env.txnPayoutStatus(t, pendingBooking))
	assert.Equal(t, "none", env.txnPayoutStatus(t, refunded))
}

func TestPayout_Consumer_DuplicateDelivery_SinglePayout(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	eventID := newEventID()
	env.completeTransaction(t, c, eventID, 10000)

	before := env.mock.PayoutCalls()
	env.emitPayoutRequested(t, eventID, 1)
	env.emitPayoutRequested(t, eventID, 1)

	p := env.waitPayoutStatus(t, eventID, "paid_out", 30*time.Second)
	assert.EqualValues(t, 10000, p.amount)
	assert.Equal(t, 1, env.mock.PayoutCalls()-before, "duplicate delivery must not call the gateway twice")

	var rows int
	require.NoError(t, env.payPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM payouts WHERE event_id=$1`, eventID).Scan(&rows))
	assert.Equal(t, 1, rows, "a repeated delivery must not create a second ledger row")
}

func TestPayout_Consumer_FailureThenRetry(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	eventID := newEventID()
	bookingID := env.completeTransaction(t, c, eventID, 10000)

	env.mock.SetPayoutError(&domain.PermanentProcessorError{Err: errors.New("invalid account")})
	defer env.mock.SetPayoutError(nil)

	env.emitPayoutRequested(t, eventID, 1)

	failed := env.waitPayoutStatus(t, eventID, "failed", 30*time.Second)
	assert.Contains(t, failed.failureReason, "invalid account")
	assert.Equal(t, "failed", env.txnPayoutStatus(t, bookingID))

	// Fix the account (the mock) and retry with a new attempt: the failed
	// transaction is included again.
	env.mock.SetPayoutError(nil)
	env.emitPayoutRequested(t, eventID, 2)

	paid := env.waitPayoutStatus(t, eventID, "paid_out", 30*time.Second)
	assert.Equal(t, 2, paid.attempt)
	assert.EqualValues(t, 10000, paid.amount)
	assert.Equal(t, "paid_out", env.txnPayoutStatus(t, bookingID))
}

func TestPayout_Consumer_NoEarnings(t *testing.T) {
	env := getTestEnv()
	eventID := newEventID()

	env.emitPayoutRequested(t, eventID, 1)

	p := env.waitPayoutStatus(t, eventID, "failed", 20*time.Second)
	assert.Equal(t, "no_earnings", p.failureReason)
	assert.Zero(t, p.amount)

	// No gateway job was written for an event with nothing to pay.
	var disbursements int
	require.NoError(t, env.payPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM outbox WHERE topic=$1 AND key=$2`,
		dto.TopicPaymentDisbursement, eventID).Scan(&disbursements))
	assert.Zero(t, disbursements)
}

//go:build integration

package integration

// refund_executor_test.go — TestRefundExecutor_*: the full executor path from
// event cancellation to a settled payout, including the transaction ledger
// mirror and the customer-facing refund list.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerRefundAccount saves a payout destination for the customer, which is
// what unblocks the executor's sweep.
func (env *TestEnv) registerRefundAccount(t *testing.T, c customer, channel, number, holder string) {
	t.Helper()
	resp, respBody, err := doJSON("PUT", env.payURL+"/api/refund-account", map[string]string{
		"channel_code":        channel,
		"account_number":      number,
		"account_holder_name": holder,
	}, c.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "save refund account: %s", respBody)
}

// TestRefundExecutor_SettlesAndMirrorsLedger covers the whole chain: a
// cancelled event creates the refund, the customer registers a destination,
// the executor pays it out, and both refund_requests and transactions end up
// settled — with refund.completed emitted for downstream consumers.
func TestRefundExecutor_SettlesAndMirrorsLedger(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	bookingID, eventID := newBookingID(), newEventID()
	env.seedTxn(t, c, bookingID, eventID, defaultAmount, time.Hour)
	env.initiate(t, c, bookingID)

	env.postCompletedWebhook(t, bookingID)
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.txnStatusByBooking(t, bookingID) == "completed"
	}, "txn completed before event cancellation")

	txnID := env.txnIDByBooking(t, bookingID)
	env.emitEventCancelled(t, eventID)
	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		return env.pendingRefundCount(t, txnID, "event_cancelled") == 1
	}, "refund request created")

	// Without a destination the sweep must leave the refund pending.
	time.Sleep(3 * time.Second)
	assert.Equal(t, "pending", env.refundStatus(t, txnID),
		"a refund with no destination must stay pending")

	// Registering the destination unblocks the executor. The mock processor
	// settles refund payouts synchronously, so one sweep is enough.
	env.registerRefundAccount(t, c, "BCA", "1234567890", "Cust QA")

	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		return env.refundStatus(t, txnID) == "succeeded"
	}, "transaction ledger mirrors the settled refund")

	assert.Equal(t, "succeeded", env.refundRequestStatus(t, txnID),
		"refund_requests settles as succeeded")
	// refund.completed is keyed by booking, and the outbox worker has already
	// published it (delivered=true), so count without filtering on that.
	assert.Equal(t, 1, env.refundCompletedCount(t, bookingID),
		"refund.completed emitted exactly once")
}

// TestRefundExecutor_CustomerList is the customer-facing view: GET /api/refunds
// returns the caller's own refunds with the destination, and never another
// customer's.
func TestRefundExecutor_CustomerList(t *testing.T) {
	env := getTestEnv()
	owner := newCustomer()
	other := newCustomer()

	bookingID, eventID := newBookingID(), newEventID()
	env.seedTxn(t, owner, bookingID, eventID, defaultAmount, time.Hour)
	env.initiate(t, owner, bookingID)
	env.postCompletedWebhook(t, bookingID)
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.txnStatusByBooking(t, bookingID) == "completed"
	}, "txn completed")

	env.emitEventCancelled(t, eventID)
	txnID := env.txnIDByBooking(t, bookingID)
	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		return env.pendingRefundCount(t, txnID, "event_cancelled") == 1
	}, "refund request created")

	env.registerRefundAccount(t, owner, "GOPAY", "081234567890", "Cust QA")

	type refundRow struct {
		BookingID     string `json:"booking_id"`
		Status        string `json:"status"`
		ChannelCode   string `json:"channel_code"`
		AccountNumber string `json:"account_number"`
		HasAccount    bool   `json:"has_account"`
		AmountRupiah  int64  `json:"amount_rupiah"`
	}
	type listResponse struct {
		Data []refundRow `json:"data"`
	}

	var mine []refundRow
	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		resp, body, err := doJSON("GET", env.payURL+"/api/refunds", nil, owner.headers)
		if err != nil || resp.StatusCode != http.StatusOK {
			return false
		}
		var env listResponse
		if json.Unmarshal(body, &env) != nil {
			return false
		}
		mine = env.Data
		return len(env.Data) > 0 && env.Data[0].Status == "succeeded"
	}, "customer refund list shows the settled refund")

	require.NotEmpty(t, mine)
	assert.Equal(t, bookingID, mine[0].BookingID)
	assert.Equal(t, "GOPAY", mine[0].ChannelCode)
	assert.Equal(t, "081234567890", mine[0].AccountNumber)
	assert.True(t, mine[0].HasAccount)
	assert.EqualValues(t, defaultAmount, mine[0].AmountRupiah)

	// Another customer's list must not include this refund.
	resp, body, err := doJSON("GET", env.payURL+"/api/refunds", nil, other.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var theirs listResponse
	require.NoError(t, json.Unmarshal(body, &theirs))
	assert.Empty(t, theirs.Data, "a customer must never see someone else's refund")
}

// refundRequestStatus reads the refund_requests row for a transaction.
func (env *TestEnv) refundRequestStatus(t *testing.T, txnID string) string {
	t.Helper()
	var s string
	require.NoError(t, env.payPool.QueryRow(context.Background(),
		`SELECT status FROM refund_requests WHERE transaction_id = $1`, txnID).Scan(&s))
	return s
}

// refundCompletedCount counts refund.completed outbox rows for a booking. The
// key column carries the booking id, not the transaction id.
func (env *TestEnv) refundCompletedCount(t *testing.T, bookingID string) int {
	t.Helper()
	var n int
	require.NoError(t, env.payPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM outbox WHERE topic = 'refund.completed' AND key = $1`,
		bookingID).Scan(&n))
	return n
}

// deliverRefundPayoutCallback sends a gateway payout callback for a refund,
// addressed by the payout id the executor recorded.
func (env *TestEnv) deliverRefundPayoutCallback(t *testing.T, providerRef, status, failureCode string) {
	t.Helper()
	data := map[string]interface{}{
		"payout_id": providerRef,
		"status":    status,
	}
	if failureCode != "" {
		data["failure_code"] = failureCode
	}
	code, body := env.postPayoutWebhook(t, map[string]interface{}{
		"event": "v3_payout." + strings.ToLower(status),
		"data":  data,
	}, webhookToken)
	require.Equal(t, http.StatusOK, code, "payout webhook: %s", body)
}

// TestRefundWebhook_ReversedIsFailed covers the money-correctness case: a
// REVERSED payout means the bank returned the funds to the platform, so the
// customer never kept the money. Recording it as succeeded would report a
// refund that did not happen.
func TestRefundWebhook_ReversedIsFailed(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	bookingID, eventID := newBookingID(), newEventID()
	env.seedTxn(t, c, bookingID, eventID, defaultAmount, time.Hour)
	env.initiate(t, c, bookingID)
	env.postCompletedWebhook(t, bookingID)
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.txnStatusByBooking(t, bookingID) == "completed"
	}, "txn completed")

	txnID := env.txnIDByBooking(t, bookingID)
	env.emitEventCancelled(t, eventID)
	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		return env.pendingRefundCount(t, txnID, "event_cancelled") == 1
	}, "refund request created")

	// The mock settles payouts synchronously, so the refund reaches succeeded
	// without a webhook. Drive the reversed case directly against a processing
	// row instead: reset it, then deliver the callback.
	env.registerRefundAccount(t, c, "BCA", "1234567890", "Cust QA")
	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		return env.refundRequestStatus(t, txnID) == "succeeded"
	}, "refund settles first")

	// Rewind to processing with a payout reference so the webhook has a target.
	providerRef := "dis-mock-" + env.refundIDByTxn(t, txnID)
	require.NoError(t, func() error {
		_, err := env.payPool.Exec(context.Background(), `
			UPDATE refund_requests
			SET status='processing', provider_ref=$2, processed_at=NULL
			WHERE transaction_id=$1`, txnID, providerRef)
		return err
	}())

	env.deliverRefundPayoutCallback(t, providerRef, "REVERSED", "")

	pollFor(t, 20*time.Second, 300*time.Millisecond, func() bool {
		return env.refundRequestStatus(t, txnID) == "failed"
	}, "reversed payout settles the refund as failed")

	assert.Equal(t, "failed", env.refundStatus(t, txnID),
		"the transaction ledger must not report a reversed refund as succeeded")
	assert.Contains(t, env.refundFailureReason(t, txnID), "ditarik kembali",
		"the customer needs to know the bank took the money back")
}

// refundIDByTxn resolves the refund id for a transaction.
func (env *TestEnv) refundIDByTxn(t *testing.T, txnID string) string {
	t.Helper()
	var id string
	require.NoError(t, env.payPool.QueryRow(context.Background(),
		`SELECT id FROM refund_requests WHERE transaction_id = $1`, txnID).Scan(&id))
	return id
}

// refundFailureReason reads the customer-facing failure reason.
func (env *TestEnv) refundFailureReason(t *testing.T, txnID string) string {
	t.Helper()
	var reason string
	require.NoError(t, env.payPool.QueryRow(context.Background(),
		`SELECT COALESCE(failure_reason,'') FROM refund_requests WHERE transaction_id = $1`,
		txnID).Scan(&reason))
	return reason
}

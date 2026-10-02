//go:build integration

package integration

// payment_test.go — TestPayment_*: session creation, idempotency, and ownership.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPayment_Initiate_AndWebhookCompletes verifies the full settle path: a
// seeded transaction is initiated, the gateway reports completion, and the
// booking-scoped status endpoint reflects it.
func TestPayment_Initiate_AndWebhookCompletes(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	bookingID := newBookingID()
	env.seedTxn(t, c, bookingID, newEventID(), defaultAmount, time.Hour)

	// The transaction row exists as soon as it is seeded, in initiated state.
	assert.Equal(t, "initiated", env.txnStatusByBooking(t, bookingID))

	sessionID, link := env.initiate(t, c, bookingID)
	require.NotEmpty(t, sessionID)
	require.NotEmpty(t, link)

	env.postCompletedWebhook(t, bookingID)

	resp, body, err := doJSON(http.MethodGet, env.payURL+"/api/payments/booking/"+bookingID, nil, c.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var ps struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &ps))
	assert.Equal(t, "completed", ps.Data.Status)
}

func TestPayment_Initiate_Duplicate_Idempotent(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	bookingID, _ := env.seedTxnAndInitiate(t, c, defaultAmount)

	// A second initiate for the same booking conflicts.
	resp, body, err := doJSON(http.MethodPost, env.payURL+"/api/payments/booking/"+bookingID, nil, c.headers)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "second initiate should return 409: %s", body)
}

func TestPayment_Initiate_NonexistentBooking(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	resp, _, err := doJSON(http.MethodPost, env.payURL+"/api/payments/booking/nonexistent-id", nil, c.headers)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPayment_Initiate_StaysPendingWithoutWebhook(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	bookingID, _ := env.seedTxnAndInitiate(t, c, defaultAmount)

	// NOTE: intentionally no webhook is delivered.
	resp, body, err := doJSON(http.MethodGet, env.payURL+"/api/payments/booking/"+bookingID, nil, c.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var ps struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &ps))
	assert.Equal(t, "pending", ps.Data.Status)
}

func TestPayment_Initiate_Rejected_LessThanMinute(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	bookingID := newBookingID()
	env.seedTxn(t, c, bookingID, newEventID(), defaultAmount, time.Hour)

	// Leave less than the one-minute floor the handler enforces.
	_, err := env.payPool.Exec(context.Background(),
		`UPDATE transactions SET expires_at = NOW() + INTERVAL '30 seconds' WHERE booking_id = $1`, bookingID)
	require.NoError(t, err)

	resp, body, err := doJSON(http.MethodPost, env.payURL+"/api/payments/booking/"+bookingID, nil, c.headers)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "initiate should be rejected: %s", body)
}

// TestPayment_GetTransaction_Ownership verifies the transaction detail endpoint
// returns the full record, including the payment link, only to its owner.
func TestPayment_GetTransaction_Ownership(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	other := newCustomer()

	bookingID, _ := env.seedTxnAndInitiate(t, c, defaultAmount)

	resp, body, err := doJSON(http.MethodGet, env.payURL+"/api/payments/booking/"+bookingID, nil, c.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)
	var status struct {
		Data struct {
			TransactionID string `json:"transaction_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &status))
	require.NotEmpty(t, status.Data.TransactionID)

	// The owner sees the whole transaction.
	resp, body, err = doJSON(http.MethodGet, env.payURL+"/api/payments/"+status.Data.TransactionID, nil, c.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)
	var txn struct {
		Data struct {
			ID             string `json:"id"`
			BookingID      string `json:"booking_id"`
			AmountRupiah   int    `json:"amount_rupiah"`
			Currency       string `json:"currency"`
			Status         string `json:"status"`
			PaymentLinkURL string `json:"payment_link_url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &txn))
	assert.Equal(t, status.Data.TransactionID, txn.Data.ID)
	assert.Equal(t, bookingID, txn.Data.BookingID)
	assert.Equal(t, defaultAmount, txn.Data.AmountRupiah)
	assert.Equal(t, "IDR", txn.Data.Currency)
	assert.Equal(t, "pending", txn.Data.Status)
	assert.NotEmpty(t, txn.Data.PaymentLinkURL)

	// Another customer cannot read it.
	resp, _, err = doJSON(http.MethodGet, env.payURL+"/api/payments/"+status.Data.TransactionID, nil, other.headers)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

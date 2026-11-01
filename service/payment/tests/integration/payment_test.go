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

// TestPayment_InternalCreate_Duplicate_Idempotent verifies the internal
// endpoint the ticketing REST dispatcher calls is idempotent per booking:
// the first call creates the transaction (201, created=true); a redelivered
// call returns the same transaction untouched (200, created=false) so the
// dispatcher can treat any 2xx as delivered.
func TestPayment_InternalCreate_Duplicate_Idempotent(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	bookingID := newBookingID()
	payload := map[string]interface{}{
		"booking_id":    bookingID,
		"event_id":      newEventID(),
		"user_id":       c.id,
		"email":         c.email,
		"amount_rupiah": defaultAmount,
		"expires_at":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	headers := map[string]string{"X-Internal-Key": internalKey}

	type createResp struct {
		Data struct {
			TransactionID string `json:"transaction_id"`
			BookingID     string `json:"booking_id"`
			Created       bool   `json:"created"`
		} `json:"data"`
	}

	// First call: created.
	resp, body, err := doJSON(http.MethodPost, env.payURL+"/api/payments/internal", payload, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "first internal create: %s", body)
	var first createResp
	require.NoError(t, json.Unmarshal(body, &first), "body: %s", body)
	assert.True(t, first.Data.Created)
	assert.Equal(t, bookingID, first.Data.BookingID)
	require.NotEmpty(t, first.Data.TransactionID)

	// Second call (e.g. at-least-once redelivery): idempotent success.
	resp, body, err = doJSON(http.MethodPost, env.payURL+"/api/payments/internal", payload, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "duplicate internal create: %s", body)
	var second createResp
	require.NoError(t, json.Unmarshal(body, &second), "body: %s", body)
	assert.False(t, second.Data.Created, "duplicate must report created=false")
	assert.Equal(t, first.Data.TransactionID, second.Data.TransactionID,
		"duplicate must return the existing transaction id")

	// The transaction is still a single, untouched initiated row.
	assert.Equal(t, "initiated", env.txnStatusByBooking(t, bookingID))
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

// TestPayment_Initiate_Rejected_LessThanGatewayMinimum verifies a session is
// refused once less than the gateway's ten-minute minimum remains: the hold and
// the session end together, so a shorter session could not be issued and a
// longer one would outlive the seats.
func TestPayment_Initiate_Rejected_LessThanGatewayMinimum(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	bookingID := newBookingID()
	env.seedTxn(t, c, bookingID, newEventID(), defaultAmount, time.Hour)

	_, err := env.payPool.Exec(context.Background(),
		`UPDATE transactions SET expires_at = NOW() + INTERVAL '9 minutes' WHERE booking_id = $1`, bookingID)
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

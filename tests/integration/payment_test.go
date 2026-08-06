//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_InitiateAndWebhookCompletesPayment(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	// Reserve → transaction is created synchronously by the payment service
	_, body, err := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_rupiah": 10000},
		},
	}, ch)
	require.NoError(t, err)

	var rr reserveResp
	require.NoError(t, json.Unmarshal(body, &rr))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// The transaction row must exist immediately (sync reserve), initiated.
	var txnStatus string
	require.NoError(t, env.payPool.QueryRow(
		context.Background(), `SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&txnStatus))
	assert.Equal(t, "initiated", txnStatus)

	// Initiate payment → payment session with checkout link
	sessionID, link := initiatePayment(t, env, bookingID, ch)
	require.NotEmpty(t, sessionID)
	require.NotEmpty(t, link)

	// Simulate webhook callback (provider's async POST)
	completePaymentWebhook(t, env, bookingID)

	// Verify transaction status via the booking endpoint
	resp, body, err := doJSON(http.MethodGet, env.payURL+"/api/payments/booking/"+bookingID, nil, ch)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var ps struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &ps))
	assert.Equal(t, "completed", ps.Data.Status)
}

func Test_DuplicateInitiateIsIdempotent(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	_, body, _ := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_rupiah": 10000}},
	}, ch)
	var rr reserveResp
	require.NoError(t, json.Unmarshal(body, &rr))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// First initiate succeeds
	resp, _, err := doJSON(http.MethodPost, env.payURL+"/api/payments/booking/"+bookingID, nil, ch)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	// Second initiate → conflict (already initiated)
	resp, body2, err := doJSON(http.MethodPost, env.payURL+"/api/payments/booking/"+bookingID, nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 409, resp.StatusCode, "second initiate should return 409: %s", string(body2))
}

func Test_PaymentForNonexistentBooking(t *testing.T) {
	env := getTestEnv()

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	resp, _, err := doJSON(http.MethodPost, env.payURL+"/api/payments/booking/nonexistent-id", nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}

func Test_PaymentStaysPendingWhenWebhookNotCalled(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	_, body, _ := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_rupiah": 10000}},
	}, ch)
	var rr reserveResp
	require.NoError(t, json.Unmarshal(body, &rr))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Initiate without paying
	initiatePayment(t, env, bookingID, ch)

	// NOTE: intentionally NOT calling the webhook.
	resp, body, err := doJSON(http.MethodGet, env.payURL+"/api/payments/booking/"+bookingID, nil, ch)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var ps struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &ps))
	assert.Equal(t, "pending", ps.Data.Status)
}

func Test_InitiateRejectedWhenLessThanOneMinuteRemains(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	_, body, _ := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_rupiah": 10000}},
	}, ch)
	var rr reserveResp
	require.NoError(t, json.Unmarshal(body, &rr))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Push the transaction deadline into the near future (< 1 minute left)
	ctx := t.Context()
	_, err := env.payPool.Exec(ctx,
		`UPDATE transactions SET expires_at = NOW() + INTERVAL '30 seconds' WHERE booking_id = $1`, bookingID)
	require.NoError(t, err)

	resp, body2, err := doJSON(http.MethodPost, env.payURL+"/api/payments/booking/"+bookingID, nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 409, resp.StatusCode, "initiate should be rejected: %s", string(body2))
}

// Test_GetTransactionByID_Ownership verifies the payment page endpoint returns
// the full transaction (with payment link) only to its owner.
func Test_GetTransactionByID_Ownership(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	other := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	bookingID, _ := reserveAndInitiate(t, env, eventID, ttIDs[0], 1, ch)

	// Resolve the transaction id from the booking status endpoint.
	resp, body, err := doJSON(http.MethodGet, env.payURL+"/api/payments/booking/"+bookingID, nil, ch)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode, "%s", string(body))
	var status struct {
		Data struct {
			TransactionID string `json:"transaction_id"`
		} `json:"data"`
	}
	require.NoError(t, jsonData(body, &status))
	require.NotEmpty(t, status.Data.TransactionID)

	// Owner can read the full transaction including the payment link.
	resp, body, err = doJSON(http.MethodGet, env.payURL+"/api/payments/"+status.Data.TransactionID, nil, ch)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode, "%s", string(body))
	var txn struct {
		Data struct {
			ID             string `json:"id"`
			BookingID      string `json:"booking_id"`
			AmountRupiah    int    `json:"amount_rupiah"`
			Currency       string `json:"currency"`
			Status         string `json:"status"`
			PaymentLinkURL string `json:"payment_link_url"`
		} `json:"data"`
	}
	require.NoError(t, jsonData(body, &txn))
	assert.Equal(t, status.Data.TransactionID, txn.Data.ID)
	assert.Equal(t, bookingID, txn.Data.BookingID)
	assert.Equal(t, 10000, txn.Data.AmountRupiah)
	assert.Equal(t, "IDR", txn.Data.Currency)
	assert.Equal(t, "pending", txn.Data.Status)
	assert.NotEmpty(t, txn.Data.PaymentLinkURL)

	// Another customer cannot read it.
	resp, _, err = doJSON(http.MethodGet, env.payURL+"/api/payments/"+status.Data.TransactionID, nil, env.authHeadersWith(other.AccessToken))
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}

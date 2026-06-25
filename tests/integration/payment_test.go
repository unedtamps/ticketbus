//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_CheckoutAndWebhookCompletesPayment(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	// Reserve
	_, body, err := doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_cents": 10000},
		},
	}, ch)
	require.NoError(t, err)

	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Poll checkout until transaction is created by payment consumer
	var tr struct {
		Data struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			BookingID string `json:"booking_id"`
		} `json:"data"`
	}
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		resp, b, _ := doJSON(http.MethodPost, env.payURL+"/api/payments/by-booking/"+bookingID+"/checkout", nil, ch)
		if resp == nil || resp.StatusCode != 200 {
			return false
		}
		json.Unmarshal(b, &tr)
		return tr.Data.ID != ""
	}, "payment transaction init via outbox → Kafka → payment consumer")

	txnID := tr.Data.ID
	require.NotEmpty(t, txnID)

	// Simulate webhook callback (provider's async POST)
	resp, body, err := doJSON(http.MethodPost, env.payURL+"/api/payments/webhook/mock", map[string]string{
		"transaction_id": txnID,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode, "webhook: %s", string(body))

	// Verify status
	resp, body, err = doJSON(http.MethodGet, env.payURL+"/api/payments/"+txnID+"/status", nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var ts struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	json.Unmarshal(body, &ts)
	assert.Equal(t, "completed", ts.Data.Status)
}

func Test_DuplicateBookingTransactionIsIdempotent(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	// Reserve
	_, body, _ := doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_cents": 10000}},
	}, ch)
	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Poll checkout until transaction is created by Kafka consumer
	var tr struct {
		Data struct{ ID string `json:"id"` } `json:"data"`
	}
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		resp, b, _ := doJSON(http.MethodPost, env.payURL+"/api/payments/by-booking/"+bookingID+"/checkout", nil, ch)
		if resp == nil || resp.StatusCode != 200 {
			return false
		}
		json.Unmarshal(b, &tr)
		return tr.Data.ID != ""
	}, "payment transaction init via outbox → Kafka → payment consumer")

	require.NotEmpty(t, tr.Data.ID, "first checkout returned no transaction ID")

	// Second checkout on same booking should return conflict (already processed)
	resp, body2, err := doJSON(http.MethodPost, env.payURL+"/api/payments/by-booking/"+bookingID+"/checkout", nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 409, resp.StatusCode, "second checkout should return 409: %s", string(body2))
}

func Test_PaymentForNonexistentBooking(t *testing.T) {
	env := getTestEnv()

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	resp, _, err := doJSON(http.MethodPost, env.payURL+"/api/payments/by-booking/nonexistent-id/checkout", nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}

func Test_PaymentStaysProcessingWhenWebhookNotCalled(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	// Reserve
	_, body, _ := doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_cents": 10000}},
	}, ch)
	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Poll checkout until transaction is created
	var tr struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		resp, b, _ := doJSON(http.MethodPost, env.payURL+"/api/payments/by-booking/"+bookingID+"/checkout", nil, ch)
		if resp == nil || resp.StatusCode != 200 {
			return false
		}
		json.Unmarshal(b, &tr)
		return tr.Data.ID != ""
	}, "payment transaction init via outbox → Kafka → payment consumer")

	txnID := tr.Data.ID
	require.NotEmpty(t, txnID)

	// NOTE: intentionally NOT calling the webhook.
	// Status should be "processing" — not "completed".
	resp, body, err := doJSON(http.MethodGet, env.payURL+"/api/payments/"+txnID+"/status", nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var ts struct {
		Data struct{ Status string `json:"status"` } `json:"data"`
	}
	json.Unmarshal(body, &ts)
	assert.Equal(t, "processing", ts.Data.Status)
}

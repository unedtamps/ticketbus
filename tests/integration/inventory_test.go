//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reserveResp struct {
	Data struct {
		BookingID  string `json:"booking_id"`
		EventID    string `json:"event_id"`
		Status     string `json:"status"`
		TotalCents int    `json:"total_cents"`
	} `json:"data"`
}

func Test_ReserveTicketsSucceeds(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	resp, body, err := doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_cents": 10000},
		},
	}, ch)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode, "reserve failed: %s", string(body))

	var rr reserveResp
	if err := json.Unmarshal(body, &rr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assert.NotEmpty(t, rr.Data.BookingID)
	assert.Equal(t, "held", rr.Data.Status)
}

func Test_ReserveWithWrongPriceReturns400(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	resp, body, err := doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_cents": 1},
		},
	}, ch)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode, "expected 400 for wrong price: %s", string(body))
	assert.Contains(t, string(body), "unit_price_cents does not match",
		"error should mention price mismatch")
}

func Test_OverReserveReturnsConflict(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	resp, body, err := doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 9999, "unit_price_cents": 10000},
		},
	}, ch)
	require.NoError(t, err)
	assert.Equal(t, 409, resp.StatusCode, "body: %s", string(body))
}

func Test_ConfirmAndListBookings(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	// Reserve
	_, body, err := doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 2, "unit_price_cents": 10000},
		},
	}, ch)
	require.NoError(t, err)

	var rr reserveResp
	if err := json.Unmarshal(body, &rr); err != nil {
		t.Fatalf("decode reserve: %v", err)
	}
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Poll checkout until transaction is created by payment consumer
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

	txnID := tr.Data.ID
	require.NotEmpty(t, txnID, "no transaction ID in checkout response")

	// Simulate webhook callback
	resp, _, err := doJSON(http.MethodPost, env.payURL+"/api/payments/webhook/mock", map[string]string{
		"transaction_id": txnID,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	// Poll bookings until confirmed via payment.completed → inventory consumer
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		resp, b, _ := doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
		return resp != nil && resp.StatusCode == 200 && strings.Contains(string(b), `"confirmed"`)
	}, "booking confirmation via payment.completed → inventory consumer")

	// Final assertion
	resp, body, err = doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, string(body), `"confirmed"`)
}

func Test_ReservationExpiry(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	// 1. Snapshot available seats before reservation
	_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
	var detail eventDetailResp
	require.NoError(t, jsonData(body, &detail))
	require.NotEmpty(t, detail.TicketTypes)

	// Find matching ticket type
	var availableBefore int
	for _, tt := range detail.TicketTypes {
		if tt.ID == ttIDs[0] {
			availableBefore = tt.Available
			break
		}
	}
	require.Greater(t, availableBefore, 4, "not enough seats for test")

	// 2. Reserve seats
	_, body, _ = doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 5, "unit_price_cents": 10000}},
	}, ch)
	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// 3. Verify seats decreased
	_, body, _ = doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
	require.NoError(t, jsonData(body, &detail))
	for _, tt := range detail.TicketTypes {
		if tt.ID == ttIDs[0] {
			assert.Equal(t, availableBefore-5, tt.Available, "seats should be deducted after reserve")
			break
		}
	}

	// 4. Poll checkout until transaction is created
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
	}, "payment transaction init")

	txnID := tr.Data.ID
	require.NotEmpty(t, txnID)

	// 5. NOTE: intentionally NOT calling the webhook.

	// 6. Poll payment status until "failed" (TTL → reservation.expired → payment failed)
	var ts struct {
		Data struct{ Status string `json:"status"` } `json:"data"`
	}
	pollFor(t, 60*time.Second, 1*time.Second, func() bool {
		resp, b, _ := doJSON(http.MethodGet, env.payURL+"/api/payments/"+txnID+"/status", nil, ch)
		if resp == nil || resp.StatusCode != 200 {
			return false
		}
		json.Unmarshal(b, &ts)
		return ts.Data.Status == "failed"
	}, "payment failed via reservation expiry (30s TTL + grace)")

	assert.Equal(t, "failed", ts.Data.Status)

	// 7. Verify seats released back to original count
	_, body, _ = doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
	require.NoError(t, jsonData(body, &detail))
	for _, tt := range detail.TicketTypes {
		if tt.ID == ttIDs[0] {
			assert.Equal(t, availableBefore, tt.Available, "seats should be released after expiry")
			break
		}
	}
}

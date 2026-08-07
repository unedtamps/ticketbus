//go:build integration

package integration

// bookings_test.go — TestBooking_*: reservation, seat counting, release (ticketing service).

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

type reserveResp struct {
	Data struct {
		BookingID   string `json:"booking_id"`
		EventID     string `json:"event_id"`
		Status      string `json:"status"`
		TotalRupiah int    `json:"total_rupiah"`
	} `json:"data"`
}

func TestBooking_Reserve_Succeeds(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	resp, body, err := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_rupiah": ticketPrice(t, env, ttIDs[0])},
		},
	}, ch)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode, "reserve failed: %s", string(body))

	var rr reserveResp
	if err := json.Unmarshal(body, &rr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assert.NotEmpty(t, rr.Data.BookingID)
	assert.Equal(t, "pending", rr.Data.Status)
}

func TestBooking_Reserve_WrongPrice_400(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	resp, body, err := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_rupiah": 1},
		},
	}, ch)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode, "expected 400 for wrong price: %s", string(body))
	assert.Contains(t, string(body), "unit_price_rupiah does not match",
		"error should mention price mismatch")
}

func TestBooking_Reserve_OverReserve_Conflict(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	resp, body, err := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 9999, "unit_price_rupiah": ticketPrice(t, env, ttIDs[0])},
		},
	}, ch)
	require.NoError(t, err)
	assert.Equal(t, 409, resp.StatusCode, "body: %s", string(body))
}

func TestBooking_Reserve_CannotBeCancelled(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	_, body, _ := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_rupiah": ticketPrice(t, env, ttIDs[0])}},
	}, ch)
	var rr reserveResp
	require.NoError(t, json.Unmarshal(body, &rr))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Cancelling a reservation is not allowed — seats release on expiry only.
	resp, body2, err := doJSON(http.MethodDelete, env.invURL+"/api/bookings/reserve/"+bookingID, nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 409, resp.StatusCode, "DELETE should be rejected: %s", string(body2))
}

func TestBooking_Confirm_AndList(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	// Reserve
	_, body, err := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttIDs[0], "quantity": 2, "unit_price_rupiah": ticketPrice(t, env, ttIDs[0])},
		},
	}, ch)
	require.NoError(t, err)

	var rr reserveResp
	if err := json.Unmarshal(body, &rr); err != nil {
		t.Fatalf("decode reserve: %v", err)
	}
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Initiate payment, then simulate the completed webhook
	initiatePayment(t, env, bookingID, ch)
	completePaymentWebhook(t, env, bookingID)

	// Poll bookings until confirmed via payment.completed → ticketing consumer
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		resp, b, _ := doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
		return resp != nil && resp.StatusCode == 200 && strings.Contains(string(b), `"confirmed"`)
	}, "booking confirmation via payment.completed → ticketing consumer")

	// Final assertion
	finalResp, body, err := doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 200, finalResp.StatusCode)
	assert.True(t, strings.Contains(string(body), `"confirmed"`), "booking should be confirmed: %s", string(body))
}

func TestBooking_Expiry_ViaPaymentPoll(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	ctx := context.Background()

	// 1. Snapshot available seats before reservation
	_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
	var detail eventDetailResp
	require.NoError(t, decodeDataPayload(body, &detail))
	require.NotEmpty(t, detail.TicketTypes)

	var availableBefore int
	for _, tt := range detail.TicketTypes {
		if tt.ID == ttIDs[0] {
			availableBefore = tt.Available
			break
		}
	}
	require.Greater(t, availableBefore, 4, "not enough seats for test")

	// 2. Reserve seats (payment never initiated)
	_, body, _ = doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 5, "unit_price_rupiah": ticketPrice(t, env, ttIDs[0])}},
	}, ch)
	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// 3. Verify seats decreased
	_, body, _ = doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
	require.NoError(t, decodeDataPayload(body, &detail))
	for _, tt := range detail.TicketTypes {
		if tt.ID == ttIDs[0] {
			assert.Equal(t, availableBefore-5, tt.Available, "seats should be deducted after reserve")
			break
		}
	}

	// 4. Push the transaction deadline into the past so the payment expiry
	// poller picks it up immediately (no session was ever created).
	_, err := env.payPool.Exec(ctx, `UPDATE transactions SET expires_at = NOW() - INTERVAL '1 minute' WHERE booking_id = $1`, bookingID)
	require.NoError(t, err)

	// 5. The payment poller expires the transaction → payment.expired →
	// ticketing expires the booking and releases seats.
	pollFor(t, 30*time.Second, 1*time.Second, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "booking expiry via payment poll → payment.expired → ticketing consumer")

	// 6. Verify seats released back to original count
	_, body, _ = doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
	require.NoError(t, decodeDataPayload(body, &detail))
	for _, tt := range detail.TicketTypes {
		if tt.ID == ttIDs[0] {
			assert.Equal(t, availableBefore, tt.Available, "seats should be released after expiry")
			break
		}
	}
}

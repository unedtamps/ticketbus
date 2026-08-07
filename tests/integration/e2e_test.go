//go:build integration

package integration

// e2e_test.go — TestE2E_*: cross-service journeys (register → pay → confirm, cancel cascade).

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

func TestE2E_FullBookingJourney(t *testing.T) {
	env := getTestEnv()

	// 1. EO registers
	eo := env.registerAndLogin("eo")

	// 2. EO creates event
	event := createEventRaw(t, env, eo.AccessToken)
	eventID := event.ID

	// 3. Admin approves event
	admin := env.loginAdmin()
	ah := env.authHeadersWith(admin.AccessToken)
	_, body, err := doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/approve", nil, ah)
	require.NoError(t, err)

	var approved eventResp
	decodeDataPayload(body, &approved)
	assert.Equal(t, "published", approved.Status)

	// 4. Poll event detail until seats are initialized (outbox → Kafka → inventory)
	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	var detail eventDetailResp
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
		if decodeDataPayload(body, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init after approval")

	require.NotEmpty(t, detail.TicketTypes)
	ttID := detail.TicketTypes[0].ID

	// 6. Customer reserves tickets
	_, body, err = doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttID, "quantity": 1, "unit_price_rupiah": ticketPrice(t, env, ttID)}},
	}, ch)
	require.NoError(t, err)

	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// 7. Initiate payment and simulate the completed webhook
	initiatePayment(t, env, bookingID, ch)
	completePaymentWebhook(t, env, bookingID)

	// 10. Poll bookings until confirmed via payment.completed → inventory consumer
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		resp, b, _ := doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
		return resp != nil && resp.StatusCode == 200 && strings.Contains(string(b), `"confirmed"`)
	}, "booking confirmation via payment.completed → inventory consumer")

	// 11. Final assertion (always passes since poll just confirmed it)
	resp, body, err := doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, string(body), `"confirmed"`)
}

func TestE2E_EventCancelCascade(t *testing.T) {
	env := getTestEnv()

	// 1. EO creates event
	eo := env.registerAndLogin("eo")
	event := createEventRaw(t, env, eo.AccessToken)
	eventID := event.ID

	// 2. Admin approves
	admin := env.loginAdmin()
	ah := env.authHeadersWith(admin.AccessToken)
	resp, body, err := doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/approve", nil, ah)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	// 3. Poll until seats are initialized
	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	var detail eventDetailResp
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
		if decodeDataPayload(body, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init after approval")
	require.NotEmpty(t, detail.TicketTypes)
	ttID := detail.TicketTypes[0].ID
	availableBefore := detail.TicketTypes[0].Available

	// 4. Customer reserves tickets
	_, body, err = doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttID, "quantity": 2, "unit_price_rupiah": ticketPrice(t, env, ttID)}},
	}, ch)
	require.NoError(t, err)
	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// 5. Initiate payment and simulate the completed webhook
	initiatePayment(t, env, bookingID, ch)
	completePaymentWebhook(t, env, bookingID)

	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		resp, b, _ := doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
		return resp != nil && resp.StatusCode == 200 && strings.Contains(string(b), `"confirmed"`)
	}, "booking confirmed via payment.completed → inventory")

	// 6. Same EO cancels event
	eh := env.authHeadersWith(eo.AccessToken)
	resp, body, err = doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/cancel", nil, eh)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode, "cancel event: %s", string(body))

	// 7. Poll until booking is cancelled
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		resp, b, _ := doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
		return resp != nil && resp.StatusCode == 200 && strings.Contains(string(b), `"cancelled"`)
	}, "booking cancelled via event.cancelled → inventory")

	// Verify a refund request was created for the cancelled booking
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		var count int
		err := env.payPool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM refund_requests WHERE booking_id = $1`, bookingID).Scan(&count)
		return err == nil && count > 0
	}, "refund request created for cancelled event")

	// 8. Verify seats released back to original count
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
		if decodeDataPayload(body, &detail) != nil {
			return false
		}
		for _, tt := range detail.TicketTypes {
			if tt.ID == ttID {
				return tt.Available == availableBefore
			}
		}
		return false
	}, "seats released after event cancel")
}

func TestE2E_EventCancelCascade_CancelBeforeConfirm(t *testing.T) {
	env := getTestEnv()

	// 1. EO creates event
	eo := env.registerAndLogin("eo")
	event := createEventRaw(t, env, eo.AccessToken)
	eventID := event.ID

	// 2. Admin approves
	admin := env.loginAdmin()
	ah := env.authHeadersWith(admin.AccessToken)
	resp, body, err := doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/approve", nil, ah)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	// 3. Poll until seats are initialized
	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	var detail eventDetailResp
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
		if decodeDataPayload(body, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init after approval")
	require.NotEmpty(t, detail.TicketTypes)
	ttID := detail.TicketTypes[0].ID
	availableBefore := detail.TicketTypes[0].Available

	// 4. Customer reserves tickets
	_, body, err = doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttID, "quantity": 1, "unit_price_rupiah": ticketPrice(t, env, ttID)}},
	}, ch)
	require.NoError(t, err)
	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// 5. Initiate payment (session created, payment not yet completed)
	initiatePayment(t, env, bookingID, ch)

	// 6. EO cancels event BEFORE webhook
	eh := env.authHeadersWith(eo.AccessToken)
	resp, body, err = doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/cancel", nil, eh)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode, "cancel event: %s", string(body))

	// Wait for event.cancelled → Kafka → inventory consumer → event_status_cache upsert
	var evStatus struct {
		Event struct {
			Status string `json:"status"`
		} `json:"event"`
	}
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, eh)
		if decodeDataPayload(body, &evStatus) != nil {
			return false
		}
		return evStatus.Event.Status == "cancelled"
	}, "event status changed to cancelled")

	// Brief wait for Kafka → inventory consumer to upsert event_status_cache
	time.Sleep(3 * time.Second)

	// 7. Webhook fires AFTER cancel — payment completes but Confirm sees cancelled
	completePaymentWebhook(t, env, bookingID)

	// 8. Poll until booking is cancelled and a refund request was created
	// (late completion after cancel → payment refunds, booking stays cancelled)
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var count int
		err := env.payPool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM refund_requests WHERE booking_id = $1`, bookingID).Scan(&count)
		return err == nil && count > 0
	}, "booking created as cancelled with refund pending")

	// 9. Verify seats released (never confirmed, so back to original)
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
		if decodeDataPayload(body, &detail) != nil {
			return false
		}
		for _, tt := range detail.TicketTypes {
			if tt.ID == ttID {
				return tt.Available == availableBefore
			}
		}
		return false
	}, "seats released after cancel-before-confirm")
}

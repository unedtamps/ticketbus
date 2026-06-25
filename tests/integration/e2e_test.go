//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_FullBookingJourney(t *testing.T) {
	env := getTestEnv()

	// 1. EO registers
	eo := env.registerAndLogin("eo")
	waitForOrganizer(t, env, eo.AccessToken)

	// 2. EO creates event
	event := createEventRaw(t, env, eo.AccessToken)
	eventID := event.ID

	// 3. Admin approves event
	admin := env.loginAdmin()
	ah := env.authHeadersWith(admin.AccessToken)
	_, body, err := doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/approve", nil, ah)
	require.NoError(t, err)

	var approved eventResp
	jsonData(body, &approved)
	assert.Equal(t, "published", approved.Status)

	// 4. Poll event detail until seats are initialized (outbox → Kafka → inventory)
	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	var detail eventDetailResp
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
		if jsonData(body, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init via event.approved → inventory")

	require.NotEmpty(t, detail.TicketTypes)
	ttID := detail.TicketTypes[0].ID

	// 6. Customer reserves tickets
	_, body, err = doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttID, "quantity": 1, "unit_price_cents": 10000}},
	}, ch)
	require.NoError(t, err)

	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// 7. Poll checkout until transaction is created by payment consumer
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
	require.NotEmpty(t, txnID)

	// 9. Webhook confirms payment
	_, body, err = doJSON(http.MethodPost, env.payURL+"/api/payments/webhook/mock", map[string]string{
		"transaction_id": txnID,
	}, nil)
	require.NoError(t, err)

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

func Test_ConcurrentDuplicateReservation(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	var wg sync.WaitGroup
	results := make(chan int, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, _, _ := doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
				"event_id": eventID,
				"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 5, "unit_price_cents": 10000}},
			}, ch)
			if resp != nil {
				results <- resp.StatusCode
			}
		}()
	}
	wg.Wait()
	close(results)

	var success201, conflict409 int
	for code := range results {
		switch code {
		case 201:
			success201++
		case 409:
			conflict409++
		}
	}

	// At least one should succeed, at least one should fail if enough contention
	assert.GreaterOrEqual(t, success201, 1, "at least one concurrent reserve should succeed")
	t.Logf("concurrent reserve: 201=%d, 409=%d", success201, conflict409)
}

func Test_EventCancelCascade(t *testing.T) {
	env := getTestEnv()

	// 1. EO creates event
	eo := env.registerAndLogin("eo")
	waitForOrganizer(t, env, eo.AccessToken)
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
		if jsonData(body, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init via event.approved → inventory")
	require.NotEmpty(t, detail.TicketTypes)
	ttID := detail.TicketTypes[0].ID
	availableBefore := detail.TicketTypes[0].Available

	// 4. Customer reserves tickets
	_, body, err = doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttID, "quantity": 2, "unit_price_cents": 10000}},
	}, ch)
	require.NoError(t, err)
	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// 5. Poll checkout → webhook → booking confirmed
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
	}, "payment transaction init")
	txnID := tr.Data.ID
	require.NotEmpty(t, txnID)

	_, body, err = doJSON(http.MethodPost, env.payURL+"/api/payments/webhook/mock", map[string]string{
		"transaction_id": txnID,
	}, nil)
	require.NoError(t, err)

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

	// Verify refund_status is "pending" on cancelled booking
	resp, body, err = doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"refund_status":"pending"`)

	// 8. Verify seats released back to original count
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
		if jsonData(body, &detail) != nil {
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

func Test_EventCancelCascade_CancelBeforeConfirm(t *testing.T) {
	env := getTestEnv()

	// 1. EO creates event
	eo := env.registerAndLogin("eo")
	waitForOrganizer(t, env, eo.AccessToken)
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
		if jsonData(body, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init via event.approved → inventory")
	require.NotEmpty(t, detail.TicketTypes)
	ttID := detail.TicketTypes[0].ID
	availableBefore := detail.TicketTypes[0].Available

	// 4. Customer reserves tickets
	_, body, err = doJSON(http.MethodPost, env.invURL+"/api/inventory/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttID, "quantity": 1, "unit_price_cents": 10000}},
	}, ch)
	require.NoError(t, err)
	var rr reserveResp
	json.Unmarshal(body, &rr)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// 5. Poll checkout (payment now processing)
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
	}, "payment transaction init")
	txnID := tr.Data.ID
	require.NotEmpty(t, txnID)

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
		if jsonData(body, &evStatus) != nil {
			return false
		}
		return evStatus.Event.Status == "cancelled"
	}, "event status changed to cancelled")

	// Brief wait for Kafka → inventory consumer to upsert event_status_cache
	time.Sleep(3 * time.Second)

	// 7. Webhook fires AFTER cancel — payment succeeds but Confirm sees cancelled
	_, body, err = doJSON(http.MethodPost, env.payURL+"/api/payments/webhook/mock", map[string]string{
		"transaction_id": txnID,
	}, nil)
	require.NoError(t, err)

	// 8. Poll until booking appears as cancelled with refund pending
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		resp, b, _ := doJSON(http.MethodGet, env.invURL+"/api/bookings", nil, ch)
		return resp != nil && resp.StatusCode == 200 &&
			strings.Contains(string(b), `"cancelled"`) &&
			strings.Contains(string(b), `"refund_status":"pending"`)
	}, "booking created as cancelled with refund pending")

	// 9. Verify seats released (never confirmed, so back to original)
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
		if jsonData(body, &detail) != nil {
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

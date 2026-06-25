//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type eventResp struct {
	ID            string `json:"id"`
	OrganizerID   string `json:"organizer_id"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	VenueCapacity int    `json:"venue_capacity"`
}

type eventDetailResp struct {
	Event struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"event"`
	TicketTypes []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Available int    `json:"available"`
	} `json:"ticket_types"`
}

func Test_EOCreatesDraftEvent(t *testing.T) {
	env := getTestEnv()

	eo := env.registerAndLogin("eo")
	_ = env.authHeadersWith(eo.AccessToken)
	waitForOrganizer(t, env, eo.AccessToken)

	// Create event
	event := createEventRaw(t, env, eo.AccessToken)
	assert.Equal(t, "Integration E2E Event", event.Title)
	assert.Equal(t, "pending", event.Status)
}

func Test_EOSeesOnlyOwnEvents(t *testing.T) {
	env := getTestEnv()

	eo1 := env.registerAndLogin("eo")
	waitForOrganizer(t, env, eo1.AccessToken)

	eo2 := env.registerAndLogin("eo")
	waitForOrganizer(t, env, eo2.AccessToken)

	// EO1 creates an event
	createEventRaw(t, env, eo1.AccessToken)

	// EO2 lists their events — should be empty
	resp, body, err := doJSON(http.MethodGet, env.eventURL+"/api/events/mine", nil,
		env.authHeadersWith(eo2.AccessToken))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var mine struct {
		Data []eventResp `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &mine))
	assert.Empty(t, mine.Data, "EO2 should not see EO1's events")
}

func Test_AdminApprovesEvent(t *testing.T) {
	env := getTestEnv()

	eo := env.registerAndLogin("eo")
	waitForOrganizer(t, env, eo.AccessToken)

	event := createEventRaw(t, env, eo.AccessToken)
	eventID := event.ID

	admin := env.loginAdmin()
	ah := env.authHeadersWith(admin.AccessToken)

	// Approve
	resp, body, err := doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/approve", nil, ah)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var approved eventResp
	if err := jsonData(body, &approved); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, string(body))
	}
	assert.Equal(t, "published", approved.Status)
}

func Test_CustomerViewsEventWithAvailableSeats(t *testing.T) {
	env := getTestEnv()

	eo := env.registerAndLogin("eo")
	waitForOrganizer(t, env, eo.AccessToken)

	event := createEventRaw(t, env, eo.AccessToken)

	admin := env.loginAdmin()
	ah := env.authHeadersWith(admin.AccessToken)
	doJSON(http.MethodPost, env.eventURL+"/api/events/"+event.ID+"/approve", nil, ah)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	var detail eventDetailResp
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+event.ID, nil, ch)
		if jsonData(body, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init via event.approved → inventory")

	assert.NotEmpty(t, detail.TicketTypes)
	assert.Greater(t, detail.TicketTypes[0].Available, 0)
}

func Test_AdminCannotCreateEvent(t *testing.T) {
	env := getTestEnv()

	admin := env.loginAdmin()
	ah := env.authHeadersWith(admin.AccessToken)

	resp, _, err := doJSON(http.MethodPost, env.eventURL+"/api/events", map[string]interface{}{
		"title": "Admin Event",
	}, ah)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}

func Test_CustomerCannotApproveEvent(t *testing.T) {
	env := getTestEnv()

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	resp, _, err := doJSON(http.MethodPost, env.eventURL+"/api/events/"+uuid.NewString()+"/approve", nil, ch)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}

// ── Helpers ──

func waitForOrganizer(t *testing.T, env *TestEnv, accessToken string) {
	t.Helper()
	h := env.authHeadersWith(accessToken)
	for i := 0; i < 30; i++ {
		if i > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		resp, _, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/organizers/me", nil, h)
		if resp != nil && resp.StatusCode == 200 {
			return
		}
	}
	t.Fatal("organizer profile was never created")
}

func createEventRaw(t *testing.T, env *TestEnv, accessToken string) eventResp {
	t.Helper()
	h := env.authHeadersWith(accessToken)
	future := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	futureEnd := time.Now().Add(30*24*time.Hour + 3*time.Hour).Format(time.RFC3339)

	resp, body, err := doJSON(http.MethodPost, env.eventURL+"/api/events", map[string]interface{}{
		"title":          "Integration E2E Event",
		"description":    "End-to-end test event",
		"venue_name":     "Test Stadium",
		"venue_address":  "123 Test St",
		"venue_capacity": 100,
		"start_at":       future,
		"end_at":         futureEnd,
		"ticket_types": []map[string]interface{}{
			{"name": "VIP", "price_cents": 10000, "quantity": 20, "max_per_order": 5},
			{"name": "GA", "price_cents": 5000, "quantity": 80, "max_per_order": 10},
		},
	}, h)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)

	var ev eventResp
	if err := jsonData(body, &ev); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, string(body))
	}
	return ev
}

// setupApprovedEvent creates an event and approves it. Used by inventory/payment/e2e tests.
func setupApprovedEvent(t *testing.T, env *TestEnv) (eventID string, ticketTypeIDs []string) {
	t.Helper()

	eo := env.registerAndLogin("eo")
	waitForOrganizer(t, env, eo.AccessToken)

	event := createEventRaw(t, env, eo.AccessToken)
	eventID = event.ID

	admin := env.loginAdmin()
	ah := env.authHeadersWith(admin.AccessToken)
	resp, _, err := doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/approve", nil, ah)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

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

	for _, tt := range detail.TicketTypes {
		ticketTypeIDs = append(ticketTypeIDs, tt.ID)
	}

	return eventID, ticketTypeIDs
}

// jsonData extracts the "data" field from a standard API response.
func jsonData(respBody []byte, target interface{}) error {
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return err
	}
	if envelope.Error != "" {
		return assert.AnError // generic non-nil
	}
	return json.Unmarshal(envelope.Data, target)
}

// jsonErrorMsg extracts the error message from an error response.
func jsonErrorMsg(respBody []byte) string {
	var envelope struct {
		Error string `json:"error"`
	}
	json.Unmarshal(respBody, &envelope)
	return envelope.Error
}

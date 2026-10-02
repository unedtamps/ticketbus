//go:build integration

package integration

// event_test.go — TestEvent_*: event lifecycle, approval flow, and ownership.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvent_EO_CreatesDraft(t *testing.T) {
	env := getTestEnv()

	ev := createEventRaw(t, env, newEO())
	assert.Equal(t, "Integration E2E Event", ev.Title)
	assert.Equal(t, "pending", ev.Status)
}

func TestEvent_EO_SeesOnlyOwnEvents(t *testing.T) {
	env := getTestEnv()

	eo1 := newEO()
	eo2 := newEO()

	createEventRaw(t, env, eo1)

	resp, body, err := doJSON(http.MethodGet, env.apiURL+"/api/events/mine", nil, eo2.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var mine struct {
		Data []eventResp `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &mine))
	assert.Empty(t, mine.Data, "EO2 should not see EO1's events")
}

func TestEvent_Admin_Approve(t *testing.T) {
	env := getTestEnv()

	ev := createEventRaw(t, env, newEO())

	resp, body, err := doJSON(http.MethodPost, env.apiURL+"/api/events/"+ev.ID+"/approve", nil, adminActor().headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var approved eventResp
	require.NoError(t, decodeDataPayload(body, &approved), "decode: %s", body)
	assert.Equal(t, "published", approved.Status)
}

func TestEvent_Customer_ViewsWithSeats(t *testing.T) {
	env := getTestEnv()

	ev := createEventRaw(t, env, newEO())

	resp, _, err := doJSON(http.MethodPost, env.apiURL+"/api/events/"+ev.ID+"/approve", nil, adminActor().headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	cust := newCustomer()
	var detail eventDetailResp
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, body, _ := doJSON(http.MethodGet, env.apiURL+"/api/events/"+ev.ID, nil, cust.headers)
		if decodeDataPayload(body, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init after approval")

	require.NotEmpty(t, detail.TicketTypes)
	assert.Greater(t, detail.TicketTypes[0].Available, 0)
}

func TestEvent_Admin_CannotCreate(t *testing.T) {
	env := getTestEnv()

	resp, _, err := doJSON(http.MethodPost, env.apiURL+"/api/events", map[string]interface{}{
		"title": "Admin Event",
	}, adminActor().headers)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestEvent_Customer_CannotApprove(t *testing.T) {
	env := getTestEnv()

	resp, _, err := doJSON(http.MethodPost,
		env.apiURL+"/api/events/"+uuid.NewString()+"/approve", nil, newCustomer().headers)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestEvent_RequiresIdentity verifies RequireRole rejects a request that carries
// no gateway-injected identity headers at all.
func TestEvent_RequiresIdentity(t *testing.T) {
	env := getTestEnv()

	resp, _, err := doJSON(http.MethodPost, env.apiURL+"/api/events", map[string]string{
		"title": "Bad",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

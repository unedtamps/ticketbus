//go:build integration

package integration

// payout_test.go — organizer payout request and completion through the
// ticketing API. The payment side is simulated by publishing payout.completed
// to Kafka, which the ticketing consumer applies.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
)

type payoutState struct {
	EventID       string `json:"event_id"`
	Status        string `json:"status"`
	AmountRupiah  int64  `json:"amount_rupiah"`
	Attempts      int    `json:"attempts"`
	ProviderRef   string `json:"provider_ref"`
	FailureReason string `json:"failure_reason"`
}

func (env *TestEnv) setPayoutAccount(t *testing.T, eo actor) {
	t.Helper()
	resp, body, err := doJSON(http.MethodPut, env.apiURL+"/api/payout-account", map[string]interface{}{
		"bank_code":                "BCA",
		"bank_account_number":      "1234567890",
		"bank_account_holder_name": "Integration EO",
	}, eo.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "set payout account: %s", body)
}

func (env *TestEnv) requestPayout(t *testing.T, eo actor, eventID string) (*http.Response, []byte) {
	t.Helper()
	resp, body, err := doJSON(http.MethodPost, env.apiURL+"/api/events/"+eventID+"/payout", nil, eo.headers)
	require.NoError(t, err)
	return resp, body
}

// endEvent moves the event's end time into the past.
func (env *TestEnv) endEvent(t *testing.T, eventID string) {
	t.Helper()
	_, err := env.pool.Exec(context.Background(),
		`UPDATE events SET end_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, eventID)
	require.NoError(t, err)
}

func (env *TestEnv) payoutRow(t *testing.T, eventID string) (status string, amount int64, attempt int) {
	t.Helper()
	require.NoError(t, env.pool.QueryRow(context.Background(),
		`SELECT status, COALESCE(amount_rupiah, 0), attempt
		 FROM event_payout_attempts WHERE event_id = $1 ORDER BY attempt DESC LIMIT 1`,
		eventID).Scan(&status, &amount, &attempt))
	return status, amount, attempt
}

func TestPayout_Request_Outbox_And_Completion(t *testing.T) {
	env := getTestEnv()
	eventID, _, eo := setupApprovedEvent(t, env)
	env.endEvent(t, eventID)
	env.setPayoutAccount(t, eo)

	resp, body := env.requestPayout(t, eo, eventID)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, "request payout: %s", body)

	var requested payoutState
	require.NoError(t, decodeDataPayload(body, &requested), "request payout: %s", body)
	assert.Equal(t, "pending", requested.Status)

	// The attempt row and its outbox event were written atomically.
	var bankSnapshot, payoutStatus string
	var attempt int
	require.NoError(t, env.pool.QueryRow(context.Background(),
		`SELECT bank_code, status, attempt FROM event_payout_attempts WHERE event_id = $1`, eventID).
		Scan(&bankSnapshot, &payoutStatus, &attempt))
	assert.Equal(t, "BCA", bankSnapshot)
	assert.Equal(t, "pending", payoutStatus)
	assert.Equal(t, 1, attempt)

	var outboxCount int
	require.NoError(t, env.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM outbox WHERE topic = $1 AND key = $2`,
		sdomain.TopicPayoutRequested, eventID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount)

	// A second request while the first is in flight is rejected.
	resp, body = env.requestPayout(t, eo, eventID)
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "payout in flight: %s", body)

	// Payment reports the gateway outcome.
	require.NoError(t, env.infra.Producer.Produce(context.Background(),
		sdomain.TopicPayoutCompleted, eventID, sdomain.PayoutCompleted{
			EventID:      eventID,
			Attempt:      1,
			Status:       "paid_out",
			AmountRupiah: 25000,
			ProviderRef:  "dis-mock-test",
			At:           time.Now(),
		}))

	pollFor(t, 20*time.Second, 500*time.Millisecond, func() bool {
		status, _, _ := env.payoutRow(t, eventID)
		return status == "paid_out"
	}, "event payout to become paid_out")

	status, amount, _ := env.payoutRow(t, eventID)
	assert.Equal(t, "paid_out", status)
	assert.EqualValues(t, 25000, amount)

	resp, body, err := doJSON(http.MethodGet, env.apiURL+"/api/events/"+eventID+"/payout", nil, eo.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "get payout: %s", body)
	var got payoutState
	require.NoError(t, decodeDataPayload(body, &got))
	assert.Equal(t, "paid_out", got.Status)
	assert.EqualValues(t, 25000, got.AmountRupiah)
	assert.Equal(t, 1, got.Attempts)
	assert.Equal(t, "dis-mock-test", got.ProviderRef)
}

func TestPayout_Guards(t *testing.T) {
	env := getTestEnv()

	t.Run("not ended", func(t *testing.T) {
		eventID, _, eo := setupApprovedEvent(t, env)
		env.setPayoutAccount(t, eo)
		resp, body := env.requestPayout(t, eo, eventID)
		assert.Equal(t, http.StatusConflict, resp.StatusCode, "not ended: %s", body)
	})

	t.Run("no payout account", func(t *testing.T) {
		eventID, _, eo := setupApprovedEvent(t, env)
		env.endEvent(t, eventID)
		resp, body := env.requestPayout(t, eo, eventID)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "no account: %s", body)
	})

	t.Run("not owner", func(t *testing.T) {
		eventID, _, _ := setupApprovedEvent(t, env)
		env.endEvent(t, eventID)
		other := newEO()
		env.setPayoutAccount(t, other)
		resp, body := env.requestPayout(t, other, eventID)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "not owner: %s", body)
	})

	t.Run("cancelled event", func(t *testing.T) {
		eventID, _, eo := setupApprovedEvent(t, env)
		env.endEvent(t, eventID)
		env.setPayoutAccount(t, eo)
		// An ended event is admin-cancel only now; the organizer's payout is
		// still what is under test.
		resp, body, err := doJSON(http.MethodPost,
			env.apiURL+"/api/admin/events/"+eventID+"/cancel", nil, adminActor().headers)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, "cancel: %s", body)

		resp, body = env.requestPayout(t, eo, eventID)
		assert.Equal(t, http.StatusConflict, resp.StatusCode, "cancelled: %s", body)
	})

	t.Run("invalid bank account", func(t *testing.T) {
		_, _, eo := setupApprovedEvent(t, env)
		resp, body, err := doJSON(http.MethodPut, env.apiURL+"/api/payout-account", map[string]interface{}{
			"bank_code":                "NOTABANK",
			"bank_account_number":      "1234567890",
			"bank_account_holder_name": "Integration EO",
		}, eo.headers)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "invalid bank: %s", body)
	})
}

// TestPayout_Request_OrganizerEventShape guards the JSON contract the web
// dashboard consumes from GET /api/events/mine.
func TestPayout_Request_OrganizerEventShape(t *testing.T) {
	env := getTestEnv()
	eventID, _, eo := setupApprovedEvent(t, env)
	env.endEvent(t, eventID)
	env.setPayoutAccount(t, eo)

	resp, body := env.requestPayout(t, eo, eventID)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, "request payout: %s", body)

	resp, body, err := doJSON(http.MethodGet, env.apiURL+"/api/events/mine", nil, eo.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "list mine: %s", body)

	var page struct {
		Events []struct {
			ID           string `json:"id"`
			PayoutStatus string `json:"payout_status"`
		} `json:"events"`
	}
	require.NoError(t, decodeDataPayload(body, &page))
	for _, e := range page.Events {
		if e.ID == eventID {
			assert.Equal(t, "pending", e.PayoutStatus)
			return
		}
	}
	t.Fatalf("event %s not in /api/events/mine", eventID)
}

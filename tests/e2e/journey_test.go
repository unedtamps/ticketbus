//go:build e2e

package e2e

// journey_test.go — the full purchase path through the gateway, plus the
// gateway's own authentication contract.

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGateway_RequiresIdentity verifies the forward-auth contract: the gateway
// rejects protected routes without a token and accepts them with one, injecting
// the identity headers the upstream services trust.
//
// This is the one behaviour the per-service suites structurally cannot check —
// they synthesize X-Authenticated-* headers themselves.
func TestGateway_RequiresIdentity(t *testing.T) {
	env := getTestEnv()

	t.Run("no token is rejected by forward-auth", func(t *testing.T) {
		status, body, err := env.doJSON(http.MethodPost, "/api/events", map[string]string{
			"title": "Bad",
		}, nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, status, "body: %s", body)
	})

	t.Run("garbage token is rejected by forward-auth", func(t *testing.T) {
		status, _, err := env.doJSON(http.MethodPost, "/api/events", map[string]string{
			"title": "Bad",
		}, map[string]string{"Authorization": "Bearer not-a-real-jwt"})
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("valid token is accepted and identity reaches the service", func(t *testing.T) {
		eo := env.registerEO(t)

		// The created event's organizer_id can only come from the JWT claims,
		// copied by Traefik from auth's /api/auth/verify response headers.
		created := env.createEvent(t, eo, 100)
		detail := env.fetchEvent(t, created.Event.ID)
		require.NotEmpty(t, detail.Event.ID)

		status, body, err := env.doJSON(http.MethodGet, "/api/events/mine", nil, bearer(eo))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status, "mine: %s", body)
		assert.Contains(t, string(body), created.Event.ID,
			"organizer should see the event it created through forward-auth")
	})

	t.Run("customer cannot reach an organizer-only route", func(t *testing.T) {
		cust := env.registerCustomer(t)
		status, _, err := env.doJSON(http.MethodPost, "/api/events", map[string]string{
			"title": "Bad",
		}, bearer(cust))
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, status,
			"RequireRole(customer) should reject an event create")
	})
}

// TestE2E_FullBookingJourney walks the whole purchase path: an organizer creates
// an event, an admin approves it, a customer reserves a ticket and pays, and the
// booking ends up confirmed rather than pending.
func TestE2E_FullBookingJourney(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)

	// 1. EO creates an event, 2. admin approves it, seats initialise.
	eventID, ticketTypes := env.approvedEvent(t, admin)
	tt := ticketTypes[0]

	// 3. Customer reserves a ticket.
	cust := env.registerCustomer(t)
	bookingID := env.reserve(t, cust, eventID, tt.ID, 1, tt.PriceRupiah)
	require.Equal(t, "pending", env.getBooking(t, cust, bookingID).Status)

	// 4. Customer pays and the gateway reports success.
	env.settleBooking(t, cust, bookingID)

	// 5. The booking is no longer pending.
	confirmed := env.getBooking(t, cust, bookingID)
	assert.Equal(t, "confirmed", confirmed.Status)
	assert.Equal(t, "completed", env.paymentStatus(t, cust, bookingID))
}

// TestE2E_Webhook_RealGatewayPayload replays a verbatim provider payload through
// the public webhook route, proving the handler accepts the real wire format
// (session id in data.payment_session_id) and not just our own envelope.
func TestE2E_Webhook_RealGatewayPayload(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)

	eventID, ticketTypes := env.approvedEvent(t, admin)
	cust := env.registerCustomer(t)
	tt := ticketTypes[0]

	bookingID := env.reserve(t, cust, eventID, tt.ID, 1, tt.PriceRupiah)
	sessionID := env.payBooking(t, cust, bookingID)

	env.postRawWebhook(t, fixtureWebhook(t, "xendit_payment_session_completed.json", bookingID, sessionID))

	env.waitPaymentStatus(t, cust, bookingID, "completed", txnTimeout)
	env.waitBookingStatus(t, cust, bookingID, "confirmed", settleTimeout)
}

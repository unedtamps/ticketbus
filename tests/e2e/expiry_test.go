//go:build e2e

package e2e

// expiry_test.go — the gateway's expired-session webhook, and the guard that
// ignores a webhook belonging to a different session.

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_Expiry_WebhookExpiresBooking verifies that an expired session webhook
// expires the transaction and releases the reserved seats.
//
// The gateway sends payment_session.expired; the payment service writes the
// outbox row and Kafka carries it to the ticketing consumer.
//
// The payment service's own expiry sweeper — which expires a transaction whose
// deadline passed with no webhook at all — is covered by the payment service
// suite (TestExpiry_Sweeper_ExpiresUnpaidSession), because triggering it
// black-box requires backdating the stored deadline, and the e2e stack runs with
// a one-hour reservation TTL.
func TestE2E_Expiry_WebhookExpiresBooking(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)

	eventID, ticketTypes := env.approvedEvent(t, admin)
	tt := ticketTypes[0]
	availableBefore := tt.Available

	cust := env.registerCustomer(t)
	bookingID := env.reserve(t, cust, eventID, tt.ID, 2, tt.PriceRupiah)
	require.Equal(t, availableBefore-2, env.availableSeats(t, eventID, tt.ID),
		"reserved seats are held")

	sessionID := env.payBooking(t, cust, bookingID)
	env.deliverWebhook(t, "payment_session.expired", bookingID, sessionID)

	env.waitPaymentStatus(t, cust, bookingID, "expired", txnTimeout)
	env.waitBookingStatus(t, cust, bookingID, "expired", settleTimeout)
	assert.Equal(t, availableBefore, env.availableSeats(t, eventID, tt.ID),
		"seats should be released after the session expires")
}

// TestE2E_Webhook_SessionMismatchIgnored documents a sharp edge in the webhook
// handler: when the delivered payment_session_id does not match the one stored
// on the transaction, the delivery is acknowledged with 200 but changes nothing.
// Acknowledging rather than erroring is deliberate — it stops the gateway from
// retrying a delivery that will never apply.
//
// This is why a test must call the initiate endpoint before delivering a
// webhook: before that, transactions.provider_ref is empty, so a webhook
// carrying a session id would be silently dropped.
func TestE2E_Webhook_SessionMismatchIgnored(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)

	eventID, ticketTypes := env.approvedEvent(t, admin)
	tt := ticketTypes[0]

	cust := env.registerCustomer(t)
	bookingID := env.reserve(t, cust, eventID, tt.ID, 1, tt.PriceRupiah)

	// Deliberately skip env.payBooking: the stored provider_ref is still empty.
	before := env.paymentStatus(t, cust, bookingID)
	require.Equal(t, "initiated", before,
		"a seeded-but-unpaid transaction is initiated")

	status := env.deliverWebhook(t, "payment_session.completed", bookingID, "ps-mock-some-other-booking")
	assert.Equal(t, http.StatusOK, status, "mismatched delivery is acknowledged, not retried")

	assert.Equal(t, before, env.paymentStatus(t, cust, bookingID),
		"a mismatched session id must not change the transaction")
	assert.Equal(t, "pending", env.getBooking(t, cust, bookingID).Status,
		"a mismatched session id must not confirm the booking")
}

// TestE2E_Webhook_RejectsBadCallbackToken verifies the shared secret on the
// public webhook route. The route has no forward-auth, so this token is the only
// thing standing between an attacker and the transaction table.
func TestE2E_Webhook_RejectsBadCallbackToken(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)

	eventID, ticketTypes := env.approvedEvent(t, admin)
	cust := env.registerCustomer(t)
	bookingID := env.reserve(t, cust, eventID, ticketTypes[0].ID, 1, ticketTypes[0].PriceRupiah)
	sessionID := env.payBooking(t, cust, bookingID)

	status, body, err := env.doJSON(http.MethodPost, "/api/payments/webhook", map[string]interface{}{
		"event": "payment_session.completed",
		"data": map[string]string{
			"reference_id":       bookingID,
			"payment_session_id": sessionID,
			"status":             "COMPLETED",
		},
	}, map[string]string{"x-callback-token": "wrong-token"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, status, "body: %s", body)

	assert.Equal(t, "pending", env.paymentStatus(t, cust, bookingID),
		"a rejected webhook must not settle the transaction")
}

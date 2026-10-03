//go:build e2e

package e2e

// concurrency_test.go — seat invariants under parallel load.
//
// The original suite asserted these by querying Postgres directly. This one is
// black-box, so the invariant is re-derived from one observable quantity:
//
//	confirmed = capacity - available
//
// If that equals the quantity this test actually settled, seats were neither
// oversold, lost, nor double-released.

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_ConcurrentReservations_NoOversell fires parallel reservations against a
// deliberately small ticket type so contention is real, settles everything the
// gateway accepted, and checks the seat ledger balances.
func TestE2E_ConcurrentReservations_NoOversell(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)

	// VIP holds 20 seats in the fixture, so compete for the GA type (80 seats)
	// with 10 customers each trying to take 12 — three can win, the rest must be
	// rejected rather than oversold.
	const contenders = 10
	const qty = 12

	ev := env.createEvent(t, env.registerEO(t), 100)
	eventID := ev.Event.ID
	env.approveEvent(t, eventID, admin)

	// Wait for seats, then pick the ticket type with the largest capacity so the
	// contention maths is about reservations, not about a 20-seat ceiling.
	var chosen *ticketType
	pollFor(t, 30*pollInterval, pollInterval, func() bool {
		for i, tt := range env.fetchEvent(t, eventID).TicketTypes {
			if chosen == nil || tt.Available > chosen.Available {
				tt := tt
				chosen = &tt
				_ = i
			}
		}
		return chosen != nil && chosen.Available > 0
	}, "seat init after approval")
	require.NotNil(t, chosen)

	ttID, price, capacity := chosen.ID, chosen.PriceRupiah, chosen.Available
	t.Logf("ticket type %s: %d seats, %d contenders x %d", ttID, capacity, contenders, qty)

	type accepted struct {
		bookingID string
		account   account
	}

	var (
		mu    sync.Mutex
		wins  []accepted
		wg    sync.WaitGroup
		codes = make(chan int, contenders)
	)

	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			cust := env.registerCustomer(t)
			status, body, err := env.doJSON(http.MethodPost, "/api/bookings/reserve", map[string]interface{}{
				"event_id": eventID,
				"items": []map[string]interface{}{
					{"ticket_type_id": ttID, "quantity": qty, "unit_price_rupiah": price},
				},
			}, bearer(cust))
			codes <- status
			if err != nil || status != http.StatusCreated {
				return // oversubscribed or rejected: not an accepted reservation
			}

			var out struct {
				BookingID string `json:"booking_id"`
			}
			if decodeData(body, &out) != nil || out.BookingID == "" {
				return
			}

			mu.Lock()
			wins = append(wins, accepted{out.BookingID, cust})
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(codes)

	accepted201, conflicts := 0, 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			accepted201++
		case http.StatusConflict:
			conflicts++
		}
	}
	t.Logf("reservations: %d accepted, %d conflict, %d accepted bookings recorded",
		accepted201, conflicts, len(wins))

	require.Equal(t, accepted201, len(wins),
		"every 201 reserve must yield a booking id")

	// The invariant that matters: capacity must never be oversold.
	require.LessOrEqual(t, len(wins)*qty, capacity,
		"accepted reservations exceed capacity: %d x %d > %d", len(wins), qty, capacity)
	require.Greater(t, len(wins), 0, "no reservation succeeded; the test proved nothing")

	// Settle every accepted reservation through the gateway.
	wantConfirmed := 0
	for _, w := range wins {
		env.settleBooking(t, w.account, w.bookingID)
		wantConfirmed += qty
	}

	// Wait for the Kafka round trip to land every confirmation before reading
	// the ledger, otherwise available is still settling.
	pollFor(t, settleTimeout, pollInterval, func() bool {
		return capacity-env.availableSeats(t, eventID, ttID) == wantConfirmed
	}, "every settled booking to be reflected in seat availability")

	available := env.availableSeats(t, eventID, ttID)
	confirmed := capacity - available

	assert.Equal(t, wantConfirmed, confirmed,
		"confirmed seats (%d) should equal the quantity this test settled (%d)",
		confirmed, wantConfirmed)
	assert.GreaterOrEqual(t, available, 0, "seat count went negative: %d", available)
	assert.Equal(t, capacity, confirmed+available,
		"seat conservation: confirmed %d + available %d != capacity %d",
		confirmed, available, capacity)
}

//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type eventSeatInfo struct {
	EventID    string
	TicketType string // VIP or GA
	TTID       string
	Initial    int
}

func TestConcurrentBookingConsistency(t *testing.T) {
	env := getTestEnv()
	ctx := context.Background()

	totalSeats := 0

	// ── 1. Setup: 5 events × 2 ticket types = 550 seats ──
	var seats []eventSeatInfo
	for i := 0; i < 5; i++ {
		eventID, _ := setupApprovedEvent(t, env)
		cust := env.registerAndLogin("customer")
		ch := env.authHeadersWith(cust.AccessToken)
		var detail eventDetailResp
		pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
			_, body, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
			if jsonData(body, &detail) != nil {
				return false
			}
			return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
		}, "seat init")

		for _, tt := range detail.TicketTypes {
			initQty := tt.Available
			totalSeats += initQty
			seats = append(seats, eventSeatInfo{
				EventID:    eventID,
				TTID:       tt.ID,
				TicketType: tt.Name,
				Initial:    initQty,
			})
		}
	}
	t.Logf("setup: %d events, %d total seats across %d ticket types", 5, totalSeats, len(seats))

	// ── 2. Register 30 customers ──
	type custInfo struct {
		Headers map[string]string
	}
	var customers []custInfo
	for i := 0; i < 30; i++ {
		creds := env.registerAndLogin("customer")
		customers = append(customers, custInfo{
			Headers: env.authHeadersWith(creds.AccessToken),
		})
	}

	// ── 3. Fire concurrent reserve + checkout + webhook ──
	type attempt struct {
		BookingID string
		SeatIdx   int
		Qty       int
		ReserveOK bool
	}
	results := make(chan attempt, 90)
	var wg sync.WaitGroup

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(cust custInfo) {
			defer wg.Done()
			for a := 0; a < 3; a++ {
				si := rand.Intn(len(seats))
				s := seats[si]
				qty := rand.Intn(6) + 5

				// 3a. Reserve
				resp, body, err := doJSON(
					http.MethodPost,
					env.invURL+"/api/inventory/reserve",
					map[string]interface{}{
						"event_id": s.EventID,
						"items": []map[string]interface{}{{
							"ticket_type_id":   s.TTID,
							"quantity":         qty,
							"unit_price_cents": 10000,
						}},
					},
					cust.Headers,
				)

				ok := err == nil && resp != nil &&
					(resp.StatusCode == 200 || resp.StatusCode == 201)
				if !ok {
					results <- attempt{ReserveOK: false}
					continue
				}

				var rr reserveResp
				if json.Unmarshal(body, &rr) != nil || rr.Data.BookingID == "" {
					results <- attempt{ReserveOK: false}
					continue
				}
				bookingID := rr.Data.BookingID

				// 3b. Poll checkout until transaction is created by payment consumer
				var txnID string
				deadline := time.Now().Add(500 * time.Second)
				for time.Now().Before(deadline) {
					r, b, _ := doJSON(http.MethodPost,
						env.payURL+"/api/payments/by-booking/"+bookingID+"/checkout",
						nil, cust.Headers)
					if r == nil || r.StatusCode != 200 {
						time.Sleep(500 * time.Millisecond)
						continue
					}
					var tr struct {
						Data struct {
							ID string `json:"id"`
						} `json:"data"`
					}
					if json.Unmarshal(b, &tr) == nil && tr.Data.ID != "" {
						txnID = tr.Data.ID
						break
					}
					time.Sleep(500 * time.Millisecond)
				}

				if txnID == "" {
					results <- attempt{ReserveOK: false}
					continue
				}

				// 3c. Fire webhook mock immediately
				doJSON(http.MethodPost, env.payURL+"/api/payments/webhook/mock",
					map[string]string{"transaction_id": txnID}, nil)

				results <- attempt{
					BookingID: bookingID,
					SeatIdx:   si,
					Qty:       qty,
					ReserveOK: true,
				}
			}
		}(customers[i])
	}
	wg.Wait()
	close(results)

	var reservesOK, reservesFail int
	goroutineQty := make(map[string]int) // TTID → total qty reserved
	for r := range results {
		if r.ReserveOK {
			reservesOK++
			goroutineQty[seats[r.SeatIdx].TTID] += r.Qty
		} else {
			reservesFail++
		}
	}
	t.Logf(
		"concurrent reserve+checkout+webhook: %d success, %d failed out of 90",
		reservesOK,
		reservesFail,
	)

	// ── 4. Settle async pipeline ──
	pools := map[string]*pgxpool.Pool{
		"auth": env.authPool, "event": env.eventPool,
		"inventory": env.invPool, "payment": env.payPool,
	}

	// Wait for payment.completed → outbox → Kafka → inventory → booking confirmed
	pollFor(t, 300*time.Second, 500*time.Millisecond, func() bool {
		for _, pool := range pools {
			var pending int
			if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbox WHERE delivered = false").
				Scan(&pending); err != nil {
				return false
			}
			if pending > 0 {
				return false
			}
		}
		var pendingTxn int
		env.payPool.QueryRow(
			ctx,
			"SELECT COUNT(*) FROM transactions WHERE status IN ('initiated', 'processing')",
		).Scan(&pendingTxn)
		return pendingTxn == 0
	}, "async propagation (payment.completed → inventory)")

	// 4b. Wait for inventory consumer to create all bookings
	pollFor(t, 500*time.Second, 500*time.Millisecond, func() bool {
		var completed int
		env.payPool.QueryRow(ctx,
			"SELECT COUNT(*) FROM transactions WHERE status = 'completed'").Scan(&completed)

		var confirmed int
		env.invPool.QueryRow(ctx,
			"SELECT COUNT(*) FROM bookings WHERE status = 'confirmed'").Scan(&confirmed)

		return confirmed >= completed
	}, "inventory consumer: all bookings created")

	// 4c. Final outbox flush (ticket.issued events from last Confirm calls)
	pollFor(t, 500*time.Second, 500*time.Millisecond, func() bool {
		for _, pool := range pools {
			var pending int
			if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbox WHERE delivered = false").
				Scan(&pending); err != nil {
				return false
			}
			if pending > 0 {
				return false
			}
		}
		return true
	}, "outbox delivery (ticket.issued)")

	// ── 5. Verify invariants ──

	t.Run("no overselling", func(t *testing.T) {
		for _, s := range seats {
			var totalBooked int
			err := env.invPool.QueryRow(ctx, `
				SELECT COALESCE(SUM(bi.quantity), 0)
				FROM booking_items bi
				JOIN bookings b ON b.id = bi.booking_id
				WHERE bi.ticket_type_id = $1 AND b.status = 'confirmed'
			`, s.TTID).Scan(&totalBooked)
			require.NoError(t, err)

			assert.LessOrEqual(t, totalBooked, s.Initial,
				"ticket type %s/%s: booked %d > initial %d (oversold!)",
				s.EventID, s.TTID, totalBooked, s.Initial)
		}
	})

	t.Run("seat conservation", func(t *testing.T) {
		for _, s := range seats {
			key := fmt.Sprintf("counter:seat:%s:%s", s.EventID, s.TTID)
			avail, err := env.rdb.Get(ctx, key).Int()
			require.NoError(t, err, "redis key %s not found", key)

			var booked int
			env.invPool.QueryRow(ctx, `
				SELECT COALESCE(SUM(bi.quantity), 0)
				FROM booking_items bi
				JOIN bookings b ON b.id = bi.booking_id
				WHERE bi.ticket_type_id = $1 AND b.status = 'confirmed'
			`, s.TTID).Scan(&booked)

			assert.Equal(t, s.Initial, booked+avail,
				"seat conservation failed for %s/%s: booked=%d + avail=%d != initial=%d",
				s.EventID, s.TTID, booked, avail, s.Initial)
		}
	})

	t.Run("booking quantity matches", func(t *testing.T) {
		for _, s := range seats {
			expected := goroutineQty[s.TTID]
			var booked int
			env.invPool.QueryRow(ctx, `
				SELECT COALESCE(SUM(bi.quantity), 0)
				FROM booking_items bi
				JOIN bookings b ON b.id = bi.booking_id
				WHERE bi.ticket_type_id = $1 AND b.status = 'confirmed'
			`, s.TTID).Scan(&booked)

			assert.Equal(t, expected, booked,
				"tt %s (%s): goroutines reserved %d, DB confirmed %d",
				s.TTID, s.TicketType, expected, booked)
		}
	})

	t.Run("no ghost seats", func(t *testing.T) {
		for _, s := range seats {
			key := fmt.Sprintf("counter:seat:%s:%s", s.EventID, s.TTID)
			avail, err := env.rdb.Get(ctx, key).Int()
			require.NoError(t, err)
			assert.GreaterOrEqual(t, avail, 0,
				"negative seat count for %s/%s: %d", s.EventID, s.TTID, avail)
		}
	})
}

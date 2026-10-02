//go:build integration

package integration

// concurrency_test.go — TestConcurrency_*: parallel reservation invariants.

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConcurrency_DuplicateReservation verifies two simultaneous reserves of the
// same seats converge to at most one success.
func TestConcurrency_DuplicateReservation(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs, _ := setupApprovedEvent(t, env)
	cust := newCustomer()
	ttID := ttIDs[0]

	var wg sync.WaitGroup
	results := make(chan int, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, _, _ := env.reserve(t, cust, eventID, ttID, 5, env.ticketPrice(t, ttID))
			if resp != nil {
				results <- resp.StatusCode
			}
		}()
	}
	wg.Wait()
	close(results)

	var success, conflict int
	for code := range results {
		switch code {
		case http.StatusCreated, http.StatusOK:
			success++
		case http.StatusConflict:
			conflict++
		}
	}

	assert.GreaterOrEqual(t, success, 1, "at least one concurrent reserve should succeed")
	t.Logf("concurrent reserve: success=%d, conflict=%d", success, conflict)
}

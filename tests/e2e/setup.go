//go:build e2e

// Package e2e is the multi-service end-to-end suite.
//
// Unlike the per-service suites under service/*/tests/integration, these tests
// import nothing from any service. They drive the whole system the way a
// browser would: every request goes through Traefik on E2E_BASE_URL, carrying a
// real RS256 JWT issued by the auth service. Traefik's forward-auth middleware
// calls /api/auth/verify and injects the X-Authenticated-* headers that
// ticketing and payment trust — a contract no in-process suite can exercise.
//
// The suite needs the docker/docker-compose.e2e.yml stack running:
//
//	make e2e        # brings the stack up, runs these tests, tears it down
//	make e2e-up     # bring the stack up and leave it running
//	make e2e-down   # tear it down
//
// Gateway location can be overridden with E2E_BASE_URL.
//
// The gateway is the only public entry point, so a missing or rejected token
// surfaces as 401 from forward-auth. The per-service suites hit handlers
// directly, where RequireRole answers 403 for the same condition.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// defaultBaseURL matches the Traefik host port in docker-compose.e2e.yml.
const defaultBaseURL = "http://localhost:8100"

// readinessTimeout allows for a cold stack: three postgres instances, one Kafka
// broker, three service images starting and the gateway coming up behind them.
const readinessTimeout = 3 * time.Minute

// TestEnv holds the gateway address. There is nothing else to wire: no database
// pools, no containers, no in-process handlers.
type TestEnv struct {
	baseURL string
	client  *http.Client
}

// TestMain blocks until the gateway answers, so tests never fail on a stack
// that is still booting.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	env := getTestEnv()
	if err := env.waitForGateway(readinessTimeout); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: gateway not ready: %v\n", err)
		fmt.Fprintf(os.Stderr, "e2e: is the stack up? try `make e2e-up`\n")
		return 1
	}
	return m.Run()
}

func getTestEnv() *TestEnv {
	base := os.Getenv("E2E_BASE_URL")
	if base == "" {
		base = defaultBaseURL
	}
	return &TestEnv{
		baseURL: base,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// url builds an absolute gateway URL.
func (env *TestEnv) url(path string) string {
	return env.baseURL + path
}

// waitForGateway polls the auth health route through Traefik, which also proves
// Traefik can reach auth over the e2e network.
func (env *TestEnv) waitForGateway(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ctx := context.Background()
	var lastErr error

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.url("/api/auth/health"), nil)
		if err != nil {
			return err
		}
		resp, err := env.client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("gateway returned %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %v: %w", timeout, lastErr)
}

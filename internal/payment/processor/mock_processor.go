package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nedo/TicketSaas/internal/payment/domain"
)

// MockProcessor implements domain.PaymentProcessor with a simulated gateway.
// It stores payment sessions in memory, exposes them via GetSession, and
// fires a payment_session.expired webhook at the session deadline unless
// suppressed (used to test the internal expiry poller).
type MockProcessor struct {
	webhookURL     string
	callbackToken  string
	httpClient     *http.Client

	mu             sync.Mutex
	sessions       map[string]*domain.SessionResult
	timers         map[string]*time.Timer
	suppressExpiry bool
}

// NewMockProcessor creates a new mock payment processor.
func NewMockProcessor(webhookURL string) *MockProcessor {
	return &MockProcessor{
		webhookURL: strings.TrimSuffix(webhookURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		sessions:   make(map[string]*domain.SessionResult),
		timers:     make(map[string]*time.Timer),
	}
}

// SuppressExpiryWebhook disables the automatic expiry webhook (test helper).
func (p *MockProcessor) SuppressExpiryWebhook(suppress bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.suppressExpiry = suppress
}

// SetWebhookURL updates the webhook endpoint (used once the test server URL
// is known).
func (p *MockProcessor) SetWebhookURL(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.webhookURL = strings.TrimSuffix(url, "/")
}

// SetCallbackToken sets the shared webhook verification token the processor
// sends as the x-callback-token header.
func (p *MockProcessor) SetCallbackToken(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callbackToken = token
}

// CreateSession creates a simulated payment session and schedules the expiry
// webhook at the session deadline.
func (p *MockProcessor) CreateSession(
	ctx context.Context,
	refID string,
	amountRupiah int,
	currency string,
	expiresAt time.Time,
	allowedChannels []string,
	email string,
) (*domain.SessionResult, error) {
	providerRef := fmt.Sprintf("ps-mock-%s", refID)
	result := &domain.SessionResult{
		ProviderRef:    providerRef,
		PaymentLinkURL: fmt.Sprintf("https://mock.checkout/%s", refID),
		ExpiresAt:      expiresAt,
	}

	p.mu.Lock()
	p.sessions[providerRef] = result
	if !p.suppressExpiry {
		p.timers[providerRef] = time.AfterFunc(time.Until(expiresAt), func() {
			p.postWebhook("payment_session.expired", refID, providerRef)
		})
	}
	p.mu.Unlock()

	return result, nil
}

// CancelSession cancels the pending expiry timer.
func (p *MockProcessor) CancelSession(ctx context.Context, providerRef string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if timer, ok := p.timers[providerRef]; ok {
		timer.Stop()
		delete(p.timers, providerRef)
	}
	return nil
}

// GetSession returns the stored session result.
func (p *MockProcessor) GetSession(ctx context.Context, providerRef string) (*domain.SessionResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	result, ok := p.sessions[providerRef]
	if !ok {
		return nil, fmt.Errorf("mock session %s not found", providerRef)
	}
	return result, nil
}

// postWebhook simulates the gateway notifying our webhook endpoint.
func (p *MockProcessor) postWebhook(event, referenceID, providerRef string) {
	if p.webhookURL == "" {
		return
	}
	status := "EXPIRED"
	if event == "payment_session.completed" {
		status = "COMPLETED"
	}
	body, _ := json.Marshal(map[string]interface{}{
		"event": event,
		"data": map[string]string{
			"reference_id":       referenceID,
			"payment_session_id": providerRef,
			"status":             status,
		},
	})
	req, err := http.NewRequest(http.MethodPost, p.webhookURL, bytes.NewReader(body))
	if err != nil {
		fmt.Printf("mock webhook request failed: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-callback-token", p.callbackToken)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		fmt.Printf("mock webhook POST failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
}

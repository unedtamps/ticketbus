package processor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// MockProcessor implements domain.PaymentProcessor with a simulated gateway.
// It stores payment sessions in memory and exposes them via GetSession.
//
// It makes no outbound calls. Provider webhooks are delivered explicitly by
// whoever drives the flow, so tests choose the exact event and observe the
// result immediately instead of waiting on a timer.
type MockProcessor struct {
	mu           sync.Mutex
	sessions     map[string]*domain.SessionResult
	payouts      map[string]*domain.PayoutResult
	payoutCalls  int
	payoutErr    error
	payoutStatus string
}

// NewMockProcessor creates a new mock payment processor.
func NewMockProcessor() *MockProcessor {
	return &MockProcessor{
		sessions: make(map[string]*domain.SessionResult),
		payouts:  make(map[string]*domain.PayoutResult),
		// Real gateways accept payouts asynchronously: the create response is
		// non-terminal and settlement arrives later via webhook or polling.
		// Tests drive the confirmation explicitly.
		payoutStatus: domain.GatewayPayoutPending,
	}
}

// CreateSession creates a simulated payment session.
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
	p.mu.Unlock()

	return result, nil
}

// CancelSession satisfies domain.PaymentProcessor. The mock has no gateway-side
// session to close, so this is a no-op that keeps the stored session readable
// through GetSession.
func (p *MockProcessor) CancelSession(ctx context.Context, providerRef string) error {
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

// CreatePayout simulates a successful gateway payout. The provider reference is
// deterministic so e2e assertions can recognise it; the reported status is the
// currently configured one (SetPayoutStatus).
func (p *MockProcessor) CreatePayout(
	ctx context.Context,
	req domain.PayoutRequest,
) (*domain.PayoutResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.payoutCalls++
	if p.payoutErr != nil {
		return nil, p.payoutErr
	}
	ref := "dis-mock-" + req.ReferenceID
	result := &domain.PayoutResult{ProviderRef: ref, Status: p.payoutStatus}
	p.payouts[ref] = result
	return result, nil
}

// GetPayout reports the gateway state of a payout created earlier, carrying
// the currently configured status: flipping it after creation simulates the
// gateway's async transition.
func (p *MockProcessor) GetPayout(
	ctx context.Context,
	payoutID string,
) (*domain.PayoutResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	stored, ok := p.payouts[payoutID]
	if !ok {
		return nil, fmt.Errorf("mock payout %s not found", payoutID)
	}
	return &domain.PayoutResult{
		ProviderRef: stored.ProviderRef,
		Status:      p.payoutStatus,
	}, nil
}

// SetPayoutError makes subsequent CreatePayout calls fail with err. Tests use
// it to exercise the failed/retry path deterministically.
func (p *MockProcessor) SetPayoutError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.payoutErr = err
}

// SetPayoutStatus sets the status CreatePayout and GetPayout report, letting
// tests drive the async confirmation flow (PENDING now, COMPLETED later).
func (p *MockProcessor) SetPayoutStatus(status string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.payoutStatus = status
}

// PayoutCalls returns how many CreatePayout calls were attempted.
func (p *MockProcessor) PayoutCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.payoutCalls
}

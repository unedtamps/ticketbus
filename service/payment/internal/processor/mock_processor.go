package processor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nedo/TicketSaas/service/payment/internal/domain"
)

// MockProcessor implements domain.PaymentProcessor with a simulated gateway.
// It stores payment sessions in memory and exposes them via GetSession.
//
// It makes no outbound calls. Provider webhooks are delivered explicitly by
// whoever drives the flow, so tests choose the exact event and observe the
// result immediately instead of waiting on a timer.
type MockProcessor struct {
	mu       sync.Mutex
	sessions map[string]*domain.SessionResult
}

// NewMockProcessor creates a new mock payment processor.
func NewMockProcessor() *MockProcessor {
	return &MockProcessor{
		sessions: make(map[string]*domain.SessionResult),
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

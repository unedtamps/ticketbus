package processor

import (
	"context"
	"fmt"
)

// MockProcessor implements domain.PaymentProcessor with a simulated payment gateway.
type MockProcessor struct{}

// NewMockProcessor creates a new mock payment processor.
func NewMockProcessor() *MockProcessor {
	return &MockProcessor{}
}

// Charge always succeeds and returns a simulated provider reference.
func (p *MockProcessor) Charge(
	ctx context.Context,
	txnID string,
	amountCents int,
	currency string,
) (string, error) {
	return fmt.Sprintf("mock_%s", txnID[:8]), nil
}

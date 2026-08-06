package application

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"github.com/nedo/TicketSaas/internal/payment/domain"
)

// Checkout initiates a payment and simulates an async provider callback.
// Only meaningful for the mock provider; real providers redirect the customer
// to the hosted checkout page and notify via webhook.
func (s *PaymentService) Checkout(ctx context.Context, txnID string) (*domain.Transaction, error) {
	txn, err := s.txnRepo.FindByID(ctx, txnID)
	if err != nil {
		return nil, domain.ErrTransactionNotFound
	}
	if txn.Status != domain.StatusInitiated && txn.Status != domain.StatusPending {
		return nil, domain.ErrAlreadyProcessed
	}

	go func() {
		if s.webhookURL == "" {
			return
		}
		time.Sleep(15 * time.Second)
		if rand.Intn(4) == 0 {
			s.logger.Warn("mock webhook not called (simulated provider failure)", "txn_id", txnID)
			return
		}
		body := strings.NewReader(
			`{"event":"payment_session.completed","data":{"reference_id":"` + txn.BookingID + `","payment_session_id":"` + txn.ProviderRef + `","status":"COMPLETED"}}`,
		)
		resp, err := s.httpClient.Post(s.webhookURL+"/mock", "application/json", body)
		if err != nil {
			s.logger.Error("mock webhook POST failed", "txn_id", txnID, "error", err)
			return
		}
		resp.Body.Close()
		s.logger.Info("mock webhook POST succeeded", "txn_id", txnID, "status", resp.StatusCode)
	}()

	return txn, nil
}

// CheckoutByBooking finds the transaction by booking ID and initiates checkout.
func (s *PaymentService) CheckoutByBooking(
	ctx context.Context,
	bookingID string,
) (*domain.Transaction, error) {
	txn, err := s.txnRepo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, domain.ErrTransactionNotFound
	}
	return s.Checkout(ctx, txn.ID)
}

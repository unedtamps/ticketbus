package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/nedo/TicketSaas/internal/payment/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// sessionWebhookPayload is the Xendit payment session webhook envelope.
type sessionWebhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		ReferenceID      string `json:"reference_id"`
		PaymentSessionID string `json:"payment_session_id"`
		Status           string `json:"status"`
	} `json:"data"`
}

// HandleSessionWebhook applies a verified gateway webhook directly to the
// transaction row. Terminal transitions are guarded by UpdateStatusIfPending,
// so duplicates and concurrent deliveries are idempotent. When the
// transaction does not exist yet the caller returns a non-2xx so the gateway
// retries.
func (s *PaymentService) HandleSessionWebhook(
	ctx context.Context,
	provider string,
	payload []byte,
) error {
	var event sessionWebhookPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	if event.Data.ReferenceID == "" {
		return errors.New("webhook missing reference_id")
	}

	txn, err := s.txnRepo.FindByBookingID(ctx, event.Data.ReferenceID)
	if err != nil {
		return err
	}

	switch event.Event {
	case "payment_session.completed":
		applied, err := s.txnRepo.UpdateStatusIfPending(
			ctx,
			txn.ID,
			domain.StatusCompleted,
			txn.ProviderRef,
		)
		if err != nil {
			return err
		}
		if applied {
			_ = s.outbox.Insert(ctx, "payment.completed", txn.ID, sdomain.PaymentCompleted{
				TransactionID: txn.ID,
				BookingID:     txn.BookingID,
				EventID:       txn.EventID,
				UserID:        txn.UserID,
				At:            time.Now(),
			})
			s.logger.Info("payment completed", "txn_id", txn.ID)
			return nil
		}
		// Not pending: either a duplicate delivery or the booking was already
		// expired when the money arrived — the latter needs an auto refund.
		if txn.Status == domain.StatusExpired {
			s.requestLatePaymentRefund(ctx, txn)
		}

	case "payment_session.expired":
		applied, err := s.txnRepo.UpdateStatusIfPending(
			ctx,
			txn.ID,
			domain.StatusExpired,
			txn.ProviderRef,
		)
		if err != nil {
			return err
		}
		if applied {
			_ = s.outbox.Insert(ctx, "payment.expired", txn.ID, sdomain.PaymentExpired{
				TransactionID: txn.ID,
				BookingID:     txn.BookingID,
				EventID:       txn.EventID,
				UserID:        txn.UserID,
				Reason:        domain.GatewayExpiredReason,
				At:            time.Now(),
			})
			s.logger.Info("payment expired (gateway)", "txn_id", txn.ID)
		}

	default:
		s.logger.Info("ignoring webhook event", "event", event.Event, "txn_id", txn.ID)
	}
	return nil
}

// requestLatePaymentRefund creates a refund request when a payment completed
// after the booking was already expired. Idempotent per transaction.
func (s *PaymentService) requestLatePaymentRefund(ctx context.Context, txn *domain.Transaction) {
	refund := &domain.RefundRequest{
		ID:             uuid.NewString(),
		EventID:        txn.EventID,
		BookingID:      txn.BookingID,
		TransactionID:  txn.ID,
		CustomerEmail:  txn.CustomerEmail,
		AmountCents:    int64(txn.AmountCents),
		Currency:       txn.Currency,
		Status:         domain.RefundPending,
		Reason:         "late_payment",
		IdempotencyKey: "late-payment:" + txn.ID,
	}
	if err := s.refundRepo.Create(ctx, refund); err != nil {
		s.logger.Error("failed to create late payment refund", "txn_id", txn.ID, "error", err)
		return
	}
	_ = s.txnRepo.UpdateRefundStatus(ctx, txn.ID, "pending")
	s.logger.Warn("late payment refunded", "txn_id", txn.ID)
}

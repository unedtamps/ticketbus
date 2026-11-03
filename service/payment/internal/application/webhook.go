package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// sessionWebhookPayload is the Xendit payment session webhook envelope.
// Real Xendit payloads carry the session id in data.payment_session_id;
// documented examples use data.id — both are parsed defensively.
type sessionWebhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		ReferenceID      string `json:"reference_id"`
		PaymentSessionID string `json:"payment_session_id"`
		ID               string `json:"id"`
		Status           string `json:"status"`
	} `json:"data"`
}

// sessionID returns the payment session id, preferring the real payload field
// (data.payment_session_id) over the documented variant (data.id).
func (p *sessionWebhookPayload) sessionID() string {
	if p.Data.PaymentSessionID != "" {
		return p.Data.PaymentSessionID
	}
	return p.Data.ID
}

// HandleSessionWebhook applies a verified gateway webhook directly to the
// transaction row. Terminal transitions are guarded by TransitionIfActive,
// so duplicates and concurrent deliveries are idempotent. When the
// transaction does not exist yet the caller returns a non-2xx so the gateway
// retries.
func (s *PaymentService) HandleSessionWebhook(
	ctx context.Context,
	payload []byte,
) error {
	var event sessionWebhookPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidWebhook, err)
	}
	if event.Data.ReferenceID == "" {
		return fmt.Errorf("%w: missing reference_id", domain.ErrInvalidWebhook)
	}
	// Gateway test payloads (and sessions created outside our flow) carry a
	// reference_id that is not one of our booking UUIDs. Postgres answers a
	// non-UUID input with a syntax error — not ErrNoRows — so validate before
	// querying and acknowledge: no retry can ever make this match a booking.
	if _, err := uuid.Parse(event.Data.ReferenceID); err != nil {
		s.logger.Warn("session webhook with non-booking reference, ignoring",
			"reference_id", event.Data.ReferenceID,
			"event", event.Event,
		)
		return nil
	}

	txn, err := s.txnRepo.FindByBookingID(ctx, event.Data.ReferenceID)
	if err != nil {
		return err
	}

	// The session referenced by the webhook must be the one stored on the
	// transaction. A mismatch means the delivery does not belong to this
	// transaction — acknowledge and ignore instead of retrying forever.
	if sid := event.sessionID(); sid != "" && sid != txn.ProviderRef {
		s.logger.Warn(
			"webhook session mismatch, ignoring",
			"txn_id", txn.ID,
			"webhook_session_id", sid,
			"provider_ref", txn.ProviderRef,
		)
		return nil
	}

	switch event.Event {
	case "payment_session.completed":
		// Transition and the payment.completed outbox row commit together: if
		// the row were lost after a standalone transition, ticketing would
		// never learn about the money — the transition never re-fires, and a
		// redelivered webhook is answered as a duplicate.
		applied, err := s.txnRepo.CompleteIfActive(ctx, txn)
		if err != nil {
			return err
		}
		if applied {
			s.logger.Info("payment completed", "txn_id", txn.ID)
			return nil
		}
		// Not active: either a duplicate delivery or the booking was already
		// expired when the money arrived — the latter needs an auto refund.
		if txn.Status == domain.StatusExpired {
			s.requestLatePaymentRefund(ctx, txn)
		}

	case "payment_session.expired":
		// Same atomic pair as the completed path: the transition and its
		// payment.expired outbox row commit together.
		applied, err := s.txnRepo.ExpireIfActive(ctx, txn, domain.GatewayExpiredReason)
		if err != nil {
			return err
		}
		if applied {
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
		AmountRupiah:   int64(txn.AmountRupiah),
		Currency:       txn.Currency,
		Status:         domain.RefundPending,
		Reason:         "late_payment",
		IdempotencyKey: "late-payment:" + txn.ID,
		ProviderRef:    txn.ProviderRef,
	}
	if err := s.refundRepo.Create(ctx, refund); err != nil {
		s.logger.Error("failed to create late payment refund", "txn_id", txn.ID, "error", err)
		return
	}
	_ = s.txnRepo.UpdateRefundStatus(ctx, txn.ID, "pending")
	s.logger.Warn("late payment refunded", "txn_id", txn.ID)
}

// payoutWebhookPayload parses a gateway payout callback. The legacy v2
// callback carries the payout object at the top level; other providers wrap it
// in {event, data}. Both shapes are parsed, and the decision is made from the
// payout's status field — not from the event name.
type payoutWebhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		ReferenceID string `json:"reference_id"`
		FailureCode string `json:"failure_code"`
	} `json:"data"`
	ID          string `json:"id"`
	Status      string `json:"status"`
	FailureCode string `json:"failure_code"`
}

// fields resolves the payout identity and outcome across both payload shapes.
func (p *payoutWebhookPayload) fields() (id, status, failureCode string) {
	id, status, failureCode = p.Data.ID, p.Data.Status, p.Data.FailureCode
	if id == "" {
		id = p.ID
	}
	if status == "" {
		status = p.Status
	}
	if failureCode == "" {
		failureCode = p.FailureCode
	}
	return id, status, failureCode
}

// HandlePayoutWebhook applies a verified gateway payout callback to the payout
// ledger. The webhook is a fast path only — the dispatcher's reconciliation
// poll reaches the same outcome if a delivery is lost — so anything that is
// not an actionable terminal confirmation is acknowledged with success and
// logged: only transient database failures return an error (a non-2xx makes
// the gateway retry).
func (s *PaymentService) HandlePayoutWebhook(
	ctx context.Context,
	payload []byte,
) error {
	var event payoutWebhookPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		s.logger.Warn("payout webhook payload not parseable", "error", err)
		return nil
	}
	payoutID, gatewayStatus, failureCode := event.fields()
	if payoutID == "" {
		s.logger.Warn("payout webhook missing payout id", "event", event.Event)
		return nil
	}

	payout, err := s.payoutRepo.FindByProviderRef(ctx, payoutID)
	if err != nil {
		if errors.Is(err, domain.ErrNoRows) {
			// Not a payout we know. Retrying cannot fix that, so acknowledge.
			s.logger.Warn("payout webhook for unknown provider ref",
				"provider_ref", payoutID, "event", event.Event)
			return nil
		}
		return err
	}

	// Terminal already (e.g. REVERSED arriving after a COMPLETED
	// confirmation): log loudly, keep the ledger as it is.
	if payout.Status != domain.PayoutStatusPending {
		s.logger.Warn("payout webhook ignored, attempt already final",
			"payout_id", payout.ID,
			"event_id", payout.EventID,
			"ledger_status", payout.Status,
			"gateway_status", gatewayStatus,
			"event", event.Event,
		)
		return nil
	}

	ledgerStatus, reason, terminal := domain.PayoutConfirmation(gatewayStatus, failureCode)
	if !terminal {
		s.logger.Info("payout webhook non-terminal, ignoring",
			"payout_id", payout.ID,
			"gateway_status", gatewayStatus,
			"event", event.Event,
		)
		return nil
	}

	if err := s.payoutRepo.Complete(ctx, payout.EventID, payout.Attempt,
		ledgerStatus, payout.AmountRupiah, payout.ProviderRef, reason); err != nil {
		return err
	}
	s.logger.Info("payout confirmed via webhook",
		"event_id", payout.EventID,
		"payout_id", payout.ID,
		"gateway_status", gatewayStatus,
		"status", ledgerStatus,
	)
	return nil
}

// MockPayoutWebhookConfirmation resolves the event's latest payout attempt and
// feeds HandlePayoutWebhook a synthetic gateway callback. It backs the dev-only
// mock endpoint and mirrors MockSessionWebhook: the caller cannot know the
// gateway reference upfront (the dispatcher records it after delivery), so the
// service resolves it. An attempt that is not dispatched yet surfaces
// domain.ErrPayoutNotDispatched so callers poll until it lands.
func (s *PaymentService) MockPayoutWebhookConfirmation(
	ctx context.Context,
	eventID, outcome string,
) error {
	payout, err := s.payoutRepo.FindLatestByEvent(ctx, eventID)
	if err != nil {
		return err
	}
	if payout.ProviderRef == "" {
		return domain.ErrPayoutNotDispatched
	}

	event, gatewayStatus, failureCode := "payout.succeeded", domain.GatewayPayoutCompleted, ""
	if outcome == "failed" {
		event, gatewayStatus, failureCode = "payout.failed", domain.GatewayPayoutFailed, "INSUFFICIENT_BALANCE"
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"event": event,
		"data": map[string]string{
			"id":           payout.ProviderRef,
			"status":       gatewayStatus,
			"failure_code": failureCode,
		},
	})
	return s.HandlePayoutWebhook(ctx, payload)
}

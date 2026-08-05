package application

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nedo/TicketSaas/internal/payment/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
)

// PaymentService orchestrates payment operations.
type PaymentService struct {
	txnRepo                domain.TransactionRepository
	refundRepo             domain.RefundRepository
	processor              domain.PaymentProcessor
	consumer               domain.EventConsumer
	outbox                 outbox.StoreInterface
	logger                 *slog.Logger
	webhookURL             string
	httpClient             *http.Client
	provider               string
	gatewayExpiryBufferMin int
	allowedChannels        []string
}

// NewPaymentService creates a new PaymentService.
func NewPaymentService(
	txnRepo domain.TransactionRepository,
	refundRepo domain.RefundRepository,
	processor domain.PaymentProcessor,
	consumer domain.EventConsumer,
	ob outbox.StoreInterface,
	logger *slog.Logger,
	webhookURL string,
	provider string,
	gatewayExpiryBufferMin int,
	allowedChannels []string,
) *PaymentService {
	return &PaymentService{
		txnRepo:                txnRepo,
		refundRepo:             refundRepo,
		processor:              processor,
		consumer:               consumer,
		outbox:                 ob,
		logger:                 logger,
		webhookURL:             webhookURL,
		httpClient:             &http.Client{Timeout: 10 * time.Second},
		provider:               provider,
		gatewayExpiryBufferMin: gatewayExpiryBufferMin,
		allowedChannels:        allowedChannels,
	}
}

// CreateTxnForBooking creates a pending transaction for a booking. Called
// synchronously by the ticketing service during reserve, so the transaction
// row always exists before any gateway activity. Idempotent per booking.
func (s *PaymentService) CreateTxnForBooking(
	ctx context.Context,
	bookingID, eventID, userID, email string,
	amountCents int,
	expiresAt time.Time,
) (*domain.Transaction, error) {
	txn := &domain.Transaction{
		ID:            uuid.NewString(),
		UserID:        userID,
		BookingID:     bookingID,
		EventID:       eventID,
		AmountCents:   amountCents,
		Currency:      "IDR",
		Status:        domain.StatusPending,
		Provider:      s.provider,
		CustomerEmail: email,
		RefundStatus:  "none",
		ExpiresAt:     &expiresAt,
	}
	if err := s.txnRepo.Create(ctx, txn); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			existing, ferr := s.txnRepo.FindByBookingID(ctx, bookingID)
			if ferr != nil {
				return nil, ferr
			}
			return existing, nil
		}
		return nil, err
	}
	return txn, nil
}

// InitiatePayment creates the gateway payment session for a booking. Allowed
// only while at least one minute of the booking hold remains. Amount and
// currency come from the stored transaction, never from the request.
func (s *PaymentService) InitiatePayment(
	ctx context.Context,
	bookingID, userID string,
) (*domain.Transaction, *domain.SessionResult, error) {
	txn, err := s.txnRepo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, nil, domain.ErrTransactionNotFound
	}
	if txn.Status != domain.StatusPending {
		return nil, nil, domain.ErrAlreadyProcessed
	}
	if txn.ExpiresAt == nil || !txn.ExpiresAt.After(time.Now()) ||
		time.Until(*txn.ExpiresAt) < time.Minute {
		return nil, nil, domain.ErrTransactionExpired
	}
	if txn.UserID != userID {
		return nil, nil, domain.ErrTransactionNotFound
	}

	gatewayExpiresAt := txn.ExpiresAt.Add(-time.Duration(s.gatewayExpiryBufferMin) * time.Minute)
	result, err := s.processor.CreateSession(
		ctx,
		bookingID,
		txn.AmountCents,
		txn.Currency,
		gatewayExpiresAt,
		s.allowedChannels,
		txn.CustomerEmail,
	)
	if err != nil {
		return nil, nil, err
	}

	if err := s.txnRepo.UpdateSession(
		ctx,
		txn.ID,
		result.ProviderRef,
		result.PaymentLinkURL,
	); err != nil {
		return nil, nil, err
	}

	s.logger.Info(
		"payment session initiated",
		"booking_id", bookingID,
		"session_id", result.ProviderRef,
	)
	return txn, result, nil
}

// GetPaymentStatus returns the transaction (including the stored payment
// link) for a booking, so the customer can re-open the checkout page.
func (s *PaymentService) GetPaymentStatus(
	ctx context.Context,
	bookingID string,
) (*domain.Transaction, error) {
	txn, err := s.txnRepo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, domain.ErrTransactionNotFound
	}
	return txn, nil
}

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

// ProcessExpired expires pending transactions whose deadline passed and whose
// gateway expiry webhook never arrived (or was never created because the
// customer did not initiate a payment session).
func (s *PaymentService) ProcessExpired(ctx context.Context, limit int) error {
	txns, err := s.txnRepo.ListPendingExpired(ctx, time.Now(), limit)
	if err != nil {
		return err
	}
	for i := range txns {
		txn := &txns[i]
		if txn.ProviderRef != "" {
			if err := s.processor.CancelSession(ctx, txn.ProviderRef); err != nil {
				s.logger.Warn("failed to cancel session at gateway", "txn_id", txn.ID, "error", err)
			}
		}
		applied, err := s.txnRepo.UpdateStatusIfPending(
			ctx,
			txn.ID,
			domain.StatusExpired,
			txn.ProviderRef,
		)
		if err != nil {
			s.logger.Error("failed to expire transaction", "txn_id", txn.ID, "error", err)
			continue
		}
		if !applied {
			continue
		}
		_ = s.outbox.Insert(ctx, "payment.expired", txn.ID, sdomain.PaymentExpired{
			TransactionID: txn.ID,
			BookingID:     txn.BookingID,
			EventID:       txn.EventID,
			UserID:        txn.UserID,
			Reason:        "internal_expiry_fallback",
			At:            time.Now(),
		})
		s.logger.Info("expired transaction (poller)", "txn_id", txn.ID)
	}
	return nil
}

// HandleReservationCancelled ends a pending transaction when its booking is
// released by the customer, cancelling the gateway session if any.
func (s *PaymentService) HandleReservationCancelled(ctx context.Context, bookingID string) error {
	txn, err := s.txnRepo.FindByBookingID(ctx, bookingID)
	if err != nil {
		s.logger.Warn("no transaction found for cancelled reservation", "booking_id", bookingID)
		return nil
	}
	if txn.Status != domain.StatusPending {
		s.logger.Info(
			"transaction already processed, skipping cancellation",
			"booking_id",
			bookingID,
			"status",
			txn.Status,
		)
		return nil
	}
	if txn.ProviderRef != "" {
		if err := s.processor.CancelSession(ctx, txn.ProviderRef); err != nil {
			s.logger.Warn("failed to cancel session", "txn_id", txn.ID, "error", err)
		}
	}
	if _, err := s.txnRepo.UpdateStatusIfPending(
		ctx,
		txn.ID,
		domain.StatusExpired,
		txn.ProviderRef,
	); err != nil {
		return err
	}
	s.logger.Info(
		"cancelled transaction for released reservation",
		"booking_id",
		bookingID,
		"txn_id",
		txn.ID,
	)
	return nil
}

// HandleEventCancelled refunds completed transactions and cancels pending
// sessions when an event is cancelled. Idempotent per transaction.
func (s *PaymentService) HandleEventCancelled(ctx context.Context, eventID string) error {
	txns, err := s.txnRepo.ListByEventID(ctx, eventID)
	if err != nil {
		return err
	}

	for i := range txns {
		txn := &txns[i]
		switch txn.Status {
		case domain.StatusCompleted:
			refund := &domain.RefundRequest{
				ID:             uuid.NewString(),
				EventID:        txn.EventID,
				BookingID:      txn.BookingID,
				TransactionID:  txn.ID,
				CustomerEmail:  txn.CustomerEmail,
				AmountCents:    int64(txn.AmountCents),
				Currency:       txn.Currency,
				Status:         domain.RefundPending,
				Reason:         "event_cancelled",
				IdempotencyKey: "event-cancelled:" + txn.ID,
			}
			if err := s.refundRepo.Create(ctx, refund); err != nil {
				return err
			}
			if err := s.txnRepo.UpdateRefundStatus(ctx, txn.ID, "pending"); err != nil {
				return err
			}
			s.logger.Info(
				"refund requested for cancelled event",
				"event_id",
				eventID,
				"transaction_id",
				txn.ID,
			)

		case domain.StatusPending:
			if txn.ProviderRef != "" {
				if err := s.processor.CancelSession(ctx, txn.ProviderRef); err != nil {
					s.logger.Warn("failed to cancel session", "txn_id", txn.ID, "error", err)
				}
			}
			if _, err := s.txnRepo.UpdateStatusIfPending(
				ctx,
				txn.ID,
				domain.StatusExpired,
				txn.ProviderRef,
			); err != nil {
				return err
			}
			s.logger.Info(
				"cancelled pending payment for cancelled event",
				"event_id",
				eventID,
				"transaction_id",
				txn.ID,
			)
		}
	}
	return nil
}

// Checkout initiates a payment and simulates an async provider callback.
// Only meaningful for the mock provider; real providers redirect the customer
// to the hosted checkout page and notify via webhook.
func (s *PaymentService) Checkout(ctx context.Context, txnID string) (*domain.Transaction, error) {
	txn, err := s.txnRepo.FindByID(ctx, txnID)
	if err != nil {
		return nil, domain.ErrTransactionNotFound
	}
	if txn.Status != domain.StatusPending {
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

// GetTransaction returns a transaction by ID.
func (s *PaymentService) GetTransaction(
	ctx context.Context,
	txnID string,
) (*domain.Transaction, error) {
	return s.txnRepo.FindByID(ctx, txnID)
}

// ListMyTransactions returns transactions for a user.
func (s *PaymentService) ListMyTransactions(
	ctx context.Context,
	userID string,
) ([]domain.Transaction, error) {
	return s.txnRepo.ListByUser(ctx, userID)
}

// StartExpiryPoller expires overdue transactions in the background.
func (s *PaymentService) StartExpiryPoller(ctx context.Context, intervalSec int) {
	go func() {
		time.Sleep(500 * time.Millisecond)
		ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.ProcessExpired(ctx, 100); err != nil {
					s.logger.Error("expiry poll failed", "error", err)
				}
			}
		}
	}()
}

// StartConsumer starts the reservation and event lifecycle listeners.
func (s *PaymentService) StartConsumer(ctx context.Context) error {
	s.consumer.OnReservationCancelled(ctx, func(ctx context.Context, bookingID string) error {
		s.logger.Info("reservation cancelled received", "booking_id", bookingID)
		return s.HandleReservationCancelled(ctx, bookingID)
	})

	s.consumer.OnEventCancelled(ctx, func(ctx context.Context, eventID string) error {
		s.logger.Info("event cancelled received", "event_id", eventID)
		return s.HandleEventCancelled(ctx, eventID)
	})

	go func() {
		if err := s.consumer.Start(ctx); err != nil {
			s.logger.Error("consumer error", "error", err)
		}
	}()
	return nil
}

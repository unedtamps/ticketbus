package application

import (
	"context"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nedo/TicketSaas/internal/payment/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
)

// PaymentService orchestrates payment operations.
type PaymentService struct {
	txnRepo    domain.TransactionRepository
	refundRepo domain.RefundRepository
	processor  domain.PaymentProcessor
	consumer   domain.EventConsumer
	outbox     outbox.StoreInterface
	logger     *slog.Logger
	webhookURL string
	httpClient *http.Client
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
) *PaymentService {
	return &PaymentService{
		txnRepo:    txnRepo,
		refundRepo: refundRepo,
		processor:  processor,
		consumer:   consumer,
		outbox:     ob,
		logger:     logger,
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// InitiatePayment creates a new transaction when a reservation is created.
func (s *PaymentService) InitiatePayment(
	ctx context.Context,
	bookingID string,
	amountCents int,
	userID string,
) (*domain.Transaction, error) {
	return s.initiatePayment(ctx, bookingID, "", amountCents, userID)
}

// InitiatePaymentForEvent creates a transaction with its owning Ticketing event.
func (s *PaymentService) InitiatePaymentForEvent(
	ctx context.Context,
	bookingID, eventID string,
	amountCents int,
	userID string,
) (*domain.Transaction, error) {
	return s.initiatePayment(ctx, bookingID, eventID, amountCents, userID)
}

func (s *PaymentService) initiatePayment(
	ctx context.Context,
	bookingID, eventID string,
	amountCents int,
	userID string,
) (*domain.Transaction, error) {
	txn := &domain.Transaction{
		ID:           uuid.NewString(),
		UserID:       userID,
		BookingID:    bookingID,
		EventID:      eventID,
		AmountCents:  amountCents,
		Currency:     "USD",
		Status:       domain.StatusPending,
		Provider:     "mock",
		RefundStatus: "none",
	}
	if err := s.txnRepo.Create(ctx, txn); err != nil {
		return nil, err
	}
	return txn, nil
}

// Checkout initiates a payment and simulates async provider callback.
func (s *PaymentService) Checkout(ctx context.Context, txnID string) (*domain.Transaction, error) {
	txn, err := s.txnRepo.FindByID(ctx, txnID)
	if err != nil {
		return nil, domain.ErrTransactionNotFound
	}
	if txn.Status != domain.StatusPending {
		return nil, domain.ErrAlreadyProcessed
	}

	providerRef, err := s.processor.Charge(ctx, txn.ID, txn.AmountCents, txn.Currency)
	if err != nil {
		txn.Status = domain.StatusExpired
		_ = s.txnRepo.UpdateStatus(ctx, txn.ID, domain.StatusExpired, "")
		_ = s.outbox.Insert(ctx, "payment.failed", txn.ID, sdomain.PaymentFailed{
			TransactionID: txn.ID,
			BookingID:     txn.BookingID,
			EventID:       txn.EventID,
			UserID:        txn.UserID,
			Reason:        err.Error(),
			At:            time.Now(),
		})
		return txn, err
	}

	txn.Status = domain.StatusPending
	txn.ProviderRef = providerRef
	_ = s.txnRepo.UpdateStatus(ctx, txn.ID, domain.StatusPending, providerRef)

	go func() {
		if s.webhookURL == "" {
			return
		}
		delay := 15 * time.Second
		time.Sleep(delay)
		if rand.Intn(4) == 0 {
			s.logger.Warn("mock webhook not called (simulated provider failure)", "txn_id", txnID)
			return
		}

		body := strings.NewReader(`{"transaction_id":"` + txnID + `"}`)
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

// ConfirmPayment completes a processing transaction (called by webhook).
func (s *PaymentService) ConfirmPayment(ctx context.Context, txnID string) error {
	txn, err := s.txnRepo.FindByID(ctx, txnID)
	if err != nil {
		return domain.ErrTransactionNotFound
	}
	if txn.Status != domain.StatusPending {
		s.logger.Warn(
			"cannot confirm non-processing transaction",
			"txn_id",
			txnID,
			"status",
			txn.Status,
		)
		return domain.ErrAlreadyProcessed
	}
	txn.Status = domain.StatusSuccess
	if err := s.txnRepo.UpdateStatus(
		ctx,
		txn.ID,
		domain.StatusSuccess,
		txn.ProviderRef,
	); err != nil {
		return err
	}
	_ = s.outbox.Insert(ctx, "payment.completed", txn.ID, sdomain.PaymentCompleted{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
		EventID:       txn.EventID,
		UserID:        txn.UserID,
		At:            time.Now(),
	})
	s.logger.Info("payment confirmed", "txn_id", txnID)
	return nil
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

// CheckoutByBooking finds the transaction by booking ID and initiates checkout.
// Useful when the frontend only has the booking ID (not transaction ID).
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

// HandleReservationCancelled ends a pending transaction when its booking is
// released by the customer. The gateway payment request is voided in Phase 2
// once the processor exposes CancelPayment.
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
	if err := s.txnRepo.UpdateStatus(
		ctx,
		txn.ID,
		domain.StatusExpired,
		"reservation_cancelled",
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

// HandleEventCancelled creates refund requests for successful transactions of
// a cancelled event. Idempotent per transaction via the idempotency key.
func (s *PaymentService) HandleEventCancelled(ctx context.Context, eventID string) error {
	txns, err := s.txnRepo.ListByEventID(ctx, eventID)
	if err != nil {
		return err
	}

	for i := range txns {
		txn := &txns[i]
		if txn.Status != domain.StatusSuccess {
			continue
		}
		refund := &domain.RefundRequest{
			ID:             uuid.NewString(),
			EventID:        txn.EventID,
			BookingID:      txn.BookingID,
			TransactionID:  txn.ID,
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
		s.logger.Info("refund requested for cancelled event", "event_id", eventID, "transaction_id", txn.ID)
	}
	return nil
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

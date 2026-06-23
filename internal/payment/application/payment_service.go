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
	processor domain.PaymentProcessor,
	consumer domain.EventConsumer,
	ob outbox.StoreInterface,
	logger *slog.Logger,
	webhookURL string,
) *PaymentService {
	return &PaymentService{
		txnRepo:    txnRepo,
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
	txn := &domain.Transaction{
		ID:          uuid.NewString(),
		UserID:      userID,
		BookingID:   bookingID,
		AmountCents: amountCents,
		Currency:    "USD",
		Status:      domain.StatusInitiated,
		Provider:    "mock",
	}
	if err := s.txnRepo.Create(ctx, txn); err != nil {
		return nil, err
	}
	_ = s.outbox.Insert(ctx, "payment.initiated", txn.ID, sdomain.PaymentInitiated{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
		UserID:        txn.UserID,
		AmountCents:   txn.AmountCents,
		At:            time.Now(),
	})
	return txn, nil
}

// Checkout initiates a payment and simulates async provider callback.
func (s *PaymentService) Checkout(ctx context.Context, txnID string) (*domain.Transaction, error) {
	txn, err := s.txnRepo.FindByID(ctx, txnID)
	if err != nil {
		return nil, domain.ErrTransactionNotFound
	}
	if txn.Status != domain.StatusInitiated {
		return nil, domain.ErrAlreadyProcessed
	}

	providerRef, err := s.processor.Charge(ctx, txn.ID, txn.AmountCents, txn.Currency)
	if err != nil {
		txn.Status = domain.StatusFailed
		_ = s.txnRepo.UpdateStatus(ctx, txn.ID, domain.StatusFailed, "")
		_ = s.outbox.Insert(ctx, "payment.failed", txn.ID, sdomain.PaymentFailed{
			TransactionID: txn.ID,
			BookingID:     txn.BookingID,
			UserID:        txn.UserID,
			Reason:        err.Error(),
			At:            time.Now(),
		})
		return txn, err
	}

	txn.Status = domain.StatusProcessing
	txn.ProviderRef = providerRef
	_ = s.txnRepo.UpdateStatus(ctx, txn.ID, domain.StatusProcessing, providerRef)

	go func() {
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
	if txn.Status != domain.StatusProcessing {
		s.logger.Warn(
			"cannot confirm non-processing transaction",
			"txn_id",
			txnID,
			"status",
			txn.Status,
		)
		return domain.ErrAlreadyProcessed
	}
	txn.Status = domain.StatusCompleted
	if err := s.txnRepo.UpdateStatus(
		ctx,
		txn.ID,
		domain.StatusCompleted,
		txn.ProviderRef,
	); err != nil {
		return err
	}
	_ = s.outbox.Insert(ctx, "payment.completed", txn.ID, sdomain.PaymentCompleted{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
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

// HandleReservationExpired cancels a pending transaction when a reservation expires.
func (s *PaymentService) HandleReservationExpired(ctx context.Context, bookingID string) error {
	txn, err := s.txnRepo.FindByBookingID(ctx, bookingID)
	if err != nil {
		s.logger.Warn("no transaction found for expired reservation", "booking_id", bookingID)
		return nil
	}
	if txn.Status != domain.StatusInitiated && txn.Status != domain.StatusProcessing {
		s.logger.Info(
			"transaction already processed, skipping expiry",
			"booking_id",
			bookingID,
			"status",
			txn.Status,
		)
		return nil
	}
	txn.Status = domain.StatusFailed
	if err := s.txnRepo.UpdateStatus(
		ctx,
		txn.ID,
		domain.StatusFailed,
		"reservation_expired",
	); err != nil {
		return err
	}
	_ = s.outbox.Insert(ctx, "payment.failed", txn.ID, sdomain.PaymentFailed{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
		UserID:        txn.UserID,
		Reason:        "reservation expired",
		At:            time.Now(),
	})
	s.logger.Info(
		"cancelled transaction for expired reservation",
		"booking_id",
		bookingID,
		"txn_id",
		txn.ID,
	)
	return nil
}

// StartConsumer starts the reservation listener.
func (s *PaymentService) StartConsumer(ctx context.Context) error {
	s.consumer.OnReservationCreated(
		ctx,
		func(ctx context.Context, bookingID string, amountCents int, userID string) error {
			s.logger.Info("reservation created received", "booking_id", bookingID)
			_, err := s.InitiatePayment(ctx, bookingID, amountCents, userID)
			return err
		},
	)

	s.consumer.OnReservationExpired(ctx, func(ctx context.Context, bookingID string) error {
		s.logger.Info("reservation expired received", "booking_id", bookingID)
		return s.HandleReservationExpired(ctx, bookingID)
	})

	go func() {
		if err := s.consumer.Start(ctx); err != nil {
			s.logger.Error("consumer error", "error", err)
		}
	}()
	return nil
}

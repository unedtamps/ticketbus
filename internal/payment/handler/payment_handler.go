package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nedo/TicketSaas/internal/payment/application"
	"github.com/nedo/TicketSaas/internal/payment/domain"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// PaymentHandler handles HTTP requests for the payment service.
type PaymentHandler struct {
	svc *application.PaymentService
}

// NewPaymentHandler creates a new PaymentHandler.
func NewPaymentHandler(svc *application.PaymentService) *PaymentHandler {
	return &PaymentHandler{svc: svc}
}

// Checkout handles POST /payments/:id/checkout.
func (h *PaymentHandler) Checkout(w http.ResponseWriter, r *http.Request) {
	txnID := chi.URLParam(r, "id")
	txn, err := h.svc.Checkout(r.Context(), txnID)
	if err != nil {
		if errors.Is(err, domain.ErrTransactionNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		if errors.Is(err, domain.ErrAlreadyProcessed) {
			sharedhttp.Error(w, http.StatusConflict, err.Error())
			return
		}
		sharedhttp.BadRequest(w, "payment failed")
		return
	}
	sharedhttp.OK(w, TransactionResponse{
		ID: txn.ID, BookingID: txn.BookingID, AmountCents: txn.AmountCents,
		Currency: txn.Currency, Status: txn.Status,
		CreatedAt: txn.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// CheckoutByBooking handles POST /payments/by-booking/:booking_id/checkout.
func (h *PaymentHandler) CheckoutByBooking(w http.ResponseWriter, r *http.Request) {
	bookingID := chi.URLParam(r, "booking_id")
	txn, err := h.svc.CheckoutByBooking(r.Context(), bookingID)
	if err != nil {
		if errors.Is(err, domain.ErrTransactionNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		if errors.Is(err, domain.ErrAlreadyProcessed) {
			sharedhttp.Error(w, http.StatusConflict, err.Error())
			return
		}
		sharedhttp.BadRequest(w, "payment failed")
		return
	}
	sharedhttp.OK(w, TransactionResponse{
		ID: txn.ID, BookingID: txn.BookingID, AmountCents: txn.AmountCents,
		Currency: txn.Currency, Status: txn.Status,
		CreatedAt: txn.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// GetStatus handles GET /payments/:id/status.
func (h *PaymentHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	txnID := chi.URLParam(r, "id")
	txn, err := h.svc.GetTransaction(r.Context(), txnID)
	if err != nil {
		sharedhttp.NotFound(w, "transaction not found")
		return
	}
	sharedhttp.OK(w, TransactionResponse{
		ID: txn.ID, BookingID: txn.BookingID, AmountCents: txn.AmountCents,
		Currency: txn.Currency, Status: txn.Status,
		CreatedAt: txn.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// Webhook handles POST /webhook/{provider} (called by payment providers).
func (h *PaymentHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if provider == "" {
		sharedhttp.BadRequest(w, "provider is required")
		return
	}

	var req struct {
		TransactionID string `json:"transaction_id"`
	}
	if err := sharedhttp.DecodeJSON(r, &req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}
	if req.TransactionID == "" {
		sharedhttp.BadRequest(w, "transaction_id is required")
		return
	}

	if err := h.svc.ConfirmPayment(r.Context(), req.TransactionID); err != nil {
		if errors.Is(err, domain.ErrTransactionNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		if errors.Is(err, domain.ErrAlreadyProcessed) {
			// Already completed — idempotent
			sharedhttp.OK(w, map[string]string{"status": "already_processed"})
			return
		}
		sharedhttp.InternalServerError(w, "failed to confirm payment")
		return
	}

	sharedhttp.OK(w, map[string]string{"status": "completed"})
}

// ListTransactions handles GET /payments.
func (h *PaymentHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	userID := sharedhttp.UserIDFromContext(r.Context())
	if userID == "" {
		sharedhttp.Unauthorized(w, "authentication required")
		return
	}
	txns, err := h.svc.ListMyTransactions(r.Context(), userID)
	if err != nil {
		sharedhttp.InternalServerError(w, "failed to list transactions")
		return
	}
	resp := make([]TransactionResponse, 0, len(txns))
	for _, t := range txns {
		resp = append(resp, TransactionResponse{
			ID: t.ID, BookingID: t.BookingID, AmountCents: t.AmountCents,
			Currency: t.Currency, Status: t.Status,
			CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	sharedhttp.OK(w, resp)
}

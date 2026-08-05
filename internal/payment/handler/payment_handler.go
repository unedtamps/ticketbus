package handler

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nedo/TicketSaas/internal/payment/application"
	"github.com/nedo/TicketSaas/internal/payment/domain"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// PaymentHandler handles HTTP requests for the payment service.
type PaymentHandler struct {
	svc           *application.PaymentService
	internalKey   string
	callbackToken string
}

// NewPaymentHandler creates a new PaymentHandler.
func NewPaymentHandler(
	svc *application.PaymentService,
	internalKey string,
	callbackToken string,
) *PaymentHandler {
	return &PaymentHandler{
		svc:           svc,
		internalKey:   internalKey,
		callbackToken: callbackToken,
	}
}

// requireInternalKey protects internal (service-to-service) routes.
func (h *PaymentHandler) requireInternalKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare(
			[]byte(r.Header.Get("X-Internal-Key")),
			[]byte(h.internalKey),
		) != 1 {
			sharedhttp.Unauthorized(w, "invalid internal key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CreateTxnForBooking handles POST /api/payments/internal (ticketing → payment).
func (h *PaymentHandler) CreateTxnForBooking(w http.ResponseWriter, r *http.Request) {
	var req InternalCreateRequest
	if err := sharedhttp.DecodeJSON(r, &req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}
	if req.BookingID == "" || req.UserID == "" || req.EventID == "" || req.AmountCents <= 0 {
		sharedhttp.BadRequest(w, "booking_id, event_id, user_id and amount_cents are required")
		return
	}

	txn, err := h.svc.CreateTxnForBooking(
		r.Context(),
		req.BookingID,
		req.EventID,
		req.UserID,
		req.Email,
		req.AmountCents,
		req.ExpiresAt,
	)
	if err != nil {
		sharedhttp.InternalServerError(w, "failed to create transaction")
		return
	}
	sharedhttp.Created(w, InternalCreateResponse{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
	})
}

// InitiatePayment handles POST /api/payments/booking/{booking_id}.
func (h *PaymentHandler) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	bookingID := chi.URLParam(r, "booking_id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	if userID == "" {
		sharedhttp.Unauthorized(w, "authentication required")
		return
	}

	txn, result, err := h.svc.InitiatePayment(r.Context(), bookingID, userID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrTransactionNotFound):
			sharedhttp.NotFound(w, "transaction not found")
		case errors.Is(err, domain.ErrAlreadyProcessed):
			sharedhttp.Error(w, http.StatusConflict, "payment already initiated")
		case errors.Is(err, domain.ErrTransactionExpired):
			sharedhttp.Error(w, http.StatusConflict, "booking has expired")
		default:
			sharedhttp.InternalServerError(w, "failed to initiate payment")
		}
		return
	}

	sharedhttp.OK(w, InitiateResponse{
		TransactionID:    txn.ID,
		PaymentSessionID: result.ProviderRef,
		PaymentLinkURL:   result.PaymentLinkURL,
		AmountCents:      txn.AmountCents,
		Currency:         txn.Currency,
		Status:           txn.Status,
		ExpiresAt:        formatTime(txn.ExpiresAt),
	})
}

// GetPaymentStatus handles GET /api/payments/booking/{booking_id}.
func (h *PaymentHandler) GetPaymentStatus(w http.ResponseWriter, r *http.Request) {
	bookingID := chi.URLParam(r, "booking_id")
	txn, err := h.svc.GetPaymentStatus(r.Context(), bookingID)
	if err != nil {
		if errors.Is(err, domain.ErrTransactionNotFound) {
			sharedhttp.NotFound(w, "transaction not found")
			return
		}
		sharedhttp.InternalServerError(w, "failed to load payment status")
		return
	}
	sharedhttp.OK(w, PaymentStatusResponse{
		TransactionID:  txn.ID,
		Status:         txn.Status,
		PaymentLinkURL: txn.PaymentLinkURL,
		AmountCents:    txn.AmountCents,
		Currency:       txn.Currency,
	})
}

// Checkout handles POST /payments/:id/checkout (mock pay simulation).
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
		ID: txn.ID, BookingID: txn.BookingID, EventID: txn.EventID, AmountCents: txn.AmountCents,
		Currency: txn.Currency, Status: txn.Status, RefundStatus: txn.RefundStatus,
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
		ID: txn.ID, BookingID: txn.BookingID, EventID: txn.EventID, AmountCents: txn.AmountCents,
		Currency: txn.Currency, Status: txn.Status, RefundStatus: txn.RefundStatus,
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
		ID: txn.ID, BookingID: txn.BookingID, EventID: txn.EventID, AmountCents: txn.AmountCents,
		Currency: txn.Currency, Status: txn.Status, RefundStatus: txn.RefundStatus,
		CreatedAt: txn.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// Webhook handles POST /webhook/{provider} (called by payment providers).
// The delivery is verified and applied to the transaction directly; when the
// transaction does not exist a non-2xx is returned so the gateway retries.
func (h *PaymentHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if provider == "" {
		sharedhttp.BadRequest(w, "provider is required")
		return
	}
	if provider == "xendit" {
		if subtle.ConstantTimeCompare(
			[]byte(r.Header.Get("x-callback-token")),
			[]byte(h.callbackToken),
		) != 1 {
			sharedhttp.Unauthorized(w, "invalid callback token")
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		sharedhttp.BadRequest(w, "failed to read body")
		return
	}
	if err := h.svc.HandleSessionWebhook(r.Context(), provider, body); err != nil {
		if errors.Is(err, domain.ErrNoRows) {
			sharedhttp.NotFound(w, "transaction not found")
			return
		}
		sharedhttp.BadRequest(w, "invalid webhook payload")
		return
	}

	sharedhttp.OK(w, map[string]string{"status": "received"})
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
			ID: t.ID, BookingID: t.BookingID, EventID: t.EventID, AmountCents: t.AmountCents,
			Currency: t.Currency, Status: t.Status, RefundStatus: t.RefundStatus,
			CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	sharedhttp.OK(w, resp)
}

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04:05Z07:00")
}

package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	sharedhttp "github.com/nedo/TicketSaas/pkg/http"
	"github.com/nedo/TicketSaas/service/payment/internal/application"
	"github.com/nedo/TicketSaas/service/payment/internal/domain"
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

// InitiateTxnForBooking handles POST /api/payments/internal (ticketing → payment).
func (h *PaymentHandler) InitiateTxnForBooking(w http.ResponseWriter, r *http.Request) {
	var req InternalCreateRequest
	if err := sharedhttp.DecodeJSON(r, &req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}
	if req.BookingID == "" || req.UserID == "" || req.EventID == "" || req.AmountRupiah <= 0 {
		sharedhttp.BadRequest(w, "booking_id, event_id, user_id and amount_rupiah are required")
		return
	}

	txn, err := h.svc.InitiateTxnForBooking(
		r.Context(),
		req.BookingID,
		req.EventID,
		req.UserID,
		req.Email,
		req.AmountRupiah,
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

// ProcessPayment handles POST /api/payments/booking/{booking_id}.
func (h *PaymentHandler) ProcessPayment(w http.ResponseWriter, r *http.Request) {
	bookingID := chi.URLParam(r, "booking_id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	if userID == "" {
		sharedhttp.Unauthorized(w, "authentication required")
		return
	}

	txn, result, err := h.svc.ProcessPayment(r.Context(), bookingID, userID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrTransactionNotFound):
			sharedhttp.NotFound(w, "transaction not found")
		case errors.Is(err, domain.ErrAlreadyProcessed):
			sharedhttp.Error(w, http.StatusConflict, "payment already initiated")
		case errors.Is(err, domain.ErrTransactionExpired):
			sharedhttp.Error(w, http.StatusConflict, "booking has expired")
		case errors.Is(err, domain.ErrGatewayUnavailable):
			fmt.Println(err)
			sharedhttp.Error(
				w,
				http.StatusBadGateway,
				"payment provider unavailable, please try again",
			)
		default:
			fmt.Println(err)
			sharedhttp.InternalServerError(w, "failed to initiate payment")
		}
		return
	}

	sharedhttp.OK(w, ProcessPaymentResponse{
		TransactionID:    txn.ID,
		PaymentSessionID: result.ProviderRef,
		PaymentLinkURL:   result.PaymentLinkURL,
		AmountRupiah:     txn.AmountRupiah,
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
		AmountRupiah:   txn.AmountRupiah,
		Currency:       txn.Currency,
	})
}

// MockSessionWebhook handles POST /api/payments/booking/mock/{booking_id}/{status}.
// Dev-only: simulates the gateway webhook for a booking without waiting for the
// real provider. status must be "success" or "expired". Blocked externally at
// the gateway (traefik), reachable only on the service port directly.
func (h *PaymentHandler) MockSessionWebhook(w http.ResponseWriter, r *http.Request) {
	bookingID := chi.URLParam(r, "booking_id")
	status := chi.URLParam(r, "status")

	var event, sessionStatus string
	switch status {
	case "success":
		event = "payment_session.completed"
		sessionStatus = "COMPLETED"
	case "expired":
		event = "payment_session.expired"
		sessionStatus = "EXPIRED"
	default:
		sharedhttp.BadRequest(w, "status must be 'success' or 'expired'")
		return
	}

	txn, err := h.svc.GetPaymentStatus(r.Context(), bookingID)
	if err != nil {
		sharedhttp.NotFound(w, "transaction not found")
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"event": event,
		"data": map[string]string{
			"reference_id":       bookingID,
			"payment_session_id": txn.ProviderRef,
			"status":             sessionStatus,
		},
	})
	if err := h.svc.HandleSessionWebhook(r.Context(), payload); err != nil {
		if errors.Is(err, domain.ErrNoRows) {
			sharedhttp.NotFound(w, "transaction not found")
			return
		}
		sharedhttp.BadRequest(w, "mock webhook failed")
		return
	}

	sharedhttp.OK(w, map[string]string{"status": "received"})
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
		ID: txn.ID, BookingID: txn.BookingID, EventID: txn.EventID, AmountRupiah: txn.AmountRupiah,
		Currency: txn.Currency, Status: txn.Status, RefundStatus: txn.RefundStatus,
		CreatedAt: txn.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// GetTransaction handles GET /payments/{txn_id}. Returns the full transaction
// details (including the stored payment link) for the payment page. Only the
// transaction owner may read it.
func (h *PaymentHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	txnID := chi.URLParam(r, "txn_id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	txn, err := h.svc.GetTransaction(r.Context(), txnID)
	if err != nil || txn.UserID != userID {
		sharedhttp.NotFound(w, "transaction not found")
		return
	}
	sharedhttp.OK(w, TransactionResponse{
		ID: txn.ID, BookingID: txn.BookingID, EventID: txn.EventID, AmountRupiah: txn.AmountRupiah,
		Currency: txn.Currency, Status: txn.Status, RefundStatus: txn.RefundStatus,
		PaymentLinkURL: txn.PaymentLinkURL,
		ExpiresAt:      formatTime(txn.ExpiresAt),
		CreatedAt:      txn.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// Webhook handles POST /api/payments/webhook (called by payment providers).
// The delivery is verified via the shared x-callback-token and applied to the
// transaction directly; when the transaction does not exist a non-2xx is
// returned so the gateway retries.
func (h *PaymentHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare(
		[]byte(r.Header.Get("x-callback-token")),
		[]byte(h.callbackToken),
	) != 1 {
		sharedhttp.Unauthorized(w, "invalid callback token")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		sharedhttp.BadRequest(w, "failed to read body")
		return
	}
	if err := h.svc.HandleSessionWebhook(r.Context(), body); err != nil {
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
			ID: t.ID, BookingID: t.BookingID, EventID: t.EventID, AmountRupiah: t.AmountRupiah,
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

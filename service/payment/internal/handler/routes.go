package handler

import (
	"github.com/go-chi/chi/v5"
	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	sharedhttp "github.com/unedtamps/ticketbus/pkg/http"
)

// Routes returns the payment service HTTP routes.
func (h *PaymentHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Provider webhook (public — providers authenticate via x-callback-token)
	r.Post("/api/payments/webhook", h.Webhook)
	// Provider payout webhook (same shared x-callback-token; covered by the
	// gateway's /api/payments/webhook route prefix)
	r.Post("/api/payments/webhook/payout", h.PayoutWebhook)

	// Dev-only mock webhook simulator (blocked at the gateway — see traefik/dynamic.yml)
	r.Post("/api/payments/booking/mock/{booking_id}/{status}", h.MockSessionWebhook)
	r.Post("/api/payments/payout/mock/{event_id}/{status}", h.MockPayoutWebhook)

	// Internal service-to-service routes (protected by X-Internal-Key)
	r.With(h.requireInternalKey).Post("/api/payments/internal", h.InitiateTxnForBooking)

	// Customer routes
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Post("/api/payments/booking/{booking_id}", h.ProcessPayment)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Get("/api/payments/booking/{booking_id}", h.GetPaymentStatus)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Get("/api/payments/{id}/status", h.GetStatus)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Get("/api/payments/{txn_id}", h.GetTransaction)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Get("/api/payments", h.ListTransactions)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Get("/api/refund-account", h.GetRefundAccount)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Put("/api/refund-account", h.SaveRefundAccount)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Get("/api/refunds", h.ListMyRefunds)
	})

	// Admin routes
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.With(sharedhttp.RequireRole(sdomain.RoleAdmin)).
			Get("/api/admin/refunds", h.ListRefunds)
		r.With(sharedhttp.RequireRole(sdomain.RoleAdmin)).
			Post("/api/admin/refunds/{id}/retry", h.RetryRefund)
	})

	return r
}

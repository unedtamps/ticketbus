package handler

import (
	"github.com/go-chi/chi/v5"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// Routes returns the payment service HTTP routes.
func (h *PaymentHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Provider webhook (public — providers authenticate via x-callback-token)
	r.Post("/api/payments/webhook", h.Webhook)

	// Dev-only mock webhook simulator (blocked at the gateway — see traefik/dynamic.yml)
	r.Post("/api/payments/booking/mock/{booking_id}/{status}", h.MockSessionWebhook)

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
	})

	return r
}

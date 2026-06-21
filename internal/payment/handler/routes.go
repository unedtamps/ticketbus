package handler

import (
	"github.com/go-chi/chi/v5"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// Routes returns the payment service HTTP routes.
func (h *PaymentHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Provider webhook (public — providers authenticate via signature, not user JWT)
	r.Post("/api/payments/webhook/{provider}", h.Webhook)

	// Customer routes
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Post("/api/payments/by-booking/{booking_id}/checkout", h.CheckoutByBooking)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).Get("/api/payments/{id}/status", h.GetStatus)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).Get("/api/payments", h.ListTransactions)
	})

	return r
}

package handler

import (
	"github.com/go-chi/chi/v5"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// Routes returns the inventory service HTTP routes.
func (h *InventoryHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Customer routes (require auth)
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).Post("/api/inventory/reserve", h.Reserve)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).Delete("/api/inventory/reserve/{id}", h.Release)
	})

	// Bookings
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).Get("/api/bookings", h.ListMyBookings)
		r.Get("/api/bookings/{id}", h.GetBooking)
	})

	return r
}

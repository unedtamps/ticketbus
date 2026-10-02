package handler

import (
	"github.com/go-chi/chi/v5"
	sdomain "github.com/nedo/TicketSaas/pkg/dto"
	sharedhttp "github.com/nedo/TicketSaas/pkg/http"
)

// Routes registers the booking HTTP routes onto the provided router.
func (h *BookingHandler) Routes(r chi.Router) chi.Router {

	// Customer routes (require auth)
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Post("/api/bookings/reserve", h.Reserve)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).
			Delete("/api/bookings/reserve/{id}", h.Release)
	})

	// Bookings
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.With(sharedhttp.RequireRole(sdomain.RoleCustomer)).Get("/api/bookings", h.ListMyBookings)
		r.Get("/api/bookings/{id}", h.GetBooking)
	})

	return r
}

package handler

import (
	"github.com/go-chi/chi/v5"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// Routes returns the booking HTTP routes.
func (h *BookingHandler) Routes() chi.Router {
	r := chi.NewRouter()

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

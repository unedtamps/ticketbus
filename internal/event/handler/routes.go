package handler

import (
	"github.com/go-chi/chi/v5"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// Routes returns the event service HTTP routes.
func (h *EventHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Public routes
	r.Get("/api/events", h.ListPublished)
	r.Get("/api/events/{id}", h.GetEvent)

	// EO routes
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.With(sharedhttp.RequireRole(sdomain.RoleEO)).Post("/api/events", h.CreateEvent)
		r.With(sharedhttp.RequireRole(sdomain.RoleEO)).
			Put("/api/events/{id}", h.UpdateEvent)
		r.With(sharedhttp.RequireRole(sdomain.RoleEO)).
			Post("/api/events/{id}/cancel", h.CancelEvent)
		r.With(sharedhttp.RequireRole(sdomain.RoleEO)).
			Get("/api/events/organizers/me", h.GetOrganizer)
		r.With(sharedhttp.RequireRole(sdomain.RoleEO)).
			Get("/api/events/mine", h.ListMyEvents)
	})

	// Admin routes
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.With(sharedhttp.RequireRole(sdomain.RoleAdmin)).Get("/api/events/pending", h.ListPending)
		r.With(sharedhttp.RequireRole(sdomain.RoleAdmin)).Post("/api/events/{id}/approve", h.ApproveEvent)
		r.With(sharedhttp.RequireRole(sdomain.RoleAdmin)).Post("/api/events/{id}/reject", h.RejectEvent)
	})

	return r
}

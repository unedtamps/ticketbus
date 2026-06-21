package handler

import (
	"github.com/go-chi/chi/v5"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// Routes returns the auth service HTTP routes.
func (h *AuthHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Public routes
	r.Post("/api/auth/register", h.Register)
	r.Post("/api/auth/register/organizer", h.RegisterOrganizer)
	r.Post("/api/auth/login", h.Login)
	r.Post("/api/auth/refresh", h.Refresh)

	// ForwardAuth endpoint for Traefik
	r.Get("/api/auth/verify", h.Verify)

	// Protected routes (JWT via Kong header OR direct Bearer token)
	r.Group(func(r chi.Router) {
		r.Use(sharedhttp.WithUserContext)
		r.Use(h.BearerAuth)
		r.Get("/api/auth/me", h.Me)
	})

	return r
}

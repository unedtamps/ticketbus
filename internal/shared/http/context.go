package http

import (
	"context"
	"net/http"

	"github.com/nedo/TicketSaas/internal/shared/domain"
)

// Context keys.
type contextKey string

const (
	ContextKeyUserID    contextKey = "user_id"
	ContextKeyUserRole  contextKey = "user_role"
	ContextKeyUserEmail contextKey = "user_email"
)

// WithUserContext injects user info from request headers (set by Kong) into the request context.
func WithUserContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if userID := r.Header.Get("X-Authenticated-User-ID"); userID != "" {
			ctx = context.WithValue(ctx, ContextKeyUserID, userID)
		}
		if role := r.Header.Get("X-Authenticated-User-Role"); role != "" {
			ctx = context.WithValue(ctx, ContextKeyUserRole, domain.Role(role))
		}
		if email := r.Header.Get("X-Authenticated-User-Email"); email != "" {
			ctx = context.WithValue(ctx, ContextKeyUserEmail, email)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserIDFromContext extracts user ID from context.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyUserID).(string); ok {
		return v
	}
	return ""
}

// UserRoleFromContext extracts user role from context.
func UserRoleFromContext(ctx context.Context) domain.Role {
	if v, ok := ctx.Value(ContextKeyUserRole).(domain.Role); ok {
		return v
	}
	return ""
}

// UserEmailFromContext extracts user email from context.
func UserEmailFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyUserEmail).(string); ok {
		return v
	}
	return ""
}

// RequireRole middleware restricts access to specific roles.
func RequireRole(roles ...domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userRole := UserRoleFromContext(r.Context())
			for _, allowed := range roles {
				if userRole == allowed {
					next.ServeHTTP(w, r)
					return
				}
			}
			Forbidden(w, "insufficient permissions")
		})
	}
}

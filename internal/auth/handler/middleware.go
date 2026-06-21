package handler

import (
	"context"
	"net/http"
	"strings"

	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// BearerAuth middleware verifies the Authorization Bearer token
// and injects user info into the request context.
// Skips verification if Kong already injected user headers (X-Authenticated-User-ID present).
func (h *AuthHandler) BearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If Kong already set user context, skip Bearer verification.
		if sharedhttp.UserIDFromContext(r.Context()) != "" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			sharedhttp.Unauthorized(w, "missing or invalid authorization header")
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := h.svc.VerifyToken(tokenString)
		if err != nil {
			sharedhttp.Unauthorized(w, "invalid token")
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, sharedhttp.ContextKeyUserID, claims.UserID)
		ctx = context.WithValue(ctx, sharedhttp.ContextKeyUserEmail, claims.Email)
		ctx = context.WithValue(ctx, sharedhttp.ContextKeyUserRole, claims.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

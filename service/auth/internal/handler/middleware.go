package handler

import (
	"context"
	"net/http"
	"strings"

	sharedhttp "github.com/unedtamps/ticketbus/pkg/http"
)

// BearerAuth middleware verifies the Authorization Bearer token
// and injects user info into the request context.
//
// There is no header-based skip: the auth service sits behind a router that
// has no forward-auth (it issues the tokens), so client-supplied
// X-Authenticated-* headers reach this handler verbatim. Trusting them would
// let anyone impersonate any user on /api/auth/me without a token.
func (h *AuthHandler) BearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

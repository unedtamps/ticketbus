package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/nedo/TicketSaas/internal/auth/application"
	adomain "github.com/nedo/TicketSaas/internal/auth/domain"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// AuthHandler handles HTTP requests for the auth service.
type AuthHandler struct {
	svc      *application.AuthService
	validate *validator.Validate
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(svc *application.AuthService) *AuthHandler {
	return &AuthHandler{
		svc:      svc,
		validate: validator.New(),
	}
}

func userToResponse(user *adomain.User) *UserResponse {
	return &UserResponse{
		ID:    user.ID,
		Email: user.Email,
		Name:  user.Name,
		Role:  user.Role,
	}
}

// Register handles POST /register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}

	if err := h.validate.Struct(req); err != nil {
		sharedhttp.BadRequest(w, "validation failed: "+err.Error())
		return
	}

	if req.Role == "" {
		req.Role = sdomain.RoleCustomer
	}

	user, err := h.svc.Register(r.Context(), req.Email, req.Password, req.Name, req.Role)
	if err != nil {
		if errors.Is(err, adomain.ErrUserExists) {
			sharedhttp.Error(w, http.StatusConflict, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "registration failed")
		return
	}

	sharedhttp.Created(w, map[string]interface{}{
		"user": userToResponse(user),
	})
}

// RegisterOrganizer handles POST /register/organizer.
func (h *AuthHandler) RegisterOrganizer(w http.ResponseWriter, r *http.Request) {
	var req RegisterOrganizerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		sharedhttp.BadRequest(w, "validation failed: "+err.Error())
		return
	}

	user, err := h.svc.RegisterOrganizer(r.Context(), req.Email, req.Password, req.Name, req.OrganizerName, req.Description, req.ProfileLink, req.ContactEmail)
	if err != nil {
		if errors.Is(err, adomain.ErrUserExists) {
			sharedhttp.Error(w, http.StatusConflict, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "registration failed")
		return
	}

	sharedhttp.Created(w, map[string]interface{}{
		"user": userToResponse(user),
	})
}

// Login handles POST /login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}

	if err := h.validate.Struct(req); err != nil {
		sharedhttp.BadRequest(w, "validation failed: "+err.Error())
		return
	}

	pair, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, adomain.ErrInvalidCredentials) {
			sharedhttp.Unauthorized(w, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "login failed")
		return
	}

	// Return tokens. User info fetched via GET /me separately.
	sharedhttp.OK(w, LoginResponse{
		RefreshToken: pair.RefreshToken,
		AuthResponse: AuthResponse{
			AccessToken: pair.AccessToken,
			ExpiresIn:   pair.ExpiresIn,
		},
	})
}

// Refresh handles POST /refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}

	if err := h.validate.Struct(req); err != nil {
		sharedhttp.BadRequest(w, "validation failed: "+err.Error())
		return
	}

	pair, err := h.svc.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, adomain.ErrTokenNotFound) || errors.Is(err, adomain.ErrTokenExpired) {
			sharedhttp.Unauthorized(w, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "token refresh failed")
		return
	}

	sharedhttp.OK(w, LoginResponse{
		AuthResponse: AuthResponse{
			AccessToken: pair.AccessToken,
			ExpiresIn:   pair.ExpiresIn,
		},
		RefreshToken: pair.RefreshToken,
	})
}

// Me handles GET /me.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := sharedhttp.UserIDFromContext(r.Context())
	if userID == "" {
		sharedhttp.Unauthorized(w, "missing authentication")
		return
	}

	user, err := h.svc.GetMe(r.Context(), userID)
	if err != nil {
		sharedhttp.NotFound(w, "user not found")
		return
	}

	sharedhttp.OK(w, AuthResponse{
		User: userToResponse(user),
	})
}

// Verify handles the ForwardAuth request from Traefik.
func (h *AuthHandler) Verify(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		sharedhttp.Unauthorized(w, "missing Authorization header")
		return
	}

	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		sharedhttp.Unauthorized(w, "invalid Authorization header")
		return
	}

	claims, err := h.svc.VerifyToken(token)
	if err != nil {
		sharedhttp.Unauthorized(w, "invalid token")
		return
	}

	w.Header().Set("X-Authenticated-User-ID", claims.UserID)
	w.Header().Set("X-Authenticated-User-Role", string(claims.Role))
	w.Header().Set("X-Authenticated-User-Email", claims.Email)
	w.WriteHeader(http.StatusOK)
}

package handler

import (
	"github.com/nedo/TicketSaas/internal/shared/domain"
)

// RegisterRequest is the DTO for user registration.
type RegisterRequest struct {
	Email    string      `json:"email" validate:"required,email"`
	Password string      `json:"password" validate:"required,min=8"`
	Name     string      `json:"name" validate:"required"`
	Role     domain.Role `json:"role,omitempty" validate:"oneof=customer eo"`
}

// RegisterOrganizerRequest is the DTO for EO registration with organizer profile.
type RegisterOrganizerRequest struct {
	Email          string `json:"email" validate:"required,email"`
	Password       string `json:"password" validate:"required,min=8"`
	Name           string `json:"name" validate:"required"`
	OrganizerName  string `json:"organizer_name" validate:"required"`
	Description    string `json:"description"`
	ProfileLink    string `json:"profile_link"`
	ContactEmail   string `json:"contact_email" validate:"required,email"`
}

// LoginRequest is the DTO for user login.
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// RefreshRequest is the DTO for token refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// AuthResponse is the DTO for auth responses.
type AuthResponse struct {
	User        *UserResponse `json:"user,omitempty"`
	AccessToken string        `json:"access_token,omitempty"`
	ExpiresIn   int64          `json:"expires_in,omitempty"`
}

// UserResponse is the public-facing user data.
type UserResponse struct {
	ID    string      `json:"id"`
	Email string      `json:"email"`
	Name  string      `json:"name"`
	Role  domain.Role `json:"role"`
}

// LoginResponse includes refresh token (only returned on login / refresh).
type LoginResponse struct {
	AuthResponse
	RefreshToken string `json:"refresh_token"`
}

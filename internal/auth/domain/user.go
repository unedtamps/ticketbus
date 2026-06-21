package domain

import (
	"time"

	"github.com/nedo/TicketSaas/internal/shared/domain"
)

// User is the aggregate root for authentication.
type User struct {
	ID           string      `json:"id"`
	Email        string      `json:"email"`
	PasswordHash string      `json:"-"`
	Name         string      `json:"name"`
	Role         domain.Role `json:"role"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

package domain

import (
	"time"

	"github.com/nedo/TicketSaas/pkg/dto"
)

// User is the aggregate root for authentication.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Name         string    `json:"name"`
	Role         dto.Role  `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

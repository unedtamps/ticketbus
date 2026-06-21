package domain

import (
	"time"

	"github.com/nedo/TicketSaas/internal/shared/domain"
)

// Organizer represents an event organizer account (linked to an EO user).
type Organizer struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	ProfileLink  string    `json:"profile_link"`
	ContactEmail string    `json:"contact_email"`
	CreatedAt    time.Time `json:"created_at"`
}

// Event is the aggregate root for events.
type Event struct {
	ID            string             `json:"id"`
	OrganizerID   string             `json:"organizer_id"`
	Title         string             `json:"title"`
	Description   string             `json:"description"`
	VenueName     string             `json:"venue_name"`
	VenueAddress  string             `json:"venue_address"`
	VenueCapacity int                `json:"venue_capacity"`
	StartAt       time.Time          `json:"start_at"`
	EndAt         time.Time          `json:"end_at"`
	Status        domain.EventStatus `json:"status"`
	ReviewedBy    *string            `json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time         `json:"reviewed_at,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

// TicketType represents a ticket category for an event.
type TicketType struct {
	ID           string `json:"id"`
	EventID      string `json:"event_id"`
	Name         string `json:"name"`
	PriceCents   int    `json:"price_cents"`
	Quantity     int    `json:"quantity"`
	Available    int    `json:"available"`
	MaxPerOrder  int    `json:"max_per_order"`
}

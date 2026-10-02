package domain

import (
	"time"

	"github.com/nedo/TicketSaas/pkg/dto"
)

// Event is the aggregate root for events.
type Event struct {
	ID            string          `json:"id"`
	OrganizerID   string          `json:"organizer_id"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	VenueName     string          `json:"venue_name"`
	VenueAddress  string          `json:"venue_address"`
	VenueCapacity int             `json:"venue_capacity"`
	StartAt       time.Time       `json:"start_at"`
	EndAt         time.Time       `json:"end_at"`
	Status        dto.EventStatus `json:"status"`
	ReviewedBy    *string         `json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time      `json:"reviewed_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// TicketType represents a ticket category for an event.
type TicketType struct {
	ID          string `json:"id"`
	EventID     string `json:"event_id"`
	Name        string `json:"name"`
	PriceRupiah int    `json:"price_rupiah"`
	Quantity    int    `json:"quantity"`
	Available   int    `json:"available"`
	MaxPerOrder int    `json:"max_per_order"`
}

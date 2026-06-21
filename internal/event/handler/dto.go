package handler

import (
	"time"

	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// CreateEventRequest is the DTO for creating an event.
type CreateEventRequest struct {
	Title         string              `json:"title" validate:"required"`
	Description   string              `json:"description"`
	VenueName     string              `json:"venue_name" validate:"required"`
	VenueAddress  string              `json:"venue_address" validate:"required"`
	VenueCapacity int                 `json:"venue_capacity" validate:"required,min=1"`
	StartAt       time.Time           `json:"start_at" validate:"required"`
	EndAt         time.Time           `json:"end_at" validate:"required"`
	TicketTypes   []TicketTypeRequest `json:"ticket_types" validate:"required,min=1,dive"`
}

// TicketTypeRequest is the DTO for a ticket type in event creation.
type TicketTypeRequest struct {
	Name        string `json:"name" validate:"required"`
	PriceCents  int    `json:"price_cents" validate:"required,min=0"`
	Quantity    int    `json:"quantity" validate:"required,min=1"`
	MaxPerOrder int    `json:"max_per_order" validate:"omitempty,min=1"`
}

// UpdateEventRequest is the DTO for updating an event.
type UpdateEventRequest struct {
	Title       string    `json:"title" validate:"required"`
	Description string    `json:"description"`
	StartAt     time.Time `json:"start_at" validate:"required"`
	EndAt       time.Time `json:"end_at" validate:"required"`
}

// RejectRequest is the DTO for rejecting an event.
type RejectRequest struct {
	Reason string `json:"reason" validate:"required"`
}

// OrganizerResponse is the public organizer data.
type OrganizerResponse struct {
	ID           string `json:"id"`
	UserID       string `json:"user_id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	ProfileLink  string `json:"profile_link"`
	ContactEmail string `json:"contact_email"`
}

// EventResponse is the public event data.
type EventResponse struct {
	ID            string             `json:"id"`
	OrganizerID   string             `json:"organizer_id"`
	Title         string             `json:"title"`
	Description   string             `json:"description"`
	VenueName     string             `json:"venue_name"`
	VenueAddress  string             `json:"venue_address"`
	VenueCapacity int                `json:"venue_capacity"`
	StartAt       time.Time          `json:"start_at"`
	EndAt         time.Time          `json:"end_at"`
	Status        sdomain.EventStatus `json:"status"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

// EventDetailResponse includes event with ticket types.
type EventDetailResponse struct {
	Event       EventResponse        `json:"event"`
	TicketTypes []TicketTypeResponse `json:"ticket_types"`
}

// TicketTypeResponse is the public ticket type data.
type TicketTypeResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PriceCents  int    `json:"price_cents"`
	Quantity    int    `json:"quantity"`
	Available   int    `json:"available"`
	MaxPerOrder int    `json:"max_per_order"`
}

// EventListResponse is the paginated event list response.
type EventListResponse struct {
	Events []EventResponse `json:"events"`
	Total  int             `json:"total"`
}

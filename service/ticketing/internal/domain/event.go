package domain

import (
	"time"

	"github.com/unedtamps/ticketbus/pkg/dto"
)

// Event is the aggregate root for events.
type Event struct {
	ID            string          `json:"id"`
	OrganizerID   string          `json:"organizer_id"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	ImageKey      string          `json:"image_key,omitempty"`
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

	// ReservationsOpen reports whether the event currently accepts
	// reservations: published, already started, and not over.
	ReservationsOpen bool `json:"reservations_open"`

	// Payout view fields, populated only by organizer-scoped queries
	// (GET /api/events/mine, GET /api/events/{id}/payout).
	PayoutStatus       dto.PayoutStatus `json:"payout_status,omitempty"`
	PayoutAmountRupiah int64            `json:"payout_amount_rupiah,omitempty"`
	PayoutRequestedAt  *time.Time       `json:"payout_requested_at,omitempty"`
	PayoutCompletedAt  *time.Time       `json:"payout_completed_at,omitempty"`

	// ConfirmedRevenueRupiah is the sum of confirmed bookings for the event —
	// what it has earned so far. Combined with PayoutAmountRupiah it yields
	// what is still collectable. Organizer-scoped queries only.
	ConfirmedRevenueRupiah int64 `json:"confirmed_revenue_rupiah,omitempty"`
}

// OrganizerSummary aggregates an organizer's events for the dashboard header.
// It is computed over every event, not one page, so paging the list cannot
// change the figures.
type OrganizerSummary struct {
	// AvailableToCollect sums max(confirmed - paidOut, 0) per event, matching
	// the per-row "Collect" amount — a failed payout stays collectable.
	AvailableToCollect int64 `json:"available_to_collect"`
	ConfirmedTotal     int64 `json:"confirmed_total"`
	PaidOutTotal       int64 `json:"paid_out_total"`
	// CollectableEvents counts events with anything left to collect.
	CollectableEvents int `json:"collectable_events"`
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

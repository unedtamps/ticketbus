package handler

import (
	"time"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
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
	PriceRupiah int    `json:"price_rupiah" validate:"required,min=0"`
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

// EventResponse is the public event data.
type EventResponse struct {
	ID            string              `json:"id"`
	OrganizerID   string              `json:"organizer_id"`
	Title         string              `json:"title"`
	Description   string              `json:"description"`
	ImageURL      string              `json:"image_url,omitempty"`
	VenueName     string              `json:"venue_name"`
	VenueAddress  string              `json:"venue_address"`
	VenueCapacity int                 `json:"venue_capacity"`
	StartAt       time.Time           `json:"start_at"`
	EndAt         time.Time           `json:"end_at"`
	Status        sdomain.EventStatus `json:"status"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	// ReservationsOpen reports whether the event currently accepts
	// reservations. The server enforces the same rule; this only tells the UI
	// what to show.
	ReservationsOpen bool `json:"reservations_open"`
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
	PriceRupiah int    `json:"price_rupiah"`
	Quantity    int    `json:"quantity"`
	Available   int    `json:"available"`
	MaxPerOrder int    `json:"max_per_order"`
}

// EventListResponse is the paginated event list response.
type EventListResponse struct {
	Events []EventResponse `json:"events"`
	Total  int             `json:"total"`
}

// OrganizerEventResponse is the organizer-scoped event view. It embeds the
// public fields and adds payout state; bank details are never returned here.
type OrganizerEventResponse struct {
	EventResponse
	PayoutStatus       string     `json:"payout_status"`
	PayoutAmountRupiah int64      `json:"payout_amount_rupiah"`
	PayoutRequestedAt  *time.Time `json:"payout_requested_at,omitempty"`
	PayoutCompletedAt  *time.Time `json:"payout_completed_at,omitempty"`
	// ConfirmedRevenueRupiah is the event's confirmed booking revenue. The
	// still-collectable amount is this minus PayoutAmountRupiah; the client
	// derives it so both figures stay visible.
	ConfirmedRevenueRupiah int64 `json:"confirmed_revenue_rupiah"`
}

// OrganizerEventListResponse is one page of an organizer's events plus the
// aggregate figures for the whole account. The summary is not page-scoped, so
// the dashboard header stays correct while the list pages.
type OrganizerEventListResponse struct {
	Events []OrganizerEventResponse `json:"events"`
	// NextCursor is opaque. Empty means this is the last page.
	NextCursor string                   `json:"next_cursor"`
	Summary    OrganizerSummaryResponse `json:"summary"`
}

// OrganizerSummaryResponse aggregates an organizer's whole event set.
type OrganizerSummaryResponse struct {
	AvailableToCollect int64 `json:"available_to_collect"`
	ConfirmedTotal     int64 `json:"confirmed_total"`
	PaidOutTotal       int64 `json:"paid_out_total"`
	CollectableEvents  int   `json:"collectable_events"`
}

// PayoutAccountRequest is the EO payout destination.
type PayoutAccountRequest struct {
	BankCode              string `json:"bank_code" validate:"required"`
	BankAccountNumber     string `json:"bank_account_number" validate:"required"`
	BankAccountHolderName string `json:"bank_account_holder_name" validate:"required"`
}

// PayoutAccountResponse is the saved payout destination.
type PayoutAccountResponse struct {
	BankCode              string    `json:"bank_code"`
	BankAccountNumber     string    `json:"bank_account_number"`
	BankAccountHolderName string    `json:"bank_account_holder_name"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// PayoutResponse is the aggregated payout state for an event.
type PayoutResponse struct {
	EventID       string                  `json:"event_id"`
	Status        string                  `json:"status"`
	AmountRupiah  int64                   `json:"amount_rupiah"`
	Attempts      int                     `json:"attempts"`
	AttemptList   []PayoutAttemptResponse `json:"attempts_list"`
	ProviderRef   string                  `json:"provider_ref,omitempty"`
	FailureReason string                  `json:"failure_reason,omitempty"`
	RequestedAt   *time.Time              `json:"requested_at,omitempty"`
	CompletedAt   *time.Time              `json:"completed_at,omitempty"`
}

// PayoutAttemptResponse is one payout attempt of an event.
type PayoutAttemptResponse struct {
	EventID               string     `json:"event_id"`
	Attempt               int        `json:"attempt"`
	Status                string     `json:"status"`
	AmountRupiah          int64      `json:"amount_rupiah"`
	BankCode              string     `json:"bank_code,omitempty"`
	BankAccountNumber     string     `json:"bank_account_number,omitempty"`
	BankAccountHolderName string     `json:"bank_account_holder_name,omitempty"`
	ProviderRef           string     `json:"provider_ref,omitempty"`
	FailureReason         string     `json:"failure_reason,omitempty"`
	RequestedAt           time.Time  `json:"requested_at"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
}

package handler

import (
	"github.com/nedo/TicketSaas/internal/inventory/domain"
)

// ReserveRequest is the DTO for creating a reservation.
type ReserveRequest struct {
	EventID string               `json:"event_id" validate:"required"`
	Items   []ReserveItemRequest `json:"items" validate:"required,min=1,dive"`
}

// ReserveItemRequest is a single item in a reservation request.
type ReserveItemRequest struct {
	TicketTypeID   string `json:"ticket_type_id" validate:"required"`
	Quantity       int    `json:"quantity" validate:"required,min=1"`
	UnitPriceCents int    `json:"unit_price_cents" validate:"required,min=0"`
}

// ReservationResponse is the public reservation data.
type ReservationResponse struct {
	BookingID  string `json:"booking_id"`
	EventID    string `json:"event_id"`
	TotalCents int    `json:"total_cents"`
	Status     string `json:"status"`
	ExpiresAt  string `json:"expires_at"`
}

// BookingResponse is the public booking data.
type BookingResponse struct {
	ID         string              `json:"id"`
	UserID     string              `json:"user_id"`
	EventID    string              `json:"event_id"`
	Status     string              `json:"status"`
	TotalCents int                 `json:"total_cents"`
	PaymentID  string              `json:"payment_id"`
	Items      []BookingItemResp   `json:"items"`
	CreatedAt  string              `json:"created_at"`
}

// BookingItemResp is a public booking item.
type BookingItemResp struct {
	ID             string `json:"id"`
	TicketTypeID   string `json:"ticket_type_id"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int    `json:"unit_price_cents"`
}

func bookingToResponse(b *domain.Booking) BookingResponse {
	items := make([]BookingItemResp, len(b.Items))
	for i, item := range b.Items {
		items[i] = BookingItemResp{
			ID: item.ID, TicketTypeID: item.TicketTypeID,
			Quantity: item.Quantity, UnitPriceCents: item.UnitPriceCents,
		}
	}
	return BookingResponse{
		ID: b.ID, UserID: b.UserID, EventID: b.EventID,
		Status: b.Status, TotalCents: b.TotalCents, PaymentID: b.PaymentID,
		Items: items, CreatedAt: b.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

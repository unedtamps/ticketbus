package handler

import (
	sdto "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
)

// ReserveRequest is the DTO for creating a reservation.
type ReserveRequest struct {
	EventID string               `json:"event_id" validate:"required"`
	Items   []ReserveItemRequest `json:"items" validate:"required,min=1,dive"`
}

// ReserveItemRequest is a single item in a reservation request.
type ReserveItemRequest struct {
	TicketTypeID    string `json:"ticket_type_id" validate:"required"`
	Quantity        int    `json:"quantity" validate:"required,min=1"`
	UnitPriceRupiah int    `json:"unit_price_rupiah" validate:"required,min=0"`
}

// ReservationResponse is the public reservation data.
type ReservationResponse struct {
	BookingID   string `json:"booking_id"`
	EventID     string `json:"event_id"`
	TotalRupiah int    `json:"total_rupiah"`
	Status      string `json:"status"`
	ExpiresAt   string `json:"expires_at"`
}

// BookingResponse is the public booking data.
type BookingResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	EventID   string `json:"event_id"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at,omitempty"`
	// PaymentDeadline is the last moment a payment can be started for this
	// booking: expires_at minus the gateway's minimum session lifetime. The
	// payment service enforces the same boundary, so the customer's countdown
	// and the point the button stops working stay in step.
	PaymentDeadline string            `json:"payment_deadline,omitempty"`
	TotalRupiah     int               `json:"total_rupiah"`
	PaymentID       string            `json:"payment_id"`
	Items           []BookingItemResp `json:"items"`
	CreatedAt       string            `json:"created_at"`
	// Event context, present on customer-facing lists so the buyer's ticket
	// table can name what was bought.
	EventTitle   string `json:"event_title,omitempty"`
	EventStartAt string `json:"event_start_at,omitempty"`
	EventVenue   string `json:"event_venue,omitempty"`
}

// BookingItemResp is a public booking item.
type BookingItemResp struct {
	ID               string `json:"id"`
	TicketTypeID     string `json:"ticket_type_id"`
	Quantity         int    `json:"quantity"`
	UnitPriceRupiah  int    `json:"unit_price_rupiah"`
	TotalPriceRupiah int    `json:"total_price_rupiah"`
	TicketTypeName   string `json:"ticket_type_name,omitempty"`
}

func bookingToResponse(b *domain.Booking) BookingResponse {
	items := make([]BookingItemResp, len(b.Items))
	for i, item := range b.Items {
		items[i] = BookingItemResp{
			ID: item.ID, TicketTypeID: item.TicketTypeID,
			Quantity: item.Quantity, UnitPriceRupiah: item.UnitPriceRupiah, TotalPriceRupiah: item.TotalPriceRupiah,
			TicketTypeName: item.TicketTypeName,
		}
	}
	expiresAt := ""
	paymentDeadline := ""
	if b.ExpiresAt != nil {
		expiresAt = b.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
		paymentDeadline = sdto.PaymentDeadline(*b.ExpiresAt).Format("2006-01-02T15:04:05Z07:00")
	}
	// EventStartAt is zero when the join found no event (deleted/cancelled).
	startAt := ""
	if !b.EventStartAt.IsZero() {
		startAt = b.EventStartAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return BookingResponse{
		ID: b.ID, UserID: b.UserID, EventID: b.EventID,
		Status: b.Status, ExpiresAt: expiresAt, PaymentDeadline: paymentDeadline,
		TotalRupiah: b.TotalRupiah, PaymentID: b.PaymentID,
		Items: items, CreatedAt: b.CreatedAt.Format("2006-01-02T15:04:05Z"),
		EventTitle:   b.EventTitle,
		EventStartAt: startAt,
		EventVenue:   b.EventVenue,
	}
}

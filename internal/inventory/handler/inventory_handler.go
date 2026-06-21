package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/nedo/TicketSaas/internal/inventory/application"
	"github.com/nedo/TicketSaas/internal/inventory/domain"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// InventoryHandler handles HTTP requests for the inventory service.
type InventoryHandler struct {
	svc      *application.InventoryService
	validate *validator.Validate
}

// NewInventoryHandler creates a new InventoryHandler.
func NewInventoryHandler(svc *application.InventoryService) *InventoryHandler {
	return &InventoryHandler{svc: svc, validate: validator.New()}
}

// Reserve handles POST /inventory/reserve.
func (h *InventoryHandler) Reserve(w http.ResponseWriter, r *http.Request) {
	var req ReserveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	userID := sharedhttp.UserIDFromContext(r.Context())
	if userID == "" {
		sharedhttp.Unauthorized(w, "authentication required")
		return
	}

	items := make([]domain.BookingItem, len(req.Items))
	for i, item := range req.Items {
		items[i] = domain.BookingItem{
			TicketTypeID:   item.TicketTypeID,
			Quantity:       item.Quantity,
			UnitPriceCents: item.UnitPriceCents,
		}
	}

	res, err := h.svc.Reserve(r.Context(), userID, req.EventID, items)
	if err != nil {
		if errors.Is(err, domain.ErrNoSeatsAvailable) {
			sharedhttp.Error(w, http.StatusConflict, "not enough seats available")
			return
		}
		sharedhttp.InternalServerError(w, "reservation failed")
		return
	}

	sharedhttp.Created(w, ReservationResponse{
		BookingID:  res.BookingID,
		EventID:    res.EventID,
		TotalCents: res.TotalCents,
		Status:     res.Status,
		ExpiresAt:  time.Now().Add(15 * time.Minute).Format(time.RFC3339),
	})
}

// Release handles DELETE /inventory/reserve/:id.
func (h *InventoryHandler) Release(w http.ResponseWriter, r *http.Request) {
	bookingID := chi.URLParam(r, "id")
	if err := h.svc.Release(r.Context(), bookingID); err != nil {
		sharedhttp.NotFound(w, err.Error())
		return
	}
	sharedhttp.NoContent(w)
}

// GetBooking handles GET /bookings/:id.
func (h *InventoryHandler) GetBooking(w http.ResponseWriter, r *http.Request) {
	bookingID := chi.URLParam(r, "id")
	booking, err := h.svc.GetBooking(r.Context(), bookingID)
	if err != nil {
		sharedhttp.NotFound(w, "booking not found")
		return
	}
	sharedhttp.OK(w, bookingToResponse(booking))
}

// ListMyBookings handles GET /bookings.
func (h *InventoryHandler) ListMyBookings(w http.ResponseWriter, r *http.Request) {
	userID := sharedhttp.UserIDFromContext(r.Context())
	if userID == "" {
		sharedhttp.Unauthorized(w, "authentication required")
		return
	}
	bookings, err := h.svc.ListMyBookings(r.Context(), userID)
	if err != nil {
		sharedhttp.InternalServerError(w, "failed to list bookings")
		return
	}
	resp := make([]BookingResponse, 0, len(bookings))
	for _, b := range bookings {
		resp = append(resp, bookingToResponse(&b))
	}
	sharedhttp.OK(w, resp)
}

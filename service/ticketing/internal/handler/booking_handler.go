package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	sharedhttp "github.com/nedo/TicketSaas/pkg/http"
	"github.com/nedo/TicketSaas/service/ticketing/internal/application/booking"
	"github.com/nedo/TicketSaas/service/ticketing/internal/domain"
)

// BookingHandler handles HTTP requests for booking operations.
type BookingHandler struct {
	svc      *booking.BookingService
	validate *validator.Validate
}

// NewBookingHandler creates a new BookingHandler.
func NewBookingHandler(svc *booking.BookingService) *BookingHandler {
	return &BookingHandler{svc: svc, validate: validator.New()}
}

// Reserve handles POST /api/inventory/reserve.
func (h *BookingHandler) Reserve(w http.ResponseWriter, r *http.Request) {
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
	userEmail := sharedhttp.UserEmailFromContext(r.Context())

	items := make([]domain.BookingItem, len(req.Items))
	for i, item := range req.Items {
		items[i] = domain.BookingItem{
			TicketTypeID:   item.TicketTypeID,
			Quantity:       item.Quantity,
			UnitPriceRupiah: item.UnitPriceRupiah,
		}
	}

	res, err := h.svc.Reserve(r.Context(), userID, userEmail, req.EventID, items)
	if err != nil {
		if errors.Is(err, domain.ErrNoSeatsAvailable) {
			sharedhttp.Error(w, http.StatusConflict, "not enough seats available")
			return
		}
		if errors.Is(err, domain.ErrPriceMismatch) {
			sharedhttp.BadRequest(w, err.Error())
			return
		}
		if errors.Is(err, domain.ErrPaymentUnavailable) {
			sharedhttp.Error(w, http.StatusBadGateway, "payment service unavailable, please retry")
			return
		}
		sharedhttp.InternalServerError(w, "reservation failed")
		return
	}

	sharedhttp.Created(w, ReservationResponse{
		BookingID:  res.BookingID,
		EventID:    res.EventID,
		TotalRupiah: res.TotalRupiah,
		Status:     res.Status,
		ExpiresAt:  res.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// Release handles DELETE /api/bookings/reserve/:id. Reservations cannot be
// cancelled by the customer: seats are released automatically when the
// payment expires (or the booking completes).
func (h *BookingHandler) Release(w http.ResponseWriter, r *http.Request) {
	sharedhttp.Error(
		w,
		http.StatusConflict,
		"reservation cannot be cancelled; seats are released automatically when the payment expires",
	)
}

// GetBooking handles GET /bookings/:id.
func (h *BookingHandler) GetBooking(w http.ResponseWriter, r *http.Request) {
	bookingID := chi.URLParam(r, "id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	booking, err := h.svc.GetBooking(r.Context(), bookingID)
	if err != nil || booking.UserID != userID {
		sharedhttp.NotFound(w, "booking not found")
		return
	}
	sharedhttp.OK(w, bookingToResponse(booking))
}

// ListMyBookings handles GET /bookings.
func (h *BookingHandler) ListMyBookings(w http.ResponseWriter, r *http.Request) {
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

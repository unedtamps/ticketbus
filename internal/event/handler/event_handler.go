package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/nedo/TicketSaas/internal/event/application"
	"github.com/nedo/TicketSaas/internal/event/domain"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
)

// EventHandler handles HTTP requests for the event service.
type EventHandler struct {
	svc      *application.EventService
	validate *validator.Validate
}

// NewEventHandler creates a new EventHandler.
func NewEventHandler(svc *application.EventService) *EventHandler {
	return &EventHandler{svc: svc, validate: validator.New()}
}

// GetOrganizer handles GET /organizers/me.
func (h *EventHandler) GetOrganizer(w http.ResponseWriter, r *http.Request) {
	userID := sharedhttp.UserIDFromContext(r.Context())
	org, err := h.svc.GetOrganizer(r.Context(), userID)
	if err != nil {
		sharedhttp.NotFound(w, "organizer not found")
		return
	}
	sharedhttp.OK(w, OrganizerResponse{
		ID: org.ID, UserID: org.UserID, Name: org.Name, Description: org.Description,
		ProfileLink: org.ProfileLink, ContactEmail: org.ContactEmail,
	})
}

// CreateEvent handles POST /events.
func (h *EventHandler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	userID := sharedhttp.UserIDFromContext(r.Context())
	org, err := h.svc.GetOrganizer(r.Context(), userID)
	if err != nil {
		sharedhttp.Forbidden(w, "you must create an organizer profile first")
		return
	}
	ticketTypes := make([]domain.TicketType, len(req.TicketTypes))
	for i, tt := range req.TicketTypes {
		mpo := tt.MaxPerOrder
		if mpo == 0 {
			mpo = 5
		}
		ticketTypes[i] = domain.TicketType{
			Name:        tt.Name,
			PriceCents:  tt.PriceCents,
			Quantity:    tt.Quantity,
			MaxPerOrder: mpo,
		}
	}
	event, err := h.svc.CreateEvent(r.Context(), org.ID, req.Title, req.Description, req.VenueName, req.VenueAddress, req.VenueCapacity, req.StartAt, req.EndAt, ticketTypes)
	if err != nil {
		if errors.Is(err, domain.ErrCapacityExceeded) || errors.Is(err, domain.ErrTicketTypesEmpty) {
			sharedhttp.BadRequest(w, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "failed to create event")
		return
	}
	sharedhttp.Created(w, EventResponse{
		ID: event.ID, OrganizerID: event.OrganizerID,
		Title: event.Title, Description: event.Description,
		VenueName: event.VenueName, VenueAddress: event.VenueAddress, VenueCapacity: event.VenueCapacity,
		StartAt: event.StartAt, EndAt: event.EndAt, Status: event.Status,
		CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
	})
}

// UpdateEvent handles PUT /events/:id.
func (h *EventHandler) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	var req UpdateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	userID := sharedhttp.UserIDFromContext(r.Context())
	org, err := h.svc.GetOrganizer(r.Context(), userID)
	if err != nil {
		sharedhttp.Forbidden(w, "organizer not found")
		return
	}
	event, err := h.svc.UpdateEvent(r.Context(), eventID, org.ID, req.Title, req.Description, req.StartAt, req.EndAt)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		if errors.Is(err, domain.ErrNotEventOwner) || errors.Is(err, domain.ErrEventNotEditable) {
			sharedhttp.Forbidden(w, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "failed to update event")
		return
	}
	sharedhttp.OK(w, EventResponse{
		ID: event.ID, OrganizerID: event.OrganizerID, VenueName: event.VenueName, VenueAddress: event.VenueAddress, VenueCapacity: event.VenueCapacity,
		Title: event.Title, Description: event.Description,
		StartAt: event.StartAt, EndAt: event.EndAt, Status: event.Status,
		CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
	})
}

// ApproveEvent handles POST /events/:id/approve.
func (h *EventHandler) ApproveEvent(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	event, err := h.svc.ApproveEvent(r.Context(), eventID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	sharedhttp.OK(w, EventResponse{
		ID: event.ID, OrganizerID: event.OrganizerID, VenueName: event.VenueName, VenueAddress: event.VenueAddress, VenueCapacity: event.VenueCapacity,
		Title: event.Title, Description: event.Description,
		StartAt: event.StartAt, EndAt: event.EndAt, Status: event.Status,
		CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
	})
}

// RejectEvent handles POST /events/:id/reject.
func (h *EventHandler) RejectEvent(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	var req RejectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	userID := sharedhttp.UserIDFromContext(r.Context())
	event, err := h.svc.RejectEvent(r.Context(), eventID, userID, req.Reason)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	sharedhttp.OK(w, EventResponse{
		ID: event.ID, OrganizerID: event.OrganizerID, VenueName: event.VenueName, VenueAddress: event.VenueAddress, VenueCapacity: event.VenueCapacity,
		Title: event.Title, Description: event.Description,
		StartAt: event.StartAt, EndAt: event.EndAt, Status: event.Status,
		CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
	})
}

// CancelEvent handles POST /events/:id/cancel.
func (h *EventHandler) CancelEvent(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	org, err := h.svc.GetOrganizer(r.Context(), userID)
	if err != nil {
		sharedhttp.BadRequest(w, "organizer not found")
		return
	}
	event, err := h.svc.CancelEvent(r.Context(), eventID, org.ID)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	sharedhttp.OK(w, EventResponse{
		ID: event.ID, OrganizerID: event.OrganizerID, VenueName: event.VenueName, VenueAddress: event.VenueAddress, VenueCapacity: event.VenueCapacity,
		Title: event.Title, Description: event.Description,
		StartAt: event.StartAt, EndAt: event.EndAt, Status: event.Status,
		CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
	})
}

// GetEvent handles GET /events/:id.
func (h *EventHandler) GetEvent(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	event, types, err := h.svc.GetEvent(r.Context(), eventID)
	if err != nil {
		sharedhttp.NotFound(w, err.Error())
		return
	}
	ttResp := make([]TicketTypeResponse, len(types))
	for i, tt := range types {
		ttResp[i] = TicketTypeResponse{ID: tt.ID, Name: tt.Name, PriceCents: tt.PriceCents, Quantity: tt.Quantity, Available: tt.Available, MaxPerOrder: tt.MaxPerOrder}
	}
	sharedhttp.OK(w, EventDetailResponse{
		Event:       EventResponse{ID: event.ID, OrganizerID: event.OrganizerID, VenueName: event.VenueName, VenueAddress: event.VenueAddress, VenueCapacity: event.VenueCapacity, Title: event.Title, Description: event.Description, StartAt: event.StartAt, EndAt: event.EndAt, Status: event.Status, CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt},
		TicketTypes: ttResp,
	})
}

// ListPublished handles GET /events.
func (h *EventHandler) ListPublished(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	events, total, err := h.svc.ListPublished(r.Context(), limit, offset)
	if err != nil {
		sharedhttp.InternalServerError(w, "failed to list events")
		return
	}
	resp := make([]EventResponse, 0, len(events))
	for _, e := range events {
		resp = append(resp, EventResponse{ID: e.ID, OrganizerID: e.OrganizerID, VenueName: e.VenueName, VenueAddress: e.VenueAddress, VenueCapacity: e.VenueCapacity, Title: e.Title, Description: e.Description, StartAt: e.StartAt, EndAt: e.EndAt, Status: e.Status, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt})
	}
	sharedhttp.OK(w, EventListResponse{Events: resp, Total: total})
}

// ListMyEvents handles GET /events/mine.
func (h *EventHandler) ListMyEvents(w http.ResponseWriter, r *http.Request) {
	userID := sharedhttp.UserIDFromContext(r.Context())
	org, err := h.svc.GetOrganizer(r.Context(), userID)
	if err != nil {
		sharedhttp.NotFound(w, "organizer not found")
		return
	}
	events, err := h.svc.ListByOrganizer(r.Context(), org.ID)
	if err != nil {
		sharedhttp.InternalServerError(w, "failed to list events")
		return
	}
	resp := make([]EventResponse, 0, len(events))
	for _, e := range events {
		resp = append(resp, EventResponse{ID: e.ID, OrganizerID: e.OrganizerID, VenueName: e.VenueName, VenueAddress: e.VenueAddress, VenueCapacity: e.VenueCapacity, Title: e.Title, Description: e.Description, StartAt: e.StartAt, EndAt: e.EndAt, Status: e.Status, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt})
	}
	sharedhttp.OK(w, resp)
}

// ListPending handles GET /events/pending.
func (h *EventHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	events, total, err := h.svc.ListPending(r.Context(), limit, offset)
	if err != nil {
		sharedhttp.InternalServerError(w, "failed to list pending events")
		return
	}
	resp := make([]EventResponse, 0, len(events))
	for _, e := range events {
		resp = append(resp, EventResponse{ID: e.ID, OrganizerID: e.OrganizerID, VenueName: e.VenueName, VenueAddress: e.VenueAddress, VenueCapacity: e.VenueCapacity, Title: e.Title, Description: e.Description, StartAt: e.StartAt, EndAt: e.EndAt, Status: e.Status, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt})
	}
	sharedhttp.OK(w, EventListResponse{Events: resp, Total: total})
}

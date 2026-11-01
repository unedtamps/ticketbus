package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	sharedhttp "github.com/unedtamps/ticketbus/pkg/http"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/application/event"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
)

// defaultPageSize is the page size used when the client omits ?limit.
const defaultPageSize = 10

// maxPageSize caps a client-supplied ?limit. Without it any caller could ask
// for the entire table in one request.
const maxPageSize = 100

// EventHandler handles HTTP requests for the event service.
type EventHandler struct {
	svc      *event.EventService
	images   domain.ImageStorage
	validate *validator.Validate
}

// NewEventHandler creates a new EventHandler.
func NewEventHandler(svc *event.EventService, images domain.ImageStorage) *EventHandler {
	return &EventHandler{svc: svc, images: images, validate: validator.New()}
}

// imageURL resolves an event's photo URL; events without a photo get an empty
// string so the JSON field is omitted.
func (h *EventHandler) imageURL(e *domain.Event) string {
	if e.ImageKey == "" {
		return ""
	}
	return h.images.PublicURL(e.ImageKey)
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
	ticketTypes := make([]domain.TicketType, len(req.TicketTypes))
	for i, tt := range req.TicketTypes {
		mpo := tt.MaxPerOrder
		if mpo == 0 {
			mpo = 5
		}
		ticketTypes[i] = domain.TicketType{
			Name:        tt.Name,
			PriceRupiah: tt.PriceRupiah,
			Quantity:    tt.Quantity,
			MaxPerOrder: mpo,
		}
	}
	event, err := h.svc.CreateEvent(
		r.Context(),
		userID,
		req.Title,
		req.Description,
		req.VenueName,
		req.VenueAddress,
		req.VenueCapacity,
		req.StartAt,
		req.EndAt,
		ticketTypes,
	)
	if err != nil {
		if errors.Is(err, domain.ErrCapacityExceeded) ||
			errors.Is(err, domain.ErrTicketTypesEmpty) ||
			errors.Is(err, domain.ErrEventInvalidWindow) {
			sharedhttp.BadRequest(w, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "failed to create event")
		return
	}
	sharedhttp.Created(w, EventResponse{
		ID:            event.ID,
		OrganizerID:   event.OrganizerID,
		Title:         event.Title,
		Description:   event.Description,
		ImageURL:      h.imageURL(event),
		VenueName:     event.VenueName,
		VenueAddress:  event.VenueAddress,
		VenueCapacity: event.VenueCapacity,
		StartAt:       event.StartAt,
		EndAt:         event.EndAt,
		Status:        event.Status,
		CreatedAt:     event.CreatedAt,
		UpdatedAt:     event.UpdatedAt,
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
	event, err := h.svc.UpdateEvent(
		r.Context(),
		eventID,
		userID,
		req.Title,
		req.Description,
		req.StartAt,
		req.EndAt,
	)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		if errors.Is(err, domain.ErrNotEventOwner) || errors.Is(err, domain.ErrEventNotEditable) {
			sharedhttp.Forbidden(w, err.Error())
			return
		}
		if errors.Is(err, domain.ErrEventInvalidWindow) {
			sharedhttp.BadRequest(w, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "failed to update event")
		return
	}
	sharedhttp.OK(w, EventResponse{
		ID:            event.ID,
		OrganizerID:   event.OrganizerID,
		VenueName:     event.VenueName,
		VenueAddress:  event.VenueAddress,
		VenueCapacity: event.VenueCapacity,
		Title:         event.Title,
		Description:   event.Description,
		ImageURL:      h.imageURL(event),
		StartAt:       event.StartAt,
		EndAt:         event.EndAt,
		Status:        event.Status,
		CreatedAt:     event.CreatedAt,
		UpdatedAt:     event.UpdatedAt,
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
		ID:            event.ID,
		OrganizerID:   event.OrganizerID,
		VenueName:     event.VenueName,
		VenueAddress:  event.VenueAddress,
		VenueCapacity: event.VenueCapacity,
		Title:         event.Title,
		Description:   event.Description,
		ImageURL:      h.imageURL(event),
		StartAt:       event.StartAt,
		EndAt:         event.EndAt,
		Status:        event.Status,
		CreatedAt:     event.CreatedAt,
		UpdatedAt:     event.UpdatedAt,
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
		ID:            event.ID,
		OrganizerID:   event.OrganizerID,
		VenueName:     event.VenueName,
		VenueAddress:  event.VenueAddress,
		VenueCapacity: event.VenueCapacity,
		Title:         event.Title,
		Description:   event.Description,
		ImageURL:      h.imageURL(event),
		StartAt:       event.StartAt,
		EndAt:         event.EndAt,
		Status:        event.Status,
		CreatedAt:     event.CreatedAt,
		UpdatedAt:     event.UpdatedAt,
	})
}

// eventResponse maps an event onto the wire shape, including the resolved
// image URL. Single place so the cancel paths and the CRUD paths cannot drift.
func (h *EventHandler) eventResponse(event *domain.Event) EventResponse {
	return EventResponse{
		ID:            event.ID,
		OrganizerID:   event.OrganizerID,
		VenueName:     event.VenueName,
		VenueAddress:  event.VenueAddress,
		VenueCapacity: event.VenueCapacity,
		Title:         event.Title,
		Description:   event.Description,
		ImageURL:      h.imageURL(event),
		StartAt:       event.StartAt,
		EndAt:         event.EndAt,
		Status:        event.Status,
		CreatedAt:     event.CreatedAt,
		UpdatedAt:     event.UpdatedAt,
	}
}

// CancelEvent handles POST /events/:id/cancel (organizer).
func (h *EventHandler) CancelEvent(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	event, err := h.svc.CancelEvent(r.Context(), eventID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		// An ended event is not an invalid request — it is a request the
		// organizer is not allowed to make. 403 makes that distinction clear
		// so the dashboard can point them at support instead of showing a
		// validation error.
		if errors.Is(err, domain.ErrCancelAfterEnd) {
			sharedhttp.Error(w, http.StatusForbidden, err.Error())
			return
		}
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	sharedhttp.OK(w, h.eventResponse(event))
}

// AdminCancelEvent handles POST /api/admin/events/:id/cancel. It is the only
// way to cancel an event that has already ended.
func (h *EventHandler) AdminCancelEvent(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	event, err := h.svc.CancelEventAsAdmin(r.Context(), eventID)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	sharedhttp.OK(w, h.eventResponse(event))
}

// ReprocessCancellation handles POST /events/:id/cancel-reprocess (EO or
// admin). It re-publishes event.cancelled so pending bookings and late
// payments of the cancelled event are reconciled.
func (h *EventHandler) ReprocessCancellation(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	var (
		event *domain.Event
		err   error
	)
	if sharedhttp.UserRoleFromContext(r.Context()) == sdomain.RoleAdmin {
		event, err = h.svc.ReprocessCancellationAsAdmin(r.Context(), eventID)
	} else {
		event, err = h.svc.ReprocessCancellation(r.Context(), eventID, userID)
	}
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	sharedhttp.OK(w, map[string]interface{}{
		"event_id":  event.ID,
		"status":    event.Status,
		"reprocess": "queued",
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
		ttResp[i] = TicketTypeResponse{
			ID:          tt.ID,
			Name:        tt.Name,
			PriceRupiah: tt.PriceRupiah,
			Quantity:    tt.Quantity,
			Available:   tt.Available,
			MaxPerOrder: tt.MaxPerOrder,
		}
	}
	sharedhttp.OK(w, EventDetailResponse{
		Event: EventResponse{
			ReservationsOpen: reservationsOpen(event),
			ID:               event.ID,
			OrganizerID:      event.OrganizerID,
			VenueName:        event.VenueName,
			VenueAddress:     event.VenueAddress,
			VenueCapacity:    event.VenueCapacity,
			Title:            event.Title,
			Description:      event.Description,
			ImageURL:         h.imageURL(event),
			StartAt:          event.StartAt,
			EndAt:            event.EndAt,
			Status:           event.Status,
			CreatedAt:        event.CreatedAt,
			UpdatedAt:        event.UpdatedAt,
		},
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
		resp = append(
			resp,
			EventResponse{
				ReservationsOpen: reservationsOpen(&e),
				ID:               e.ID,
				OrganizerID:      e.OrganizerID,
				VenueName:        e.VenueName,
				VenueAddress:     e.VenueAddress,
				VenueCapacity:    e.VenueCapacity,
				Title:            e.Title,
				Description:      e.Description,
				ImageURL:         h.imageURL(&e),
				StartAt:          e.StartAt,
				EndAt:            e.EndAt,
				Status:           e.Status,
				CreatedAt:        e.CreatedAt,
				UpdatedAt:        e.UpdatedAt,
			},
		)
	}
	sharedhttp.OK(w, EventListResponse{Events: resp, Total: total})
}

// ListMyEvents handles GET /events/mine.
//
// Keyset-paginated on (created_at, id) descending: pass ?cursor from the
// previous response's next_cursor. Each page carries the whole-account summary
// so the dashboard header does not shrink to the visible page.
func (h *EventHandler) ListMyEvents(w http.ResponseWriter, r *http.Request) {
	userID := sharedhttp.UserIDFromContext(r.Context())

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}

	var cursor *domain.Cursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		parsed, err := domain.DecodeCursor(raw)
		if err != nil {
			sharedhttp.BadRequest(w, "invalid cursor")
			return
		}
		cursor = parsed
	}

	// One extra row tells us whether a further page exists without a COUNT.
	events, err := h.svc.ListByOrganizer(r.Context(), userID, limit+1, cursor)
	if err != nil {
		sharedhttp.InternalServerError(w, "failed to list events")
		return
	}

	nextCursor := ""
	if len(events) > limit {
		events = events[:limit]
		last := events[len(events)-1]
		nextCursor = domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}.Encode()
	}

	summary, err := h.svc.OrganizerSummary(r.Context(), userID)
	if err != nil {
		sharedhttp.InternalServerError(w, "failed to summarise events")
		return
	}

	resp := make([]OrganizerEventResponse, 0, len(events))
	for _, e := range events {
		resp = append(
			resp,
			OrganizerEventResponse{
				EventResponse: EventResponse{
					ID:            e.ID,
					OrganizerID:   e.OrganizerID,
					VenueName:     e.VenueName,
					VenueAddress:  e.VenueAddress,
					VenueCapacity: e.VenueCapacity,
					Title:         e.Title,
					Description:   e.Description,
					ImageURL:      h.imageURL(&e),
					StartAt:       e.StartAt,
					EndAt:         e.EndAt,
					Status:        e.Status,
					CreatedAt:     e.CreatedAt,
					UpdatedAt:     e.UpdatedAt,
				},
				PayoutStatus:       string(e.PayoutStatus),
				PayoutAmountRupiah: e.PayoutAmountRupiah,
				PayoutRequestedAt:  e.PayoutRequestedAt,
				PayoutCompletedAt:  e.PayoutCompletedAt,

				ConfirmedRevenueRupiah: e.ConfirmedRevenueRupiah,
			},
		)
	}

	sharedhttp.OK(w, OrganizerEventListResponse{
		Events:     resp,
		NextCursor: nextCursor,
		Summary: OrganizerSummaryResponse{
			AvailableToCollect: summary.AvailableToCollect,
			ConfirmedTotal:     summary.ConfirmedTotal,
			PaidOutTotal:       summary.PaidOutTotal,
			CollectableEvents:  summary.CollectableEvents,
		},
	})
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
		resp = append(
			resp,
			EventResponse{
				ReservationsOpen: reservationsOpen(&e),
				ID:               e.ID,
				OrganizerID:      e.OrganizerID,
				VenueName:        e.VenueName,
				VenueAddress:     e.VenueAddress,
				VenueCapacity:    e.VenueCapacity,
				Title:            e.Title,
				Description:      e.Description,
				ImageURL:         h.imageURL(&e),
				StartAt:          e.StartAt,
				EndAt:            e.EndAt,
				Status:           e.Status,
				CreatedAt:        e.CreatedAt,
				UpdatedAt:        e.UpdatedAt,
			},
		)
	}
	sharedhttp.OK(w, EventListResponse{Events: resp, Total: total})
}

// ListAll handles GET /admin/events (all events with optional status filter).
func (h *EventHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	events, total, err := h.svc.ListAll(r.Context(), status, limit, offset)
	if err != nil {
		sharedhttp.InternalServerError(w, "failed to list events")
		return
	}
	resp := make([]EventResponse, 0, len(events))
	for _, e := range events {
		resp = append(
			resp,
			EventResponse{
				ReservationsOpen: reservationsOpen(&e),
				ID:               e.ID,
				OrganizerID:      e.OrganizerID,
				VenueName:        e.VenueName,
				VenueAddress:     e.VenueAddress,
				VenueCapacity:    e.VenueCapacity,
				Title:            e.Title,
				Description:      e.Description,
				ImageURL:         h.imageURL(&e),
				StartAt:          e.StartAt,
				EndAt:            e.EndAt,
				Status:           e.Status,
				CreatedAt:        e.CreatedAt,
				UpdatedAt:        e.UpdatedAt,
			},
		)
	}
	sharedhttp.OK(w, EventListResponse{Events: resp, Total: total})
}

// UploadEventPhoto handles POST /api/events/{id}/photo (EO). Multipart field
// "photo"; the whole body is capped at 6 MB (5 MB image + multipart overhead)
// and the real content type is sniffed from the bytes, not from the filename.
func (h *EventHandler) UploadEventPhoto(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		sharedhttp.BadRequest(w, "invalid or oversized upload")
		return
	}
	file, header, err := r.FormFile("photo")
	if err != nil {
		sharedhttp.BadRequest(w, "missing photo field")
		return
	}
	defer file.Close()

	sniff := make([]byte, 512)
	n, _ := io.ReadFull(file, sniff)
	contentType := http.DetectContentType(sniff[:n])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		sharedhttp.InternalServerError(w, "failed to read upload")
		return
	}

	userID := sharedhttp.UserIDFromContext(r.Context())
	imageURL, err := h.svc.UploadEventPhoto(
		r.Context(), eventID, userID, contentType, file, header.Size,
	)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrEventNotFound), errors.Is(err, domain.ErrNotEventOwner):
			sharedhttp.NotFound(w, err.Error())
		case errors.Is(err, domain.ErrImageUnsupported),
			errors.Is(err, domain.ErrImageTooLarge),
			errors.Is(err, domain.ErrEventNotEditable):
			sharedhttp.BadRequest(w, err.Error())
		default:
			sharedhttp.InternalServerError(w, "failed to upload photo")
		}
		return
	}
	sharedhttp.OK(w, map[string]string{"image_url": imageURL})
}

// GetPayoutAccount handles GET /api/payout-account (EO).
func (h *EventHandler) GetPayoutAccount(w http.ResponseWriter, r *http.Request) {
	userID := sharedhttp.UserIDFromContext(r.Context())
	acct, err := h.svc.GetPayoutAccount(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrPayoutAccountAbsent) {
			sharedhttp.NotFound(w, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "failed to load payout account")
		return
	}
	sharedhttp.OK(w, PayoutAccountResponse{
		BankCode:              acct.BankCode,
		BankAccountNumber:     acct.BankAccountNumber,
		BankAccountHolderName: acct.BankAccountHolderName,
		CreatedAt:             acct.CreatedAt,
		UpdatedAt:             acct.UpdatedAt,
	})
}

// SetPayoutAccount handles PUT /api/payout-account (EO).
func (h *EventHandler) SetPayoutAccount(w http.ResponseWriter, r *http.Request) {
	var req PayoutAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sharedhttp.BadRequest(w, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		sharedhttp.BadRequest(w, err.Error())
		return
	}
	userID := sharedhttp.UserIDFromContext(r.Context())
	acct, err := h.svc.SetPayoutAccount(
		r.Context(), userID, req.BankCode, req.BankAccountNumber, req.BankAccountHolderName,
	)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidPayoutBank) {
			sharedhttp.BadRequest(w, err.Error())
			return
		}
		sharedhttp.InternalServerError(w, "failed to save payout account")
		return
	}
	sharedhttp.OK(w, PayoutAccountResponse{
		BankCode:              acct.BankCode,
		BankAccountNumber:     acct.BankAccountNumber,
		BankAccountHolderName: acct.BankAccountHolderName,
		CreatedAt:             acct.CreatedAt,
		UpdatedAt:             acct.UpdatedAt,
	})
}

// RequestPayout handles POST /api/events/{id}/payout (EO).
func (h *EventHandler) RequestPayout(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	attempt, err := h.svc.RequestPayout(r.Context(), eventID, userID)
	if err != nil {
		h.writePayoutError(w, err)
		return
	}
	sharedhttp.Accepted(w, PayoutAttemptResponse{
		EventID:               attempt.EventID,
		Attempt:               attempt.Attempt,
		Status:                string(attempt.Status),
		AmountRupiah:          attempt.AmountRupiah,
		BankCode:              attempt.BankCode,
		BankAccountNumber:     attempt.BankAccountNumber,
		BankAccountHolderName: attempt.BankAccountHolderName,
		RequestedAt:           attempt.RequestedAt,
		CompletedAt:           attempt.CompletedAt,
	})
}

// GetPayout handles GET /api/events/{id}/payout (EO).
func (h *EventHandler) GetPayout(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "id")
	userID := sharedhttp.UserIDFromContext(r.Context())
	summary, err := h.svc.GetEventPayout(r.Context(), eventID, userID)
	if err != nil {
		h.writePayoutError(w, err)
		return
	}
	attempts := make([]PayoutAttemptResponse, 0, len(summary.AttemptsList))
	for _, a := range summary.AttemptsList {
		attempts = append(attempts, PayoutAttemptResponse{
			EventID:               a.EventID,
			Attempt:               a.Attempt,
			Status:                string(a.Status),
			AmountRupiah:          a.AmountRupiah,
			BankCode:              a.BankCode,
			BankAccountNumber:     a.BankAccountNumber,
			BankAccountHolderName: a.BankAccountHolderName,
			ProviderRef:           a.ProviderRef,
			FailureReason:         a.FailureReason,
			RequestedAt:           a.RequestedAt,
			CompletedAt:           a.CompletedAt,
		})
	}
	sharedhttp.OK(w, PayoutResponse{
		EventID:       summary.EventID,
		Status:        string(summary.Status),
		AmountRupiah:  summary.AmountRupiah,
		Attempts:      summary.Attempts,
		AttemptList:   attempts,
		ProviderRef:   summary.ProviderRef,
		FailureReason: summary.FailureReason,
		RequestedAt:   summary.RequestedAt,
		CompletedAt:   summary.CompletedAt,
	})
}

func (h *EventHandler) writePayoutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrEventNotFound):
		sharedhttp.NotFound(w, err.Error())
	case errors.Is(err, domain.ErrNotEventOwner):
		sharedhttp.Forbidden(w, err.Error())
	case errors.Is(err, domain.ErrEventNotPayoutable),
		errors.Is(err, domain.ErrEventNotEnded),
		errors.Is(err, domain.ErrPayoutExists),
		errors.Is(err, domain.ErrPayoutInFlight):
		sharedhttp.Conflict(w, err.Error())
	case errors.Is(err, domain.ErrPayoutAccountAbsent),
		errors.Is(err, domain.ErrInvalidPayoutBank):
		sharedhttp.BadRequest(w, err.Error())
	default:
		sharedhttp.InternalServerError(w, "payout operation failed")
	}
}

// reservationsOpen reports whether the event still accepts reservations: it
// must be published, already started, and not over. The window before the end
// is enforced server side as well — this flag only tells the UI what to show.
func reservationsOpen(e *domain.Event) bool {
	if e.Status != sdomain.EventStatusPublished {
		return false
	}
	now := time.Now()
	return !now.Before(e.StartAt) && now.Before(e.EndAt)
}

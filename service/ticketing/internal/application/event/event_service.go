package event

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/pkg/outbox"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
)

// MaxImageBytes bounds one event photo.
const MaxImageBytes = 5 << 20

// allowedImageTypes maps an accepted image content type to its file extension.
var allowedImageTypes = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

// EventService orchestrates event management operations.
type EventService struct {
	repo            domain.EventRepository
	seatReader      domain.SeatReader
	seatInitializer domain.SeatInitializer
	outbox          outbox.StoreInterface
	images          domain.ImageStorage
}

// NewEventService creates a new EventService.
func NewEventService(
	repo domain.EventRepository,
	seatReader domain.SeatReader,
	seatInitializer domain.SeatInitializer,
	ob outbox.StoreInterface,
	images domain.ImageStorage,
) *EventService {
	return &EventService{
		repo:            repo,
		seatReader:      seatReader,
		seatInitializer: seatInitializer,
		outbox:          ob,
		images:          images,
	}
}

// CreateEvent creates a new event with initial status "draft" or "pending".
func (s *EventService) CreateEvent(
	ctx context.Context,
	organizerID, title, description, venueName, venueAddress string,
	venueCapacity int,
	startAt, endAt time.Time,
	ticketTypes []domain.TicketType,
) (*domain.Event, error) {
	if len(ticketTypes) == 0 {
		return nil, domain.ErrTicketTypesEmpty
	}
	if !endAt.After(startAt) {
		return nil, domain.ErrEventInvalidWindow
	}
	totalQty := 0
	for _, tt := range ticketTypes {
		totalQty += tt.Quantity
	}
	if totalQty > venueCapacity {
		return nil, domain.ErrCapacityExceeded
	}

	eventID := uuid.NewString()
	event := &domain.Event{
		ID:            eventID,
		OrganizerID:   organizerID,
		Title:         title,
		Description:   description,
		VenueName:     venueName,
		VenueAddress:  venueAddress,
		VenueCapacity: venueCapacity,
		StartAt:       startAt,
		EndAt:         endAt,
		Status:        sdomain.EventStatusPending,
	}

	if err := s.repo.CreateEvent(ctx, event); err != nil {
		return nil, err
	}

	for i := range ticketTypes {
		ticketTypes[i].ID = uuid.NewString()
		ticketTypes[i].EventID = eventID
	}
	if err := s.repo.CreateTicketTypes(ctx, ticketTypes); err != nil {
		return nil, err
	}

	return event, nil
}

// UpdateEvent updates an existing event.
func (s *EventService) UpdateEvent(
	ctx context.Context,
	eventID, organizerID, title, description string,
	startAt, endAt time.Time,
) (*domain.Event, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, domain.ErrEventNotFound
	}
	if event.OrganizerID != organizerID {
		return nil, domain.ErrNotEventOwner
	}
	if event.Status != sdomain.EventStatusDraft && event.Status != sdomain.EventStatusPending {
		return nil, domain.ErrEventNotEditable
	}
	if !endAt.After(startAt) {
		return nil, domain.ErrEventInvalidWindow
	}
	event.Title = title
	event.Description = description
	event.StartAt = startAt
	event.EndAt = endAt
	event.UpdatedAt = time.Now()
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

// UploadEventPhoto stores an event's photo in object storage and records its
// key on the event. Photos are cosmetic, so any non-cancelled event accepts
// one — unlike field edits, which are limited to draft/pending. The content
// type has already been sniffed by the handler; the size limit is re-checked
// here so nothing bypasses it.
func (s *EventService) UploadEventPhoto(
	ctx context.Context,
	eventID, organizerID, contentType string,
	r io.Reader,
	size int64,
) (string, error) {
	ext, ok := allowedImageTypes[contentType]
	if !ok {
		return "", domain.ErrImageUnsupported
	}
	if size <= 0 || size > MaxImageBytes {
		return "", domain.ErrImageTooLarge
	}
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return "", domain.ErrEventNotFound
	}
	if event.OrganizerID != organizerID {
		return "", domain.ErrNotEventOwner
	}
	if event.Status == sdomain.EventStatusCancelled {
		return "", domain.ErrEventNotEditable
	}

	key := fmt.Sprintf("events/%s/%s.%s", eventID, uuid.NewString(), ext)
	if err := s.images.Upload(ctx, key, r, size, contentType); err != nil {
		return "", err
	}
	if err := s.repo.SetEventImage(ctx, eventID, key); err != nil {
		return "", err
	}
	return s.images.PublicURL(key), nil
}

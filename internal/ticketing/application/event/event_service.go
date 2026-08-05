package event

import (
	"github.com/nedo/TicketSaas/internal/shared/outbox"
	"github.com/nedo/TicketSaas/internal/ticketing/domain"
)

// EventService orchestrates event management operations.
type EventService struct {
	repo            domain.EventRepository
	seatReader      domain.SeatReader
	seatInitializer domain.SeatInitializer
	outbox          outbox.StoreInterface
}

// NewEventService creates a new EventService.
func NewEventService(
	repo domain.EventRepository,
	seatReader domain.SeatReader,
	seatInitializer domain.SeatInitializer,
	ob outbox.StoreInterface,
) *EventService {
	return &EventService{
		repo:            repo,
		seatReader:      seatReader,
		seatInitializer: seatInitializer,
		outbox:          ob,
	}
}

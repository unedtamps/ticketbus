package postgres

import (
	"context"
	"time"

	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
	"github.com/nedo/TicketSaas/internal/event/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// EventRepo implements domain.EventRepository.
type EventRepo struct {
	db shareddb.DBTx
}

// NewEventRepo creates a new EventRepo.
func NewEventRepo(db shareddb.DBTx) *EventRepo {
	return &EventRepo{db: db}
}

// CreateOrganizer inserts a new organizer.
func (r *EventRepo) CreateOrganizer(ctx context.Context, org *domain.Organizer) error {
	_, err := r.db.Exec(ctx, `INSERT INTO organizers (id, user_id, name, description, profile_link, contact_email) VALUES ($1,$2,$3,$4,$5,$6)`,
		org.ID, org.UserID, org.Name, org.Description, org.ProfileLink, org.ContactEmail)
	return err
}

// FindOrganizerByUserID retrieves an organizer by user ID.
func (r *EventRepo) FindOrganizerByUserID(ctx context.Context, userID string) (*domain.Organizer, error) {
	var org domain.Organizer
	err := r.db.QueryRow(ctx, `SELECT id, user_id, name, description, profile_link, contact_email, created_at FROM organizers WHERE user_id=$1`, userID).
		Scan(&org.ID, &org.UserID, &org.Name, &org.Description, &org.ProfileLink, &org.ContactEmail, &org.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// CreateEvent inserts a new event.
func (r *EventRepo) CreateEvent(ctx context.Context, event *domain.Event) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO events (id, organizer_id, title, description, venue_name, venue_address, venue_capacity, start_at, end_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		event.ID, event.OrganizerID, event.Title, event.Description, event.VenueName, event.VenueAddress, event.VenueCapacity, event.StartAt, event.EndAt, string(event.Status))
	return err
}

// UpdateEvent updates an existing event.
func (r *EventRepo) UpdateEvent(ctx context.Context, event *domain.Event) error {
	_, err := r.db.Exec(ctx, `
		UPDATE events SET title=$1, description=$2, start_at=$3, end_at=$4, status=$5, reviewed_by=$6, reviewed_at=$7, updated_at=$8
		WHERE id=$9`,
		event.Title, event.Description, event.StartAt, event.EndAt, string(event.Status), event.ReviewedBy, event.ReviewedAt, time.Now(), event.ID)
	return err
}

// FindEventByID retrieves an event by ID.
func (r *EventRepo) FindEventByID(ctx context.Context, id string) (*domain.Event, error) {
	var e domain.Event
	var status string
	err := r.db.QueryRow(ctx, `
		SELECT id, organizer_id, title, description, venue_name, venue_address, venue_capacity, start_at, end_at, status, reviewed_by, reviewed_at, created_at, updated_at
		FROM events WHERE id=$1`, id).
		Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.VenueName, &e.VenueAddress, &e.VenueCapacity, &e.StartAt, &e.EndAt, &status, &e.ReviewedBy, &e.ReviewedAt, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	e.Status = sdomain.EventStatus(status)
	return &e, nil
}

// ListPublished returns published events with pagination.
func (r *EventRepo) ListPublished(ctx context.Context, limit, offset int) ([]domain.Event, int, error) {
	var total int
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE status='published'`).Scan(&total)

	rows, err := r.db.Query(ctx, `
		SELECT id, organizer_id, title, description, venue_name, venue_address, venue_capacity, start_at, end_at, status, created_at, updated_at
		FROM events WHERE status='published' ORDER BY start_at ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var status string
		if err := rows.Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.VenueName, &e.VenueAddress, &e.VenueCapacity, &e.StartAt, &e.EndAt, &status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, 0, err
		}
		e.Status = sdomain.EventStatus(status)
		events = append(events, e)
	}
	return events, total, nil
}

// ListByOrganizer returns events for an organizer.
func (r *EventRepo) ListByOrganizer(ctx context.Context, organizerID string) ([]domain.Event, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, organizer_id, title, description, venue_name, venue_address, venue_capacity, start_at, end_at, status, created_at, updated_at
		FROM events WHERE organizer_id=$1 ORDER BY created_at DESC`, organizerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var status string
		if err := rows.Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.VenueName, &e.VenueAddress, &e.VenueCapacity, &e.StartAt, &e.EndAt, &status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Status = sdomain.EventStatus(status)
		events = append(events, e)
	}
	return events, nil
}

// ListPending returns pending events with pagination.
func (r *EventRepo) ListPending(ctx context.Context, limit, offset int) ([]domain.Event, int, error) {
	var total int
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE status='pending'`).Scan(&total)

	rows, err := r.db.Query(ctx, `
		SELECT id, organizer_id, title, description, venue_name, venue_address, venue_capacity, start_at, end_at, status, created_at, updated_at
		FROM events WHERE status='pending' ORDER BY created_at ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var status string
		if err := rows.Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.VenueName, &e.VenueAddress, &e.VenueCapacity, &e.StartAt, &e.EndAt, &status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, 0, err
		}
		e.Status = sdomain.EventStatus(status)
		events = append(events, e)
	}
	return events, total, nil
}

// CreateTicketTypes inserts multiple ticket types.
func (r *EventRepo) CreateTicketTypes(ctx context.Context, types []domain.TicketType) error {
	for _, tt := range types {
		_, err := r.db.Exec(ctx, `INSERT INTO ticket_types (id, event_id, name, price_cents, quantity, max_per_order) VALUES ($1,$2,$3,$4,$5,$6)`,
			tt.ID, tt.EventID, tt.Name, tt.PriceCents, tt.Quantity, tt.MaxPerOrder)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListTicketTypesByEvent returns ticket types for an event.
func (r *EventRepo) ListTicketTypesByEvent(ctx context.Context, eventID string) ([]domain.TicketType, error) {
	rows, err := r.db.Query(ctx, `SELECT id, event_id, name, price_cents, quantity, max_per_order FROM ticket_types WHERE event_id=$1`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var types []domain.TicketType
	for rows.Next() {
		var tt domain.TicketType
		if err := rows.Scan(&tt.ID, &tt.EventID, &tt.Name, &tt.PriceCents, &tt.Quantity, &tt.MaxPerOrder); err != nil {
			return nil, err
		}
		types = append(types, tt)
	}
	return types, nil
}

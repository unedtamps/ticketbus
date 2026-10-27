package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
)

// EventRepo implements domain.EventRepository.
type EventRepo struct {
	pool *pgxpool.Pool
}

// NewEventRepo creates a new EventRepo.
func NewEventRepo(pool *pgxpool.Pool) *EventRepo {
	return &EventRepo{pool: pool}
}

// CreateEvent inserts a new event.
func (r *EventRepo) CreateEvent(ctx context.Context, event *domain.Event) error {
	_, err := r.pool.Exec(
		ctx,
		`
		INSERT INTO events (id, organizer_id, title, description, venue_name, venue_address, venue_capacity, start_at, end_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		event.ID,
		event.OrganizerID,
		event.Title,
		event.Description,
		event.VenueName,
		event.VenueAddress,
		event.VenueCapacity,
		event.StartAt,
		event.EndAt,
		string(event.Status),
	)
	return err
}

// UpdateEvent updates an existing event.
func (r *EventRepo) UpdateEvent(ctx context.Context, event *domain.Event) error {
	_, err := r.pool.Exec(
		ctx,
		`
		UPDATE events SET title=$1, description=$2, start_at=$3, end_at=$4, status=$5, reviewed_by=$6, reviewed_at=$7, updated_at=$8
		WHERE id=$9`,
		event.Title,
		event.Description,
		event.StartAt,
		event.EndAt,
		string(event.Status),
		event.ReviewedBy,
		event.ReviewedAt,
		time.Now(),
		event.ID,
	)
	return err
}

// FindEventByID retrieves an event by ID.
func (r *EventRepo) FindEventByID(ctx context.Context, id string) (*domain.Event, error) {
	var e domain.Event
	var status string
	err := r.pool.QueryRow(ctx, `
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
func (r *EventRepo) ListPublished(
	ctx context.Context,
	limit, offset int,
) ([]domain.Event, int, error) {
	var total int
	r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM events
		WHERE status='published' AND start_at <= NOW() AND NOW() < end_at`).
		Scan(&total)

	// Only live events are listed: a not-yet-started event cannot be booked
	// (no presale) and an ended event is over.
	rows, err := r.pool.Query(ctx, `
		SELECT id, organizer_id, title, description, venue_name, venue_address, venue_capacity, start_at, end_at, status, created_at, updated_at
		FROM events
		WHERE status='published' AND start_at <= NOW() AND NOW() < end_at
		ORDER BY start_at ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var status string
		if err := rows.Scan(
			&e.ID,
			&e.OrganizerID,
			&e.Title,
			&e.Description,
			&e.VenueName,
			&e.VenueAddress,
			&e.VenueCapacity,
			&e.StartAt,
			&e.EndAt,
			&status,
			&e.CreatedAt,
			&e.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		e.Status = sdomain.EventStatus(status)
		events = append(events, e)
	}
	return events, total, nil
}

// ListByOrganizer returns events for an organizer, including the payout view
// fields (status/amount/timestamps) via a LEFT JOIN.
func (r *EventRepo) ListByOrganizer(
	ctx context.Context,
	organizerID string,
) ([]domain.Event, error) {
	// The payout state is aggregated over the event's attempts: paid_out if any
	// attempt paid out, otherwise the latest attempt's status, and the amount
	// paid out so far.
	rows, err := r.pool.Query(ctx, `
		SELECT e.id, e.organizer_id, e.title, e.description, e.venue_name, e.venue_address,
		       e.venue_capacity, e.start_at, e.end_at, e.status, e.created_at, e.updated_at,
		       COALESCE(p.status, 'none'), COALESCE(p.amount, 0),
		       p.requested_at, p.completed_at
		FROM events e
		LEFT JOIN LATERAL (
			SELECT
				CASE
					WHEN COUNT(*) FILTER (WHERE a.status = 'paid_out') > 0 THEN 'paid_out'
					ELSE COALESCE((array_agg(a.status ORDER BY a.attempt DESC))[1], 'none')
				END AS status,
				COALESCE(SUM(a.amount_rupiah) FILTER (WHERE a.status = 'paid_out'), 0) AS amount,
				(array_agg(a.requested_at ORDER BY a.attempt DESC))[1] AS requested_at,
				(array_agg(a.completed_at ORDER BY a.attempt DESC)
					FILTER (WHERE a.completed_at IS NOT NULL))[1] AS completed_at
			FROM event_payout_attempts a
			WHERE a.event_id = e.id
		) p ON TRUE
		WHERE e.organizer_id=$1
		ORDER BY e.created_at DESC`, organizerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var status string
		var payoutStatus string
		if err := rows.Scan(
			&e.ID,
			&e.OrganizerID,
			&e.Title,
			&e.Description,
			&e.VenueName,
			&e.VenueAddress,
			&e.VenueCapacity,
			&e.StartAt,
			&e.EndAt,
			&status,
			&e.CreatedAt,
			&e.UpdatedAt,
			&payoutStatus,
			&e.PayoutAmountRupiah,
			&e.PayoutRequestedAt,
			&e.PayoutCompletedAt,
		); err != nil {
			return nil, err
		}
		e.Status = sdomain.EventStatus(status)
		e.PayoutStatus = sdomain.PayoutStatus(payoutStatus)
		events = append(events, e)
	}
	return events, rows.Err()
}

// ListPending returns pending events with pagination.
func (r *EventRepo) ListPending(
	ctx context.Context,
	limit, offset int,
) ([]domain.Event, int, error) {
	var total int
	r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE status='pending'`).Scan(&total)

	rows, err := r.pool.Query(ctx, `
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
		if err := rows.Scan(
			&e.ID,
			&e.OrganizerID,
			&e.Title,
			&e.Description,
			&e.VenueName,
			&e.VenueAddress,
			&e.VenueCapacity,
			&e.StartAt,
			&e.EndAt,
			&status,
			&e.CreatedAt,
			&e.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		e.Status = sdomain.EventStatus(status)
		events = append(events, e)
	}
	return events, total, nil
}

// FindAll returns events filtered by status (admin). If status is empty, returns all events.
func (r *EventRepo) FindAll(
	ctx context.Context,
	status string,
	limit, offset int,
) ([]domain.Event, int, error) {
	var total int
	r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE ($1 = '' OR status = $1)`, status).
		Scan(&total)

	rows, err := r.pool.Query(ctx, `
		SELECT id, organizer_id, title, description, venue_name, venue_address, venue_capacity, start_at, end_at, status, created_at, updated_at
		FROM events WHERE ($1 = '' OR status = $1) ORDER BY created_at DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var st string
		if err := rows.Scan(
			&e.ID,
			&e.OrganizerID,
			&e.Title,
			&e.Description,
			&e.VenueName,
			&e.VenueAddress,
			&e.VenueCapacity,
			&e.StartAt,
			&e.EndAt,
			&st,
			&e.CreatedAt,
			&e.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		e.Status = sdomain.EventStatus(st)
		events = append(events, e)
	}
	return events, total, nil
}

// CreateTicketTypes inserts multiple ticket types.
func (r *EventRepo) CreateTicketTypes(ctx context.Context, types []domain.TicketType) error {
	for _, tt := range types {
		_, err := r.pool.Exec(
			ctx,
			`INSERT INTO ticket_types (id, event_id, name, price_rupiah, quantity, max_per_order) VALUES ($1,$2,$3,$4,$5,$6)`,
			tt.ID,
			tt.EventID,
			tt.Name,
			tt.PriceRupiah,
			tt.Quantity,
			tt.MaxPerOrder,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListTicketTypesByEvent returns ticket types for an event.
func (r *EventRepo) ListTicketTypesByEvent(
	ctx context.Context,
	eventID string,
) ([]domain.TicketType, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id, event_id, name, price_rupiah, quantity, max_per_order FROM ticket_types WHERE event_id=$1`,
		eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var types []domain.TicketType
	for rows.Next() {
		var tt domain.TicketType
		if err := rows.Scan(
			&tt.ID,
			&tt.EventID,
			&tt.Name,
			&tt.PriceRupiah,
			&tt.Quantity,
			&tt.MaxPerOrder,
		); err != nil {
			return nil, err
		}
		types = append(types, tt)
	}
	return types, nil
}

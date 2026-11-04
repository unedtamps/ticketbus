package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/pkg/outbox"
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

// CancelAndPublish marks a published event cancelled and writes the
// event.cancelled outbox row in one database transaction.
//
// The cancellation cascade that follows (bookings cancelled, seats released,
// refunds started) is driven entirely by that outbox row, so a lost insert
// after a standalone status update would leave the event "cancelled" in name
// only: consumers never hear about it, and the organizer already saw a 200.
// The status change and its announcement commit together or not at all.
//
// It reports whether the update was applied; false means the event was no
// longer published (a concurrent cancel or status change won the race).
func (r *EventRepo) CancelAndPublish(ctx context.Context, eventID string, ev domain.OutboxEvent) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE events SET status=$1, updated_at=NOW() WHERE id=$2 AND status=$3`,
		string(sdomain.EventStatusCancelled), eventID, string(sdomain.EventStatusPublished))
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	if err := outbox.NewStore(tx).Insert(ctx, ev.Topic, ev.Key, ev.Payload); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// FindEventByID retrieves an event by ID.
func (r *EventRepo) FindEventByID(ctx context.Context, id string) (*domain.Event, error) {
	var e domain.Event
	var status string
	err := r.pool.QueryRow(ctx, `
		SELECT id, organizer_id, title, description, image_key, venue_name, venue_address, venue_capacity, start_at, end_at, status, reviewed_by, reviewed_at, created_at, updated_at
		FROM events WHERE id=$1`, id).
		Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.ImageKey, &e.VenueName, &e.VenueAddress, &e.VenueCapacity, &e.StartAt, &e.EndAt, &status, &e.ReviewedBy, &e.ReviewedAt, &e.CreatedAt, &e.UpdatedAt)
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
		SELECT id, organizer_id, title, description, image_key, venue_name, venue_address, venue_capacity, start_at, end_at, status, created_at, updated_at
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
			&e.ImageKey,
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

// ListByOrganizer returns one page of an organizer's events, newest first,
// including the payout view fields (status/amount/timestamps) and confirmed
// revenue. Pagination is keyset on (created_at, id): pass nil for the first
// page, then the cursor of the last row returned.
//
// Callers should request limit+1 rows to learn whether another page exists
// without an extra COUNT query.
func (r *EventRepo) ListByOrganizer(
	ctx context.Context,
	organizerID string,
	limit int,
	cursor *domain.Cursor,
) ([]domain.Event, error) {
	// The payout state is aggregated over the event's attempts: paid_out if any
	// attempt paid out, otherwise the latest attempt's status, and the amount
	// paid out so far.
	//
	// The seek predicate is a row-value comparison: descending order means the
	// next page is strictly "less than" the cursor pair. With a cursor the four
	// placeholders are ($1 org, $2 ts, $3 id, $4 limit); without it they are
	// ($1 org, $2 limit).
	seek := ""
	if cursor != nil {
		seek = "AND (e.created_at, e.id) < ($2, $3)"
	}
	limitArg := "$2"
	if cursor != nil {
		limitArg = "$4"
	}

	rows, err := r.pool.Query(ctx, `
		SELECT e.id, e.organizer_id, e.title, e.description, e.image_key, e.venue_name, e.venue_address,
		       e.venue_capacity, e.start_at, e.end_at, e.status, e.created_at, e.updated_at,
		       COALESCE(p.status, 'none'), COALESCE(p.amount, 0),
		       p.requested_at, p.completed_at,
		       COALESCE(rev.confirmed, 0)
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
		-- Confirmed booking revenue. Subtracting the paid-out amount gives what
		-- is still collectable. Indexed by idx_bookings_event_status.
		LEFT JOIN LATERAL (
			SELECT SUM(bi.total_price_rupiah) AS confirmed
			FROM bookings b
			JOIN booking_items bi ON bi.booking_id = b.id
			WHERE b.event_id = e.id AND b.status = 'confirmed'
		) rev ON TRUE
		WHERE e.organizer_id = $1 `+seek+`
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT `+limitArg, r.organizerArgs(organizerID, limit, cursor)...)
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
			&e.ImageKey,
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
			&e.ConfirmedRevenueRupiah,
		); err != nil {
			return nil, err
		}
		e.Status = sdomain.EventStatus(status)
		e.PayoutStatus = sdomain.PayoutStatus(payoutStatus)
		events = append(events, e)
	}
	return events, rows.Err()
}

// organizerArgs builds the placeholder values for ListByOrganizer. The cursor
// shifts the limit placeholder from $2 to $4, so the two orderings are kept in
// one place instead of scattered through the query builder.
func (r *EventRepo) organizerArgs(organizerID string, limit int, cursor *domain.Cursor) []any {
	if cursor == nil {
		return []any{organizerID, limit}
	}
	return []any{organizerID, cursor.CreatedAt, cursor.ID, limit}
}

// OrganizerSummary aggregates every event an organizer owns: confirmed
// revenue, amount already paid out, what is still collectable, and counts.
//
// This is deliberately separate from ListByOrganizer so the dashboard header
// stays correct while the event list is paginated. GREATEST(0, confirmed -
// paid_out) matches the per-row clamp the client applies, so the sum can never
// disagree with the rows beneath it.
func (r *EventRepo) OrganizerSummary(
	ctx context.Context,
	organizerID string,
) (*domain.OrganizerSummary, error) {
	var s domain.OrganizerSummary
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(x.confirmed), 0),
		       COALESCE(SUM(x.paid_out), 0),
		       COALESCE(SUM(GREATEST(0, x.confirmed - x.paid_out)), 0),
		       COUNT(*) FILTER (WHERE x.confirmed > x.paid_out)
		FROM (
			SELECT COALESCE((
			           SELECT SUM(bi.total_price_rupiah)
			           FROM bookings b
			           JOIN booking_items bi ON bi.booking_id = b.id
			           WHERE b.event_id = e.id AND b.status = 'confirmed'
			       ), 0) AS confirmed,
			       COALESCE((
			           SELECT SUM(a.amount_rupiah)
			           FROM event_payout_attempts a
			           WHERE a.event_id = e.id AND a.status = 'paid_out'
			       ), 0) AS paid_out
			FROM events e
			WHERE e.organizer_id = $1
		) x`, organizerID,
	).Scan(
		&s.ConfirmedTotal,
		&s.PaidOutTotal,
		&s.AvailableToCollect,
		&s.CollectableEvents,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListPending returns pending events with pagination.
func (r *EventRepo) ListPending(
	ctx context.Context,
	limit, offset int,
) ([]domain.Event, int, error) {
	var total int
	r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE status='pending'`).Scan(&total)

	rows, err := r.pool.Query(ctx, `
		SELECT id, organizer_id, title, description, image_key, venue_name, venue_address, venue_capacity, start_at, end_at, status, created_at, updated_at
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
			&e.ImageKey,
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
		SELECT id, organizer_id, title, description, image_key, venue_name, venue_address, venue_capacity, start_at, end_at, status, created_at, updated_at
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
			&e.ImageKey,
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

// SetEventImage records the object key of the event's photo.
func (r *EventRepo) SetEventImage(ctx context.Context, eventID, imageKey string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE events SET image_key=$2, updated_at=NOW() WHERE id=$1`,
		eventID, imageKey,
	)
	return err
}

// CreateTicketTypes inserts multiple ticket types.
//
// available_seat starts at full quantity, not at the column default of 0.
// Approval re-sets it (SeatCounter.Init), but a pending event is readable
// before that: with 0 available it rendered as "100% sold / sold out" even
// though nothing had been sold yet.
func (r *EventRepo) CreateTicketTypes(ctx context.Context, types []domain.TicketType) error {
	for _, tt := range types {
		_, err := r.pool.Exec(
			ctx,
			`INSERT INTO ticket_types (id, event_id, name, price_rupiah, quantity, available_seat, max_per_order) VALUES ($1,$2,$3,$4,$5,$5,$6)`,
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

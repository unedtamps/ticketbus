package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unedtamps/ticketbus/pkg/outbox"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
)

// BookingRepo implements domain.BookingRepository.
type BookingRepo struct {
	pool *pgxpool.Pool
}

// NewBookingRepo creates a new BookingRepo.
func NewBookingRepo(pool *pgxpool.Pool) *BookingRepo {
	return &BookingRepo{pool: pool}
}

// CreateReservation atomically decrements the seat counters, inserts the
// booking with its items, and writes the outbox event — all in one
// transaction. A partial reservation (seats taken but no booking, or booking
// without its payment-initiate outbox row) is impossible.
func (r *BookingRepo) CreateReservation(ctx context.Context, b *domain.Booking, ev domain.OutboxEvent) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	seatCounter := NewSeatCounter(tx)
	for _, item := range b.Items {
		if err := seatCounter.Reserve(ctx, b.EventID, item.TicketTypeID, item.Quantity); err != nil {
			return err
		}
	}

	if err := insertBooking(ctx, tx, b); err != nil {
		return err
	}

	if err := outbox.NewStore(tx).Insert(ctx, ev.Topic, ev.Key, ev.Payload); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// insertBooking writes a booking row and its items inside an open transaction.
//
// The insert is conditional on the event still being reservable: published,
// already started, and not over. The check happens inside the transaction, so
// an event cancelled between the service-level check and this insert cannot
// be booked. When the condition does not hold, no row is written and the
// caller rolls the transaction back — the seat decrement is undone with it.
func insertBooking(ctx context.Context, tx pgx.Tx, b *domain.Booking) error {
	tag, err := tx.Exec(ctx, `
		INSERT INTO bookings (id, user_id, event_id, status, expires_at, customer_email)
		SELECT $1,$2,$3,$4,$5,$6
		WHERE EXISTS (
			SELECT 1 FROM events
			WHERE id = $3 AND status = 'published' AND start_at <= NOW() AND NOW() < end_at
		)`,
		b.ID, b.UserID, b.EventID, b.Status, b.ExpiresAt, b.CustomerEmail)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrEventNotActive
	}

	for _, item := range b.Items {
		totalPrice := item.TotalPriceRupiah
		if totalPrice == 0 {
			totalPrice = item.UnitPriceRupiah * item.Quantity
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO booking_items (id, booking_id, ticket_type_id, quantity, total_price_rupiah)
			VALUES ($1,$2,$3,$4,$5)`,
			item.ID, item.BookingID, item.TicketTypeID, item.Quantity, totalPrice)
		if err != nil {
			return err
		}
	}
	return nil
}

// FindByID retrieves a booking by ID, with the event context and ticket names
// the payment and confirmation screens need to name what was bought. Items come
// from the same join, so a booking costs one query rather than one per item.
func (r *BookingRepo) FindByID(ctx context.Context, id string) (*domain.Booking, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT b.id, b.user_id, b.event_id, b.status, b.expires_at, b.created_at, b.customer_email,
		       COALESCE(e.title, ''), COALESCE(e.start_at, 'epoch'::timestamptz),
		       COALESCE(e.venue_name, ''),
		       bi.id, bi.ticket_type_id, bi.quantity, bi.total_price_rupiah,
		       COALESCE(tt.name, '')
		FROM bookings b
		LEFT JOIN events e ON e.id = b.event_id
		LEFT JOIN booking_items bi ON bi.booking_id = b.id
		LEFT JOIN ticket_types tt ON tt.id = bi.ticket_type_id
		WHERE b.id = $1
		ORDER BY bi.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var b *domain.Booking
	for rows.Next() {
		var (
			row      domain.Booking
			rowStart time.Time
			itemID   *string
			ticketID *string
			quantity *int
			price    *int
			name     string
		)
		if err := rows.Scan(
			&row.ID, &row.UserID, &row.EventID, &row.Status, &row.ExpiresAt, &row.CreatedAt, &row.CustomerEmail,
			&row.EventTitle, &rowStart, &row.EventVenue,
			&itemID, &ticketID, &quantity, &price, &name,
		); err != nil {
			return nil, err
		}
		if b == nil {
			row.EventStartAt = rowStart
			b = &row
		}
		// A booking with no items still yields one row, with NULL item columns.
		if itemID == nil || quantity == nil || price == nil {
			continue
		}
		item := domain.BookingItem{
			ID:               *itemID,
			BookingID:        b.ID,
			TicketTypeID:     deref(ticketID),
			Quantity:         *quantity,
			TotalPriceRupiah: *price,
			TicketTypeName:   name,
		}
		if item.Quantity > 0 {
			item.UnitPriceRupiah = item.TotalPriceRupiah / item.Quantity
		}
		b.TotalRupiah += item.TotalPriceRupiah
		b.Items = append(b.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if b == nil {
		return nil, pgx.ErrNoRows
	}
	return b, nil
}

// ListByUser returns bookings for a user, newest first, with the event context
// and ticket names the buyer's own list needs. Items come from one join rather
// than a query per booking.
func (r *BookingRepo) ListByUser(ctx context.Context, userID string) ([]domain.Booking, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT b.id, b.user_id, b.event_id, b.status, b.expires_at, b.created_at,
		       COALESCE(e.title, ''), COALESCE(e.start_at, 'epoch'::timestamptz),
		       COALESCE(e.venue_name, ''),
		       bi.id, bi.ticket_type_id, bi.quantity, bi.total_price_rupiah,
		       COALESCE(tt.name, '')
		FROM bookings b
		LEFT JOIN events e ON e.id = b.event_id
		LEFT JOIN booking_items bi ON bi.booking_id = b.id
		LEFT JOIN ticket_types tt ON tt.id = bi.ticket_type_id
		WHERE b.user_id = $1
		ORDER BY b.created_at DESC, bi.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookings []domain.Booking
	// index of the booking the current row belongs to
	seen := map[string]int{}
	for rows.Next() {
		var (
			b            domain.Booking
			startAt      time.Time
			itemID       *string
			ticketTypeID *string
			quantity     *int
			totalPrice   *int
			ticketName   string
		)
		if err := rows.Scan(
			&b.ID, &b.UserID, &b.EventID, &b.Status, &b.ExpiresAt, &b.CreatedAt,
			&b.EventTitle, &startAt, &b.EventVenue,
			&itemID, &ticketTypeID, &quantity, &totalPrice, &ticketName,
		); err != nil {
			return nil, err
		}
		b.EventStartAt = startAt

		idx, ok := seen[b.ID]
		if !ok {
			idx = len(bookings)
			seen[b.ID] = idx
			bookings = append(bookings, b)
		}
		// A booking with no items still yields one row, with NULL item columns.
		if itemID == nil || quantity == nil || totalPrice == nil {
			continue
		}
		item := domain.BookingItem{
			ID:               *itemID,
			BookingID:        b.ID,
			TicketTypeID:     deref(ticketTypeID),
			Quantity:         *quantity,
			TotalPriceRupiah: *totalPrice,
			TicketTypeName:   ticketName,
		}
		if item.Quantity > 0 {
			item.UnitPriceRupiah = item.TotalPriceRupiah / item.Quantity
		}
		bookings[idx].TotalRupiah += item.TotalPriceRupiah
		bookings[idx].Items = append(bookings[idx].Items, item)
	}
	return bookings, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// UpdateStatusIfPending transitions a booking only if it is still pending.
// It reports whether the transition was applied, guarding against races
// between confirm, release, expiry, and the recovery sweeper.
func (r *BookingRepo) UpdateStatusIfPending(ctx context.Context, bookingID, status string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE bookings SET status=$1 WHERE id=$2 AND status='pending'`, status, bookingID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ConfirmWithTickets transitions a pending booking to confirmed and writes one
// ticket.issued outbox row per item in one database transaction.
//
// Running the transition and the inserts separately meant a failed insert (or
// a crash mid-loop) left a confirmed booking with no — or only some of its —
// tickets: UpdateStatusIfPending never matches again, so nothing re-fires and
// there is no retry. The confirm and its tickets land together or not at all.
//
// It reports whether the transition was applied; false means the booking was
// no longer pending (duplicate delivery or already settled).
func (r *BookingRepo) ConfirmWithTickets(ctx context.Context, bookingID string, events []domain.OutboxEvent) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE bookings SET status='confirmed' WHERE id=$1 AND status='pending'`, bookingID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	store := outbox.NewStore(tx)
	for _, ev := range events {
		if err := store.Insert(ctx, ev.Topic, ev.Key, ev.Payload); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// TransitionAndReleaseSeats atomically transitions a booking guarded by its
// current status and releases its seats, all in one transaction. A crash
// between the status update and the seat release is therefore impossible.
// It reports whether the transition was applied.
func (r *BookingRepo) TransitionAndReleaseSeats(ctx context.Context, booking *domain.Booking, toStatus string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `UPDATE bookings SET status=$1 WHERE id=$2 AND status=$3`, toStatus, booking.ID, booking.Status)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	for _, item := range booking.Items {
		if _, err := tx.Exec(ctx, `
			UPDATE ticket_types
			SET available_seat = LEAST(quantity, available_seat + $1)
			WHERE id = $2`, item.Quantity, item.TicketTypeID); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// loadItems loads booking_items into a booking and derives compatibility fields.
func (r *BookingRepo) loadItems(ctx context.Context, b *domain.Booking) error {
	itemRows, err := r.pool.Query(ctx, `SELECT id, booking_id, ticket_type_id, quantity, total_price_rupiah FROM booking_items WHERE booking_id=$1`, b.ID)
	if err != nil {
		return err
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var item domain.BookingItem
		if err := itemRows.Scan(&item.ID, &item.BookingID, &item.TicketTypeID, &item.Quantity, &item.TotalPriceRupiah); err != nil {
			return err
		}
		if item.Quantity > 0 {
			item.UnitPriceRupiah = item.TotalPriceRupiah / item.Quantity
		}
		b.TotalRupiah += item.TotalPriceRupiah
		b.Items = append(b.Items, item)
	}
	return itemRows.Err()
}

// ListByEventID returns all bookings for an event.
func (r *BookingRepo) ListByEventID(ctx context.Context, eventID string) ([]domain.Booking, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, user_id, event_id, status, expires_at, created_at FROM bookings WHERE event_id=$1 ORDER BY created_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookings []domain.Booking
	for rows.Next() {
		var b domain.Booking
		if err := rows.Scan(&b.ID, &b.UserID, &b.EventID, &b.Status, &b.ExpiresAt, &b.CreatedAt); err != nil {
			return nil, err
		}
		bookings = append(bookings, b)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range bookings {
		itemRows, err := r.pool.Query(ctx, `SELECT id, booking_id, ticket_type_id, quantity, total_price_rupiah FROM booking_items WHERE booking_id=$1`, bookings[i].ID)
		if err != nil {
			return nil, err
		}
		for itemRows.Next() {
			var item domain.BookingItem
			if err := itemRows.Scan(&item.ID, &item.BookingID, &item.TicketTypeID, &item.Quantity, &item.TotalPriceRupiah); err != nil {
				itemRows.Close()
				return nil, err
			}
			if item.Quantity > 0 {
				item.UnitPriceRupiah = item.TotalPriceRupiah / item.Quantity
			}
			bookings[i].TotalRupiah += item.TotalPriceRupiah
			bookings[i].Items = append(bookings[i].Items, item)
		}
		itemRows.Close()
	}
	return bookings, nil
}

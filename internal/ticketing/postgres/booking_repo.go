package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nedo/TicketSaas/internal/ticketing/domain"
)

// BookingRepo implements domain.BookingRepository.
type BookingRepo struct {
	pool *pgxpool.Pool
}

// NewBookingRepo creates a new BookingRepo.
func NewBookingRepo(pool *pgxpool.Pool) *BookingRepo {
	return &BookingRepo{pool: pool}
}

// Create inserts a new booking with items.
func (r *BookingRepo) Create(ctx context.Context, b *domain.Booking) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO bookings (id, user_id, event_id, status, expires_at)
		VALUES ($1,$2,$3,$4,$5)`,
		b.ID, b.UserID, b.EventID, b.Status, b.ExpiresAt)
	if err != nil {
		return err
	}

	for _, item := range b.Items {
		totalPrice := item.TotalPrice
		if totalPrice == 0 {
			totalPrice = item.UnitPriceCents * item.Quantity
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO booking_items (id, booking_id, ticket_type_id, quantity, total_price)
			VALUES ($1,$2,$3,$4,$5)`,
			item.ID, item.BookingID, item.TicketTypeID, item.Quantity, totalPrice)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// FindByID retrieves a booking by ID.
func (r *BookingRepo) FindByID(ctx context.Context, id string) (*domain.Booking, error) {
	var b domain.Booking
	err := r.pool.QueryRow(ctx, `SELECT id, user_id, event_id, status, expires_at, created_at FROM bookings WHERE id=$1`, id).
		Scan(&b.ID, &b.UserID, &b.EventID, &b.Status, &b.ExpiresAt, &b.CreatedAt)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT id, booking_id, ticket_type_id, quantity, total_price FROM booking_items WHERE booking_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.BookingItem
		if err := rows.Scan(&item.ID, &item.BookingID, &item.TicketTypeID, &item.Quantity, &item.TotalPrice); err != nil {
			return nil, err
		}
		if item.Quantity > 0 {
			item.UnitPriceCents = item.TotalPrice / item.Quantity
		}
		b.TotalCents += item.TotalPrice
		b.Items = append(b.Items, item)
	}
	return &b, nil
}

// ListByUser returns bookings for a user.
func (r *BookingRepo) ListByUser(ctx context.Context, userID string) ([]domain.Booking, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, user_id, event_id, status, expires_at, created_at FROM bookings WHERE user_id=$1 ORDER BY created_at DESC`, userID)
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
		itemRows, err := r.pool.Query(ctx, `SELECT id, booking_id, ticket_type_id, quantity, total_price FROM booking_items WHERE booking_id=$1`, bookings[i].ID)
		if err != nil {
			return nil, err
		}
		for itemRows.Next() {
			var item domain.BookingItem
			if err := itemRows.Scan(&item.ID, &item.BookingID, &item.TicketTypeID, &item.Quantity, &item.TotalPrice); err != nil {
				itemRows.Close()
				return nil, err
			}
			if item.Quantity > 0 {
				item.UnitPriceCents = item.TotalPrice / item.Quantity
			}
			bookings[i].TotalCents += item.TotalPrice
			bookings[i].Items = append(bookings[i].Items, item)
		}
		itemRows.Close()
	}
	return bookings, nil
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

// Delete removes a pending booking. Used to roll back a reserve when the
// payment transaction could not be created.
func (r *BookingRepo) Delete(ctx context.Context, bookingID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM bookings WHERE id=$1 AND status='pending'`, bookingID)
	return err
}

// loadItems loads booking_items into a booking and derives compatibility fields.
func (r *BookingRepo) loadItems(ctx context.Context, b *domain.Booking) error {
	itemRows, err := r.pool.Query(ctx, `SELECT id, booking_id, ticket_type_id, quantity, total_price FROM booking_items WHERE booking_id=$1`, b.ID)
	if err != nil {
		return err
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var item domain.BookingItem
		if err := itemRows.Scan(&item.ID, &item.BookingID, &item.TicketTypeID, &item.Quantity, &item.TotalPrice); err != nil {
			return err
		}
		if item.Quantity > 0 {
			item.UnitPriceCents = item.TotalPrice / item.Quantity
		}
		b.TotalCents += item.TotalPrice
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
		itemRows, err := r.pool.Query(ctx, `SELECT id, booking_id, ticket_type_id, quantity, total_price FROM booking_items WHERE booking_id=$1`, bookings[i].ID)
		if err != nil {
			return nil, err
		}
		for itemRows.Next() {
			var item domain.BookingItem
			if err := itemRows.Scan(&item.ID, &item.BookingID, &item.TicketTypeID, &item.Quantity, &item.TotalPrice); err != nil {
				itemRows.Close()
				return nil, err
			}
			if item.Quantity > 0 {
				item.UnitPriceCents = item.TotalPrice / item.Quantity
			}
			bookings[i].TotalCents += item.TotalPrice
			bookings[i].Items = append(bookings[i].Items, item)
		}
		itemRows.Close()
	}
	return bookings, nil
}

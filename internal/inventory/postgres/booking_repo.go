package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nedo/TicketSaas/internal/inventory/domain"
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
		INSERT INTO bookings (id, user_id, event_id, status, total_cents, payment_id, refund_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		b.ID, b.UserID, b.EventID, b.Status, b.TotalCents, b.PaymentID, b.RefundStatus)
	if err != nil {
		return err
	}

	for _, item := range b.Items {
		_, err = tx.Exec(ctx, `
			INSERT INTO booking_items (id, booking_id, ticket_type_id, quantity, unit_price_cents)
			VALUES ($1,$2,$3,$4,$5)`,
			item.ID, item.BookingID, item.TicketTypeID, item.Quantity, item.UnitPriceCents)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// FindByID retrieves a booking by ID.
func (r *BookingRepo) FindByID(ctx context.Context, id string) (*domain.Booking, error) {
	var b domain.Booking
	err := r.pool.QueryRow(ctx, `SELECT id, user_id, event_id, status, total_cents, payment_id, refund_status, created_at FROM bookings WHERE id=$1`, id).
		Scan(&b.ID, &b.UserID, &b.EventID, &b.Status, &b.TotalCents, &b.PaymentID, &b.RefundStatus, &b.CreatedAt)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT id, booking_id, ticket_type_id, quantity, unit_price_cents FROM booking_items WHERE booking_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.BookingItem
		if err := rows.Scan(&item.ID, &item.BookingID, &item.TicketTypeID, &item.Quantity, &item.UnitPriceCents); err != nil {
			return nil, err
		}
		b.Items = append(b.Items, item)
	}
	return &b, nil
}

// ListByUser returns bookings for a user.
func (r *BookingRepo) ListByUser(ctx context.Context, userID string) ([]domain.Booking, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, user_id, event_id, status, total_cents, payment_id, refund_status, created_at FROM bookings WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookings []domain.Booking
	for rows.Next() {
		var b domain.Booking
		if err := rows.Scan(&b.ID, &b.UserID, &b.EventID, &b.Status, &b.TotalCents, &b.PaymentID, &b.RefundStatus, &b.CreatedAt); err != nil {
			return nil, err
		}
		bookings = append(bookings, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range bookings {
		itemRows, err := r.pool.Query(ctx, `SELECT id, booking_id, ticket_type_id, quantity, unit_price_cents FROM booking_items WHERE booking_id=$1`, bookings[i].ID)
		if err != nil {
			return nil, err
		}
		for itemRows.Next() {
			var item domain.BookingItem
			if err := itemRows.Scan(&item.ID, &item.BookingID, &item.TicketTypeID, &item.Quantity, &item.UnitPriceCents); err != nil {
				itemRows.Close()
				return nil, err
			}
			bookings[i].Items = append(bookings[i].Items, item)
		}
		itemRows.Close()
	}
	return bookings, nil
}

// UpdateStatus updates the status of a booking.
func (r *BookingRepo) UpdateStatus(ctx context.Context, bookingID, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE bookings SET status=$1 WHERE id=$2`, status, bookingID)
	return err
}

// CancelByEventID marks all confirmed bookings for an event as cancelled with refund pending.
func (r *BookingRepo) CancelByEventID(ctx context.Context, eventID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE bookings SET status='cancelled', refund_status='pending' WHERE event_id=$1 AND status='confirmed'`, eventID)
	return err
}

// ListByEventID returns all bookings for an event.
func (r *BookingRepo) ListByEventID(ctx context.Context, eventID string) ([]domain.Booking, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, user_id, event_id, status, total_cents, payment_id, refund_status, created_at FROM bookings WHERE event_id=$1 ORDER BY created_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookings []domain.Booking
	for rows.Next() {
		var b domain.Booking
		if err := rows.Scan(&b.ID, &b.UserID, &b.EventID, &b.Status, &b.TotalCents, &b.PaymentID, &b.RefundStatus, &b.CreatedAt); err != nil {
			return nil, err
		}
		bookings = append(bookings, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range bookings {
		itemRows, err := r.pool.Query(ctx, `SELECT id, booking_id, ticket_type_id, quantity, unit_price_cents FROM booking_items WHERE booking_id=$1`, bookings[i].ID)
		if err != nil {
			return nil, err
		}
		for itemRows.Next() {
			var item domain.BookingItem
			if err := itemRows.Scan(&item.ID, &item.BookingID, &item.TicketTypeID, &item.Quantity, &item.UnitPriceCents); err != nil {
				itemRows.Close()
				return nil, err
			}
			bookings[i].Items = append(bookings[i].Items, item)
		}
		itemRows.Close()
	}
	return bookings, nil
}

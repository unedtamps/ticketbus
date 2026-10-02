package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	shareddb "github.com/nedo/TicketSaas/pkg/db"
	"github.com/nedo/TicketSaas/service/ticketing/internal/domain"
)

// SeatCounter implements domain.SeatCounter using atomic PostgreSQL updates.
// ticket_types.available_seat is the authoritative availability counter;
// Redis is no longer involved in seat accounting.
type SeatCounter struct {
	db shareddb.DBTx
}

// NewSeatCounter creates a PostgreSQL-backed seat counter.
func NewSeatCounter(db shareddb.DBTx) *SeatCounter {
	return &SeatCounter{db: db}
}

// Init sets the initial available seats (event approval).
func (c *SeatCounter) Init(ctx context.Context, eventID, ticketTypeID string, total int) error {
	_, err := c.db.Exec(ctx, `
		UPDATE ticket_types SET available_seat = $1 WHERE id = $2`,
		total, ticketTypeID)
	return err
}

// Reserve atomically decrements the seat counter, failing if seats run out.
func (c *SeatCounter) Reserve(ctx context.Context, eventID, ticketTypeID string, qty int) error {
	tag, err := c.db.Exec(ctx, `
		UPDATE ticket_types
		SET available_seat = available_seat - $1
		WHERE id = $2 AND available_seat >= $1`,
		qty, ticketTypeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNoSeatsAvailable
	}
	return nil
}

// Release atomically restores seats, never exceeding the original quantity.
func (c *SeatCounter) Release(ctx context.Context, eventID, ticketTypeID string, qty int) error {
	_, err := c.db.Exec(ctx, `
		UPDATE ticket_types
		SET available_seat = LEAST(quantity, available_seat + $1)
		WHERE id = $2`,
		qty, ticketTypeID)
	return err
}

// Available returns the current available seats for a ticket type.
func (c *SeatCounter) Available(ctx context.Context, eventID, ticketTypeID string) (int, error) {
	var available int
	err := c.db.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ticketTypeID).
		Scan(&available)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrReservationNotFound
	}
	if err != nil {
		return 0, err
	}
	return available, nil
}

// SetPrice stores the authoritative price for a ticket type.
func (c *SeatCounter) SetPrice(ctx context.Context, eventID, ticketTypeID string, price int) error {
	_, err := c.db.Exec(
		ctx,
		`UPDATE ticket_types SET price_rupiah = $1 WHERE id = $2`,
		price,
		ticketTypeID,
	)
	return err
}

// GetPrice returns the stored authoritative price for a ticket type.
func (c *SeatCounter) GetPrice(ctx context.Context, eventID, ticketTypeID string) (int, error) {
	var price int
	err := c.db.QueryRow(ctx, `SELECT price_rupiah FROM ticket_types WHERE id = $1`, ticketTypeID).
		Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrReservationNotFound
	}
	if err != nil {
		return 0, err
	}
	return price, nil
}

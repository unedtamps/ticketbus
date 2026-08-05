package postgres

import (
	"context"

	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
)

// SeatReader reads live seat availability from PostgreSQL.
type SeatReader struct {
	db shareddb.DBTx
}

// NewSeatReader creates a PostgreSQL-backed seat availability reader.
func NewSeatReader(db shareddb.DBTx) *SeatReader {
	return &SeatReader{db: db}
}

// Available returns current available seats for a ticket type.
// Returns 0 if the ticket type is not found or availability is unset.
func (r *SeatReader) Available(ctx context.Context, eventID, ticketTypeID string) int {
	var available int
	err := r.db.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ticketTypeID).
		Scan(&available)
	if err != nil {
		return 0
	}
	return available
}

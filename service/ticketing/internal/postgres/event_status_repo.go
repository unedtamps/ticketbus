package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventStatusRepo struct {
	pool *pgxpool.Pool
}

func NewEventStatusRepo(pool *pgxpool.Pool) *EventStatusRepo {
	return &EventStatusRepo{pool: pool}
}

func (r *EventStatusRepo) IsPublished(ctx context.Context, eventID string) (bool, error) {
	var status string
	err := r.pool.QueryRow(ctx, `SELECT status FROM events WHERE id = $1`, eventID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status == "published", nil
}

// EventWindow returns the event status and its reservation window.
func (r *EventStatusRepo) EventWindow(ctx context.Context, eventID string) (string, time.Time, time.Time, error) {
	var (
		status  string
		startAt time.Time
		endAt   time.Time
	)
	err := r.pool.QueryRow(ctx,
		`SELECT status, start_at, end_at FROM events WHERE id = $1`, eventID).
		Scan(&status, &startAt, &endAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, time.Time{}, pgx.ErrNoRows
	}
	if err != nil {
		return "", time.Time{}, time.Time{}, err
	}
	return status, startAt, endAt, nil
}

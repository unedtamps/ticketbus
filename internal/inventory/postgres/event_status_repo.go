package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventStatusRepo struct {
	pool *pgxpool.Pool
}

func NewEventStatusRepo(pool *pgxpool.Pool) *EventStatusRepo {
	return &EventStatusRepo{pool: pool}
}

func (r *EventStatusRepo) Upsert(ctx context.Context, eventID, status string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO event_status_cache (event_id, status) VALUES ($1, $2)
		ON CONFLICT (event_id) DO UPDATE SET status = $2`, eventID, status)
	return err
}

func (r *EventStatusRepo) IsPublished(ctx context.Context, eventID string) (bool, error) {
	var status string
	err := r.pool.QueryRow(ctx, `SELECT status FROM event_status_cache WHERE event_id = $1`, eventID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status == "published", nil
}

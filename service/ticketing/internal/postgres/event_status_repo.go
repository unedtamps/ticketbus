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

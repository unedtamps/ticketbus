package db

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTx is the interface that both *pgxpool.Pool and pgx.Tx satisfy.
// Repos accept this so they can participate in transactions.
type DBTx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewPool creates a new PostgreSQL connection pool.
func NewPool(dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgx config: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = time.Minute * 30

	// Emits a CLIENT span per query (plus pool.acquire) so a slow handler can be
	// attributed to the statement that caused it. otelpgx captures the global
	// tracer provider by reference, so telemetry.Init must already have run.
	// With no provider installed this is a no-op.
	//
	// Query parameters are deliberately not recorded: the repositories are fully
	// parameterised, so db.query.text holds SQL text only and never leaks emails,
	// token hashes or keys into spans.
	config.ConnConfig.Tracer = otelpgx.NewTracer()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

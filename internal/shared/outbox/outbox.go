package outbox

import (
	"context"
	"encoding/json"

	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
)

// StoreInterface is the contract for outbox event storage (mockable in tests).
type StoreInterface interface {
	Insert(ctx context.Context, topic, key string, payload any) error
}

// Store manages the outbox table within a database connection.
type Store struct {
	db shareddb.DBTx
}

// NewStore creates a new outbox store.
func NewStore(db shareddb.DBTx) *Store {
	return &Store{db: db}
}

// Insert writes an event to the outbox table for later delivery.
func (s *Store) Insert(ctx context.Context, topic, key string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO outbox (topic, key, payload) VALUES ($1, $2, $3)`,
		topic, key, data,
	)
	return err
}

// NoopStore discards all events — useful for tests that don't test outbox delivery.
type NoopStore struct{}

func (NoopStore) Insert(ctx context.Context, topic, key string, payload any) error {
	return nil
}

package outbox

import (
	"context"
	"encoding/json"

	shareddb "github.com/nedo/TicketSaas/pkg/db"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
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
//
// The active trace context is stored alongside the payload. The worker publishes
// from its own background context well after this request has returned, so
// without this the published message would carry no trace and the consumer
// would start a fresh one.
func (s *Store) Insert(ctx context.Context, topic, key string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var traceContext *string
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if tc := carrier.Get("traceparent"); tc != "" {
		traceContext = &tc
	}

	_, err = s.db.Exec(ctx,
		`INSERT INTO outbox (topic, key, payload, trace_context) VALUES ($1, $2, $3, $4)`,
		topic, key, data, traceContext,
	)
	return err
}

// NoopStore discards all events — useful for tests that don't test outbox delivery.
type NoopStore struct{}

func (NoopStore) Insert(ctx context.Context, topic, key string, payload any) error {
	return nil
}

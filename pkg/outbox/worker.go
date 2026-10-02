package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	shareddb "github.com/nedo/TicketSaas/pkg/db"
	"github.com/nedo/TicketSaas/pkg/kafka"
)

// Worker polls the outbox table and publishes undelivered events to Kafka.
type Worker struct {
	store        *Store
	producer     *kafka.Producer
	logger       *slog.Logger
	concurrency  int
	pollInterval time.Duration
}

// NewWorker creates a new outbox worker.
func NewWorker(
	db shareddb.DBTx,
	producer *kafka.Producer,
	logger *slog.Logger,
	concurrency int,
	pollMs int,
) *Worker {
	if concurrency < 1 {
		concurrency = 1
	}
	pollInterval := time.Duration(pollMs) * time.Millisecond
	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}
	return &Worker{
		store:        NewStore(db),
		producer:     producer,
		logger:       logger,
		concurrency:  concurrency,
		pollInterval: pollInterval,
	}
}

type outboxRow struct {
	ID      int64
	Topic   string
	Key     string
	Payload []byte
}

// Run polls the outbox table and publishes undelivered events.
// Blocks until the context is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	rows, err := w.store.db.Query(ctx,
		`SELECT id, topic, key, payload FROM outbox WHERE delivered = false ORDER BY id LIMIT 100`)
	if err != nil {
		w.logger.Error("outbox worker query failed", "error", err)
		return
	}
	defer rows.Close()

	var batch []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.ID, &r.Topic, &r.Key, &r.Payload); err != nil {
			w.logger.Error("outbox worker scan failed", "error", err)
			continue
		}
		batch = append(batch, r)
	}
	rows.Close()
	if len(batch) == 0 {
		return
	}

	jobs := make(chan outboxRow, len(batch))
	var wg sync.WaitGroup
	for i := 0; i < w.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				if err := w.producer.Produce(
					ctx,
					row.Topic,
					row.Key,
					json.RawMessage(row.Payload),
				); err != nil {
					w.logger.Warn("outbox publish failed, will retry",
						"id", row.ID, "topic", row.Topic, "error", err)
					continue
				}
				if _, err := w.store.db.Exec(ctx,
					`UPDATE outbox SET delivered = true WHERE id = $1`, row.ID); err != nil {
					w.logger.Error("outbox mark delivered failed", "id", row.ID, "error", err)
				}
			}
		}()
	}

	for _, row := range batch {
		jobs <- row
	}
	close(jobs)
	wg.Wait()
}

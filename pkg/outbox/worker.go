package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	shareddb "github.com/nedo/TicketSaas/pkg/db"
	"github.com/nedo/TicketSaas/pkg/kafka"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// Worker polls the outbox table and publishes undelivered events to Kafka.
type Worker struct {
	store        *Store
	producer     *kafka.Producer
	logger       *slog.Logger
	tracer       trace.Tracer
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
		store: NewStore(db),
		// Captured after telemetry.Init has run in main, so the worker publishes
		// under the same provider as the rest of the process.
		tracer:       otel.Tracer("github.com/nedo/TicketSaas/pkg/outbox"),
		producer:     producer,
		logger:       logger,
		concurrency:  concurrency,
		pollInterval: pollInterval,
	}
}

type outboxRow struct {
	ID           int64
	Topic        string
	Key          string
	Payload      []byte
	TraceContext *string
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
		`SELECT id, topic, key, payload, trace_context FROM outbox WHERE delivered = false ORDER BY id LIMIT 100`)
	if err != nil {
		w.logger.Error("outbox worker query failed", "error", err)
		return
	}
	defer rows.Close()

	var batch []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.ID, &r.Topic, &r.Key, &r.Payload, &r.TraceContext); err != nil {
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
				if err := w.publish(ctx, row); err != nil {
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

// publish emits one outbox row, restoring the trace of the request that created
// it before starting the producer span. That restored parent is what keeps the
// consumer's span in the same trace across the Kafka hop.
func (w *Worker) publish(ctx context.Context, row outboxRow) error {
	publishCtx := ctx
	if row.TraceContext != nil && *row.TraceContext != "" {
		publishCtx = otel.GetTextMapPropagator().Extract(ctx,
			propagation.MapCarrier{"traceparent": *row.TraceContext})
	}

	publishCtx, span := w.tracer.Start(publishCtx, row.Topic+" publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKafka,
			semconv.MessagingDestinationName(row.Topic),
			semconv.MessagingKafkaMessageKey(row.Key),
		))
	defer span.End()

	return w.producer.Produce(publishCtx, row.Topic, row.Key, json.RawMessage(row.Payload))
}

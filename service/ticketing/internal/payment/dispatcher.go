package payment

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	shareddb "github.com/nedo/TicketSaas/pkg/db"
	sdto "github.com/nedo/TicketSaas/pkg/dto"
	"github.com/nedo/TicketSaas/service/ticketing/internal/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Dispatcher is the outbox REST dispatcher: it polls payment.initiate rows
// from the outbox table and delivers them to the payment service's internal
// API (POST /api/payments/internal) in the background. It is deliberately
// separate from the Kafka outbox worker (pkg/outbox): those rows never go to
// Kafka. Delivery is at-least-once; the payment service is idempotent per
// booking_id, so a redelivered row returns the existing transaction.
type Dispatcher struct {
	db     shareddb.DBTx
	client domain.PaymentClient
	logger *slog.Logger
	poll   time.Duration
	// failBackoff is slept after a batch in which any delivery failed, so a
	// down payment service is not hammered at poll-rate.
	failBackoff time.Duration
}

// NewDispatcher creates a new REST dispatcher.
func NewDispatcher(
	db shareddb.DBTx,
	client domain.PaymentClient,
	logger *slog.Logger,
	pollMs int,
) *Dispatcher {
	poll := time.Duration(pollMs) * time.Millisecond
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	return &Dispatcher{
		db:          db,
		client:      client,
		logger:      logger,
		poll:        poll,
		failBackoff: time.Second,
	}
}

type initiateRow struct {
	ID           int64
	Payload      []byte
	TraceContext *string
}

// Run polls the outbox for payment.initiate rows and delivers them until the
// context is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !d.processBatch(ctx) {
				// At least one delivery failed — back off before retrying so
				// an unreachable payment service is not polled at full rate.
				select {
				case <-ctx.Done():
					return
				case <-time.After(d.failBackoff):
				}
			}
		}
	}
}

// processBatch delivers every pending payment.initiate row. It reports
// whether every delivery succeeded.
func (d *Dispatcher) processBatch(ctx context.Context) bool {
	rows, err := d.db.Query(
		ctx,
		`SELECT id, payload, trace_context FROM outbox WHERE delivered = false AND topic = $1 ORDER BY id LIMIT 50`,
		sdto.TopicPaymentInitiate,
	)
	if err != nil {
		d.logger.Error("rest dispatcher query failed", "error", err)
		return false
	}
	defer rows.Close()

	var batch []initiateRow
	for rows.Next() {
		var r initiateRow
		if err := rows.Scan(&r.ID, &r.Payload, &r.TraceContext); err != nil {
			d.logger.Error("rest dispatcher scan failed", "error", err)
			continue
		}
		batch = append(batch, r)
	}
	rows.Close()
	if len(batch) == 0 {
		return true
	}

	allOK := true
	for _, row := range batch {
		if err := d.deliver(ctx, row); err != nil {
			// Row stays undelivered and is retried on the next tick.
			d.logger.Warn("rest dispatcher delivery failed, will retry",
				"id", row.ID, "topic", sdto.TopicPaymentInitiate, "error", err)
			allOK = false
			continue
		}
		if _, err := d.db.Exec(ctx,
			`UPDATE outbox SET delivered = true WHERE id = $1`, row.ID); err != nil {
			d.logger.Error("rest dispatcher mark delivered failed", "id", row.ID, "error", err)
			allOK = false
		}
	}
	return allOK
}

// deliver performs one REST call for a single outbox row, restoring the
// trace context of the reserve request that created it so the payment
// service's span nests under the original trace across the hop.
func (d *Dispatcher) deliver(ctx context.Context, row initiateRow) error {
	var req sdto.PaymentInitiateRequested
	if err := json.Unmarshal(row.Payload, &req); err != nil {
		// A malformed payload would poison the batch forever; keep retrying
		// so it stays visible in logs.
		return err
	}
	deliverCtx := ctx
	if row.TraceContext != nil && *row.TraceContext != "" {
		deliverCtx = otel.GetTextMapPropagator().Extract(ctx,
			propagation.MapCarrier{"traceparent": *row.TraceContext})
	}
	return d.handle(deliverCtx, req)
}

// handle maps an outbox payload onto the payment client call. Kept separate
// from SQL so it is unit-testable.
func (d *Dispatcher) handle(ctx context.Context, req sdto.PaymentInitiateRequested) error {
	return d.client.InitiateTxnForBooking(
		ctx,
		req.BookingID,
		req.EventID,
		req.UserID,
		req.Email,
		req.AmountRupiah,
		req.ExpiresAt,
	)
}

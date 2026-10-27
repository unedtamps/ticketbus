// Package payout delivers disbursement jobs written by the payout consumer to
// the payment gateway. It is the payment-side counterpart of ticketing's REST
// dispatcher: jobs are outbox rows (topic payment.disbursement) and never go to
// Kafka.
package payout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	shareddb "github.com/unedtamps/ticketbus/pkg/db"
	"github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Dispatcher polls undelivered payment.disbursement rows and calls the payment
// processor. Delivery is at-least-once; the gateway idempotency key makes a
// redelivered job safe.
type Dispatcher struct {
	db          shareddb.DBTx
	repo        domain.PayoutRepository
	processor   domain.PaymentProcessor
	logger      *slog.Logger
	poll        time.Duration
	failBackoff time.Duration
}

// NewDispatcher creates a new payout dispatcher.
func NewDispatcher(
	db shareddb.DBTx,
	repo domain.PayoutRepository,
	processor domain.PaymentProcessor,
	logger *slog.Logger,
	pollMs int,
) *Dispatcher {
	poll := time.Duration(pollMs) * time.Millisecond
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	return &Dispatcher{
		db:          db,
		repo:        repo,
		processor:   processor,
		logger:      logger,
		poll:        poll,
		failBackoff: time.Second,
	}
}

type disbursementRow struct {
	ID           int64
	Payload      []byte
	TraceContext *string
}

// Run polls for pending payout jobs until the context is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !d.processBatch(ctx) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(d.failBackoff):
				}
			}
		}
	}
}

func (d *Dispatcher) processBatch(ctx context.Context) bool {
	rows, err := d.db.Query(ctx,
		`SELECT id, payload, trace_context FROM outbox
		 WHERE delivered = false AND topic = $1 ORDER BY id LIMIT 20`,
		dto.TopicPaymentDisbursement)
	if err != nil {
		d.logger.Error("payout dispatcher query failed", "error", err)
		return false
	}
	defer rows.Close()

	var batch []disbursementRow
	for rows.Next() {
		var r disbursementRow
		if err := rows.Scan(&r.ID, &r.Payload, &r.TraceContext); err != nil {
			d.logger.Error("payout dispatcher scan failed", "error", err)
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
			d.logger.Warn("payout delivery failed, will retry",
				"id", row.ID, "error", err)
			allOK = false
			continue
		}
		if _, err := d.db.Exec(ctx,
			`UPDATE outbox SET delivered = true WHERE id = $1`, row.ID); err != nil {
			d.logger.Error("payout dispatcher mark delivered failed", "id", row.ID, "error", err)
			allOK = false
		}
	}
	return allOK
}

func (d *Dispatcher) deliver(ctx context.Context, row disbursementRow) error {
	var job dto.PaymentDisbursementRequested
	if err := json.Unmarshal(row.Payload, &job); err != nil {
		return fmt.Errorf("invalid disbursement payload: %w", err)
	}

	deliverCtx := ctx
	if row.TraceContext != nil && *row.TraceContext != "" {
		deliverCtx = otel.GetTextMapPropagator().Extract(ctx,
			propagation.MapCarrier{"traceparent": *row.TraceContext})
	}

	result, err := d.processor.CreatePayout(deliverCtx, domain.PayoutRequest{
		ReferenceID:       job.PayoutID,
		IdempotencyKey:    fmt.Sprintf("payout-%s-%d", job.PayoutID, job.Attempt),
		AmountRupiah:      job.AmountRupiah,
		Currency:          job.Currency,
		BankCode:          job.BankCode,
		AccountNumber:     job.BankAccountNumber,
		AccountHolderName: job.BankAccountHolderName,
		Description:       job.Description,
	})
	if err != nil {
		switch {
		case domain.IsAuthProcessorError(err):
			// The gateway rejected the credentials, not the payout. The job
			// stays undelivered and is retried, so the money is not lost
			// because of a missing API key scope.
			d.logger.Error("payout gateway auth rejected — check the Xendit key has the MONEY-OUT permission",
				"event_id", job.EventID, "payout_id", job.PayoutID,
				"attempt", job.Attempt, "error", err)
			return err

		case domain.IsPermanentProcessorError(err):
			d.logger.Warn("payout rejected by gateway",
				"event_id", job.EventID, "payout_id", job.PayoutID,
				"attempt", job.Attempt, "error", err)
			// Record the failure and stop retrying this job. Complete is
			// idempotent, so a partial failure can be retried safely.
			return d.repo.Complete(ctx, job.EventID, job.Attempt,
				domain.PayoutStatusFailed, job.AmountRupiah, "",
				domain.SanitizeProcessorError(err))
		}
		return err
	}

	if err := d.repo.Complete(ctx, job.EventID, job.Attempt,
		domain.PayoutStatusPaidOut, job.AmountRupiah, result.ProviderRef, ""); err != nil {
		return err
	}
	d.logger.Info("payout paid out",
		"event_id", job.EventID,
		"payout_id", job.PayoutID,
		"provider_ref", result.ProviderRef,
		"amount_rupiah", job.AmountRupiah,
	)
	return nil
}

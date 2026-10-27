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
//
// A second loop reconciles payouts that were accepted by the gateway but never
// confirmed: it asks the gateway for their current status and finalizes the
// ones that reached a terminal state. It is the safety net for payout webhooks
// that never arrived.
type Dispatcher struct {
	db          shareddb.DBTx
	repo        domain.PayoutRepository
	processor   domain.PaymentProcessor
	logger      *slog.Logger
	poll        time.Duration
	failBackoff time.Duration
	reconcile   time.Duration
	staleAfter  time.Duration
}

// NewDispatcher creates a new payout dispatcher. reconcileSec is how often
// payouts already sent to the gateway are polled for their final status;
// <= 0 disables reconciliation polling.
func NewDispatcher(
	db shareddb.DBTx,
	repo domain.PayoutRepository,
	processor domain.PaymentProcessor,
	logger *slog.Logger,
	pollMs int,
	reconcileSec int,
) *Dispatcher {
	poll := time.Duration(pollMs) * time.Millisecond
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	d := &Dispatcher{
		db:          db,
		repo:        repo,
		processor:   processor,
		logger:      logger,
		poll:        poll,
		failBackoff: time.Second,
	}
	if reconcileSec > 0 {
		d.reconcile = time.Duration(reconcileSec) * time.Second
		// Payouts younger than this are left to the webhook; polling starts
		// once a confirmation is overdue.
		d.staleAfter = 2 * d.reconcile
	}
	return d
}

type disbursementRow struct {
	ID           int64
	Payload      []byte
	TraceContext *string
}

// Run polls for pending payout jobs until the context is cancelled, and (when
// enabled) reconciles sent-but-unconfirmed payouts.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.poll)
	defer ticker.Stop()

	var reconcileC <-chan time.Time
	if d.reconcile > 0 {
		reconciler := time.NewTicker(d.reconcile)
		defer reconciler.Stop()
		reconcileC = reconciler.C
	}

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
		case <-reconcileC:
			d.reconcilePending(ctx)
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

	// The gateway accepted the disbursement. Some rails settle synchronously
	// (COMPLETED on arrival); the rest stay pending until an async
	// confirmation arrives — the gateway webhook or the reconciliation poll.
	// The gateway status is final only when it maps to a terminal ledger
	// status; anything else keeps waiting.
	status, reason, terminal := domain.PayoutConfirmation(result.Status, result.FailureCode)
	if terminal {
		if err := d.repo.Complete(ctx, job.EventID, job.Attempt,
			status, job.AmountRupiah, result.ProviderRef, reason); err != nil {
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

	if err := d.repo.AttachProviderRef(ctx, job.EventID, job.Attempt, result.ProviderRef); err != nil {
		return err
	}
	d.logger.Info("payout submitted, awaiting gateway confirmation",
		"event_id", job.EventID,
		"payout_id", job.PayoutID,
		"provider_ref", result.ProviderRef,
		"gateway_status", result.Status,
	)
	return nil
}

// reconcileBatch bounds one reconciliation round.
const reconcileBatch = 50

// reconcilePending asks the gateway for the current status of payouts that
// were accepted but never confirmed, and finalizes the ones that reached a
// terminal state. Complete's pending-only guard makes this idempotent against
// a webhook that lands concurrently.
func (d *Dispatcher) reconcilePending(ctx context.Context) {
	pending, err := d.repo.ListPendingDispatched(ctx, time.Now().Add(-d.staleAfter), reconcileBatch)
	if err != nil {
		d.logger.Error("payout reconcile query failed", "error", err)
		return
	}
	for _, p := range pending {
		result, err := d.processor.GetPayout(ctx, p.ProviderRef)
		if err != nil {
			d.logger.Error("payout reconcile fetch failed",
				"event_id", p.EventID, "payout_id", p.ID,
				"provider_ref", p.ProviderRef, "error", err)
			continue
		}
		status, reason, terminal := domain.PayoutConfirmation(result.Status, result.FailureCode)
		if !terminal {
			continue
		}
		if err := d.repo.Complete(ctx, p.EventID, p.Attempt,
			status, p.AmountRupiah, p.ProviderRef, reason); err != nil {
			d.logger.Error("payout reconcile complete failed",
				"event_id", p.EventID, "attempt", p.Attempt, "error", err)
			continue
		}
		d.logger.Info("payout confirmed via reconciliation",
			"event_id", p.EventID,
			"payout_id", p.ID,
			"gateway_status", result.Status,
			"status", status,
		)
	}
}

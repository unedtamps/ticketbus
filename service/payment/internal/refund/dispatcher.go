// Package refund executes customer refunds through the Payouts API. The flow
// mirrors the payout dispatcher: a sweep claims pending refunds that have a
// registered destination (claim + refund.disbursement outbox row in one
// transaction), a delivery loop sends the job to the gateway, and a
// reconciliation poll settles payouts whose webhook never arrived.
package refund

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

const maxRefundAge = 25 * 24 * time.Hour

// Dispatcher claims pending refunds and pays them out through the processor.
// Delivery is at-least-once; the gateway idempotency key makes a redelivered
// job safe.
type Dispatcher struct {
	db          shareddb.DBTx
	refunds     domain.RefundRepository
	processor   domain.PaymentProcessor
	logger      *slog.Logger
	sweep       time.Duration
	poll        time.Duration
	failBackoff time.Duration
	reconcile   time.Duration
	staleAfter  time.Duration
}

// NewDispatcher creates the refund executor. sweepSec is how often pending
// refunds with a registered destination are claimed; pollMs drives the outbox
// delivery loop; reconcileSec polls sent-but-unconfirmed payouts for their
// final status (<= 0 disables reconciliation).
func NewDispatcher(
	db shareddb.DBTx,
	refunds domain.RefundRepository,
	processor domain.PaymentProcessor,
	logger *slog.Logger,
	sweepSec int,
	reconcileSec int,
) *Dispatcher {
	sweep := time.Duration(sweepSec) * time.Second
	if sweep <= 0 {
		sweep = 60 * time.Second
	}
	poll := 200 * time.Millisecond
	d := &Dispatcher{
		db:          db,
		refunds:     refunds,
		processor:   processor,
		logger:      logger,
		sweep:       sweep,
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

// Run starts the sweep, the outbox delivery loop, and (when enabled) the
// reconciliation poll. Blocks until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	sweeper := time.NewTicker(d.sweep)
	defer sweeper.Stop()
	poller := time.NewTicker(d.poll)
	defer poller.Stop()

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
		case <-sweeper.C:
			if err := d.sweepPending(ctx); err != nil {
				d.logger.Error("refund sweep failed", "error", err)
			}
		case <-poller.C:
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

// sweepPending claims pending refunds whose customer registered a destination:
// each claim moves the refund to processing and enqueues its payout job in one
// transaction. Refunds without a destination stay pending until the customer
// registers one (the cancellation email carries the link), and refunds past
// the channel refund windows stay pending for admin handling.
func (d *Dispatcher) sweepPending(ctx context.Context) error {
	jobs, err := d.refunds.ListPendingWithDestination(ctx, 50)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		job := dto.RefundDisbursementRequested{
			RefundID:          j.RefundID,
			Attempt:           j.Attempt,
			AmountRupiah:      j.AmountRupiah,
			Currency:          j.Currency,
			ChannelCode:       j.ChannelCode,
			AccountNumber:     j.AccountNumber,
			AccountHolderName: j.AccountHolderName,
			Description:       "TicketBus refund",
		}
		applied, err := d.refunds.ClaimForDisbursement(ctx, j.RefundID, job)
		if err != nil {
			return err
		}
		if applied {
			d.logger.Info("refund claimed for disbursement",
				"refund_id", j.RefundID, "amount_rupiah", j.AmountRupiah)
		}
	}
	return nil
}

type disbursementRow struct {
	ID           int64
	Payload      []byte
	TraceContext *string
}

// processBatch delivers undelivered refund.disbursement outbox rows. Returns
// whether the whole batch was handled without retry-worthy failures.
func (d *Dispatcher) processBatch(ctx context.Context) bool {
	rows, err := d.db.Query(ctx,
		`SELECT id, payload, trace_context FROM outbox
		 WHERE delivered = false AND topic = $1 ORDER BY id LIMIT 20`,
		dto.TopicRefundDisbursement)
	if err != nil {
		d.logger.Error("refund dispatcher query failed", "error", err)
		return false
	}

	var batch []disbursementRow
	for rows.Next() {
		var r disbursementRow
		if err := rows.Scan(&r.ID, &r.Payload, &r.TraceContext); err != nil {
			d.logger.Error("refund dispatcher scan failed", "error", err)
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
			d.logger.Warn("refund delivery failed, will retry", "id", row.ID, "error", err)
			allOK = false
			continue
		}
		if _, err := d.db.Exec(ctx,
			`UPDATE outbox SET delivered = true WHERE id = $1`, row.ID); err != nil {
			d.logger.Error("refund dispatcher mark delivered failed", "id", row.ID, "error", err)
			allOK = false
		}
	}
	return allOK
}

func (d *Dispatcher) deliver(ctx context.Context, row disbursementRow) error {
	var job dto.RefundDisbursementRequested
	if err := json.Unmarshal(row.Payload, &job); err != nil {
		return fmt.Errorf("invalid refund disbursement payload: %w", err)
	}

	deliverCtx := ctx
	if row.TraceContext != nil && *row.TraceContext != "" {
		deliverCtx = otel.GetTextMapPropagator().Extract(ctx,
			propagation.MapCarrier{"traceparent": *row.TraceContext})
	}

	result, err := d.processor.CreatePayout(deliverCtx, domain.PayoutRequest{
		ReferenceID: job.RefundID,
		// The attempt is part of the key: an admin retry must submit a fresh
		// payout rather than replay the gateway's stored failed response.
		IdempotencyKey:    fmt.Sprintf("refund-%s-%d", job.RefundID, job.Attempt),
		AmountRupiah:      job.AmountRupiah,
		Currency:          job.Currency,
		BankCode:          job.ChannelCode,
		AccountNumber:     job.AccountNumber,
		AccountHolderName: job.AccountHolderName,
		Description:       job.Description,
	})
	if err != nil {
		switch {
		case domain.IsAuthProcessorError(err):
			// The gateway rejected the credentials, not the refund. The job
			// stays undelivered and is retried.
			d.logger.Error(
				"refund gateway auth rejected — check the Xendit key has the MONEY-OUT permission",
				"refund_id", job.RefundID, "error", err,
			)
			return err

		case domain.IsPermanentProcessorError(err):
			// The gateway rejected the destination itself, so a retry with the
			// same details would fail identically. Record the failure with a
			// customer-facing reason; the admin table can retry once the
			// customer corrects their account.
			d.logger.Warn("refund rejected by gateway",
				"refund_id", job.RefundID, "error", err)
			return d.refunds.Complete(ctx, job.RefundID,
				domain.RefundFailed, "",
				"Detail rekening refund tidak diterima oleh penyedia pembayaran. Periksa kembali nomor dan nama pemilik rekening.")
		}
		return err
	}

	// Some rails settle synchronously; the rest wait for the gateway webhook
	// or the reconciliation poll. Only terminal gateway statuses finalize the
	// refund.
	refundStatus, reason, terminal := domain.RefundOutcome(result.Status, result.FailureCode)
	if terminal {
		if err := d.refunds.Complete(ctx, job.RefundID,
			refundStatus, result.ProviderRef, reason); err != nil {
			return err
		}
		d.logger.Info("refund paid out",
			"refund_id", job.RefundID,
			"provider_ref", result.ProviderRef,
			"amount_rupiah", job.AmountRupiah,
			"status", refundStatus,
		)
		return nil
	}

	if err := d.refunds.AttachProviderRef(ctx, job.RefundID, result.ProviderRef); err != nil {
		return err
	}
	d.logger.Info("refund payout submitted, awaiting gateway confirmation",
		"refund_id", job.RefundID,
		"provider_ref", result.ProviderRef,
		"gateway_status", result.Status,
	)
	return nil
}

// reconcileBatch bounds one reconciliation round.
const reconcileBatch = 50

// reconcilePending asks the gateway for the current status of refund payouts
// that were accepted but never confirmed. Complete's processing-only guard
// makes this idempotent against a webhook that lands concurrently.
func (d *Dispatcher) reconcilePending(ctx context.Context) {
	pending, err := d.refunds.ListDispatchedStale(ctx, time.Now().Add(-d.staleAfter), reconcileBatch)
	if err != nil {
		d.logger.Error("refund reconcile query failed", "error", err)
		return
	}
	for _, r := range pending {
		result, err := d.processor.GetPayout(ctx, r.ProviderRef)
		if err != nil {
			d.logger.Error("refund reconcile fetch failed",
				"refund_id", r.ID, "provider_ref", r.ProviderRef, "error", err)
			continue
		}
		status, reason, terminal := domain.RefundOutcome(result.Status, result.FailureCode)
		if !terminal {
			continue
		}
		if err := d.refunds.Complete(ctx, r.ID, status, r.ProviderRef, reason); err != nil {
			d.logger.Error("refund reconcile complete failed",
				"refund_id", r.ID, "error", err)
			continue
		}
		d.logger.Info("refund confirmed via reconciliation",
			"refund_id", r.ID,
			"gateway_status", result.Status,
			"status", status,
		)
	}
}

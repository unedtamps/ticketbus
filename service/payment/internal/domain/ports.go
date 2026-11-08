package domain

import (
	"context"
	"time"

	"github.com/unedtamps/ticketbus/pkg/dto"
)

// TransactionRepository defines the contract for transaction persistence.
type TransactionRepository interface {
	Create(ctx context.Context, txn *Transaction) error
	FindByID(ctx context.Context, id string) (*Transaction, error)
	FindByBookingID(ctx context.Context, bookingID string) (*Transaction, error)
	UpdateStatus(ctx context.Context, id, status, providerRef string) error
	// TransitionIfActive transitions the transaction only if it is still in an
	// active (non-terminal) status, guarding concurrent paths (webhook vs
	// expiry poll) atomically.
	TransitionIfActive(ctx context.Context, id, status, providerRef string) (bool, error)
	// CompleteIfActive transitions an active transaction to completed and
	// writes the payment.completed outbox row in one database transaction, so
	// a customer's payment can never settle without ticketing being told.
	// Reports whether the transition was applied.
	CompleteIfActive(ctx context.Context, txn *Transaction) (bool, error)
	// ExpireIfActive transitions an active transaction to expired and writes
	// the payment.expired outbox row in one database transaction — a lost row
	// after a standalone transition would leave the booking pending and its
	// seats held forever. Reports whether the transition was applied.
	ExpireIfActive(ctx context.Context, txn *Transaction, reason string) (bool, error)
	// TransitionIfInitiated transitions the transaction only while it is still
	// initiated (the initiated → pending step of ProcessPayment), so a
	// concurrent request cannot overwrite an existing session reference.
	TransitionIfInitiated(ctx context.Context, id, status, providerRef string) (bool, error)
	// UpdateSession stores the gateway payment session details after the
	// customer initiates a payment for the booking.
	UpdateSession(ctx context.Context, id, providerRef, paymentLinkURL string) error
	UpdateRefundStatus(ctx context.Context, id, refundStatus string) error
	ListByUser(ctx context.Context, userID string) ([]Transaction, error)
	ListByEventID(ctx context.Context, eventID string) ([]Transaction, error)
	// ListSweepableExpired returns transactions past their deadline that the
	// gateway will never report on its own: never-initiated ones immediately,
	// and initiated ones only after pendingGrace (in case a webhook was lost).
	ListSweepableExpired(ctx context.Context, now time.Time, pendingGrace time.Duration, limit int) ([]Transaction, error)
}

// RefundRepository defines the contract for refund request persistence.
type RefundRepository interface {
	Create(ctx context.Context, refund *RefundRequest) error
	// ListPendingWithDestination returns pending refunds whose customer has a
	// registered refund destination, joined with the destination details — the
	// executor's claim queue.
	ListPendingWithDestination(ctx context.Context, limit int) ([]RefundSweep, error)
	// ClaimForDisbursement atomically moves one refund from pending to
	// processing and writes the refund.disbursement outbox row in the same
	// transaction, so a claimed refund is always followed by a payout job.
	// Reports whether the transition was applied.
	ClaimForDisbursement(ctx context.Context, refundID string, job dto.RefundDisbursementRequested) (bool, error)
	// AttachProviderRef stores the gateway payout id on a processing refund
	// after the gateway accepted the disbursement.
	AttachProviderRef(ctx context.Context, refundID, providerRef string) error
	// Complete settles a processing refund (succeeded/failed) and writes the
	// refund.completed outbox row in the same transaction, so the refund can
	// never settle without the rest of the system hearing about it. Also
	// mirrors the outcome onto transactions.refund_status. Idempotent via a
	// processing-only guard.
	Complete(ctx context.Context, refundID, status, providerRef, failureReason string) error
	// FindByProviderRef locates the refund that owns a gateway payout id —
	// payout webhooks address refunds by their gateway payout id.
	FindByProviderRef(ctx context.Context, providerRef string) (*RefundRequest, error)
	// ListDispatchedStale returns processing refunds already sent to the
	// gateway (provider_ref set) and untouched since olderThan — the
	// reconciliation poll's work queue.
	ListDispatchedStale(ctx context.Context, olderThan time.Time, limit int) ([]*RefundRequest, error)
	// ListAll returns refunds for the admin refund table, newest first.
	ListAll(ctx context.Context, status string, limit, offset int) ([]*RefundRequest, error)
	// ListByCustomer returns the caller's own refunds, newest first, with the
	// destination the payout went to. This is the customer's single source of
	// truth for refund state — no mirror in ticketing.
	ListByCustomer(ctx context.Context, userID string, limit, offset int) ([]*CustomerRefund, error)
	// ResetForRetry returns a failed or stuck processing refund to the pending
	// queue and bumps its attempt counter, so the Payouts idempotency key
	// produces a fresh gateway payout instead of replaying the failed one.
	ResetForRetry(ctx context.Context, refundID string) (bool, error)
}

// RefundDestinationRepository defines persistence for customer refund
// destinations.
type RefundDestinationRepository interface {
	Upsert(ctx context.Context, dest *RefundDestination) error
	FindByUserID(ctx context.Context, userID string) (*RefundDestination, error)
}

// RefundSweep is one pending refund joined with its customer's registered
// destination — the executor's claim candidate.
type RefundSweep struct {
	RefundID          string
	AmountRupiah      int64
	Currency          string
	ChannelCode       string
	AccountNumber     string
	AccountHolderName string
	// Attempt is the retry counter: the Payouts idempotency key includes it so
	// an admin retry submits a fresh payout instead of replaying the failed one.
	Attempt int
}

// SessionResult is the normalized outcome of creating or fetching a gateway
// payment session.
type SessionResult struct {
	ProviderRef    string    `json:"provider_ref"`
	PaymentLinkURL string    `json:"payment_link_url"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// PaymentProcessor defines the contract for external payment processing.
type PaymentProcessor interface {
	// CreateSession creates a payment session at the gateway. refID is the
	// caller's idempotency key (the booking ID); email is the customer email.
	CreateSession(ctx context.Context, refID string, amountRupiah int, currency string, expiresAt time.Time, allowedChannels []string, email string) (*SessionResult, error)
	// CancelSession closes/expires an existing payment session.
	CancelSession(ctx context.Context, providerRef string) error
	// GetSession fetches a payment session.
	GetSession(ctx context.Context, providerRef string) (*SessionResult, error)
	// CreatePayout sends the organizer's earnings to their bank account.
	CreatePayout(ctx context.Context, req PayoutRequest) (*PayoutResult, error)
	// GetPayout fetches the gateway's current state of a payout created
	// earlier (the reconciliation poll's source of truth).
	GetPayout(ctx context.Context, payoutID string) (*PayoutResult, error)
}

// PayoutRepository defines the persistence contract for event payouts.
type PayoutRepository interface {
	// Prepare atomically creates the payout ledger row for one attempt, marks
	// the event's eligible transactions pending, and writes either the gateway
	// disbursement job or (when there are no earnings) a failed payout.completed
	// event — all in one transaction.
	//
	// A repeated delivery of the same attempt is a no-op.
	Prepare(ctx context.Context, req dto.PayoutRequested) (*PreparedPayout, error)
	// Complete applies the gateway outcome to one attempt of the ledger and its
	// transactions and emits payout.completed, in one transaction.
	Complete(ctx context.Context, eventID string, attempt int, status string, amountRupiah int64, providerRef, reason string) error
	// AttachProviderRef stores the gateway payout id on a still-pending attempt
	// after the gateway accepted the disbursement: the payout now waits for an
	// async confirmation (gateway webhook or reconciliation poll).
	AttachProviderRef(ctx context.Context, eventID string, attempt int, providerRef string) error
	// FindByProviderRef locates the payout attempt that owns a gateway payout
	// id — gateway callbacks address payouts by their gateway id.
	FindByProviderRef(ctx context.Context, providerRef string) (*Payout, error)
	// ListPendingDispatched returns pending payouts that were already sent to
	// the gateway (provider_ref set) and untouched since olderThan — the
	// reconciliation poll's work queue.
	ListPendingDispatched(ctx context.Context, olderThan time.Time, limit int) ([]*Payout, error)
	// FindLatestByEvent returns the most recent attempt of an event's payout
	// ledger (dev-only mock confirmation endpoint uses it to resolve the
	// gateway reference without knowing it upfront).
	FindLatestByEvent(ctx context.Context, eventID string) (*Payout, error)
	FindByEvent(ctx context.Context, eventID string, attempt int) (*Payout, error)
}

// PreparedPayout is the outcome of Prepare.
type PreparedPayout struct {
	Payout       *Payout
	AmountRupiah int64
	// Duplicate is true when an existing non-failed payout made this request a
	// no-op (Kafka redelivery).
	Duplicate bool
	// NoEarnings is true when the event had no payable transactions; the payout
	// is already recorded as failed.
	NoEarnings bool
}

// EventConsumer defines the contract for consuming reservation events.
type EventConsumer interface {
	OnEventCancelled(ctx context.Context, fn func(ctx context.Context, eventID string) error)
	OnPayoutRequested(ctx context.Context, fn func(ctx context.Context, evt dto.PayoutRequested) error)
	Start(ctx context.Context) error
	Close() error
}

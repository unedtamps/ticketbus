package domain

import (
	"time"

	"github.com/unedtamps/ticketbus/pkg/dto"
)

// OrganizerPayoutAccount is the bank destination an organizer uses for event
// payouts. It is stored in the ticketing database and can be updated at any
// time; each payout attempt snapshots it.
type OrganizerPayoutAccount struct {
	OrganizerID           string    `json:"organizer_id"`
	BankCode              string    `json:"bank_code"`
	BankAccountNumber     string    `json:"bank_account_number"`
	BankAccountHolderName string    `json:"bank_account_holder_name"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// EventPayoutAttempt is one payout attempt for an event. An event may have
// several attempts: a retry after a failure, or a top-up that collects
// transactions which completed after an earlier payout. The bank details are
// snapshotted so later account changes never rewrite history.
type EventPayoutAttempt struct {
	ID                    string           `json:"id"`
	EventID               string           `json:"event_id"`
	Attempt               int              `json:"attempt"`
	OrganizerID           string           `json:"organizer_id"`
	BankCode              string           `json:"bank_code"`
	BankAccountNumber     string           `json:"bank_account_number"`
	BankAccountHolderName string           `json:"bank_account_holder_name"`
	Status                dto.PayoutStatus `json:"status"`
	AmountRupiah          int64            `json:"amount_rupiah"`
	Provider              string           `json:"provider"`
	ProviderRef           string           `json:"provider_ref,omitempty"`
	FailureReason         string           `json:"failure_reason,omitempty"`
	RequestedAt           time.Time        `json:"requested_at"`
	CompletedAt           *time.Time       `json:"completed_at,omitempty"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
}

// EventPayoutSummary is the event-level payout state derived from its
// attempts: the cumulative amount paid out and the aggregate status.
//
// The status rule keeps a successful payout visible: if any attempt paid out,
// the event counts as paid_out even when a later attempt failed (a retry or a
// top-up with nothing to collect).
type EventPayoutSummary struct {
	EventID       string               `json:"event_id"`
	Status        dto.PayoutStatus     `json:"status"`
	AmountRupiah  int64                `json:"amount_rupiah"`
	Attempts      int                  `json:"attempts"`
	AttemptsList  []EventPayoutAttempt `json:"attempts_list"`
	ProviderRef   string               `json:"provider_ref,omitempty"`
	FailureReason string               `json:"failure_reason,omitempty"`
	RequestedAt   *time.Time           `json:"requested_at,omitempty"`
	CompletedAt   *time.Time           `json:"completed_at,omitempty"`
}

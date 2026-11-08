package domain

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrAlreadyProcessed    = errors.New("transaction already processed")
	ErrTransactionExpired  = errors.New("booking has expired")
	ErrUnsupportedChannel  = errors.New("unsupported payment channel")
	// ErrGatewayUnavailable wraps errors from the payment provider (network,
	// validation, or provider-side failures). Callers surface it as 502.
	ErrGatewayUnavailable = errors.New("payment gateway unavailable")
	// ErrNoRows is returned by repositories when no row matches.
	ErrNoRows = pgx.ErrNoRows
	// ErrPayoutNotDispatched means the payout attempt exists but has not been
	// sent to the gateway yet, so it has no gateway reference to confirm.
	ErrPayoutNotDispatched = errors.New("payout not dispatched")
	// ErrInvalidInput rejects a malformed caller-supplied payload.
	ErrInvalidInput = errors.New("invalid input")
	// ErrInvalidWebhook marks a malformed webhook body (bad JSON, missing
	// reference). Retrying it can never succeed, so the caller answers 400
	// instead of the 500 that asks the provider to redeliver.
	ErrInvalidWebhook = errors.New("invalid webhook payload")
)

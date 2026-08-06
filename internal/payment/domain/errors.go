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
)

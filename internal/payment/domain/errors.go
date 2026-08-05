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
	// ErrNoRows is returned by repositories when no row matches.
	ErrNoRows = pgx.ErrNoRows
)

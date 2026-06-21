package domain

import "errors"

var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrAlreadyProcessed    = errors.New("transaction already processed")
)

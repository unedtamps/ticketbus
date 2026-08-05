package handler

// TransactionResponse is the public transaction data.
type TransactionResponse struct {
	ID           string `json:"id"`
	BookingID    string `json:"booking_id"`
	EventID      string `json:"event_id"`
	AmountCents  int    `json:"amount_cents"`
	Currency     string `json:"currency"`
	Status       string `json:"status"`
	RefundStatus string `json:"refund_status,omitempty"`
	CreatedAt    string `json:"created_at"`
}

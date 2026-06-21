package handler

// TransactionResponse is the public transaction data.
type TransactionResponse struct {
	ID          string `json:"id"`
	BookingID   string `json:"booking_id"`
	AmountCents int    `json:"amount_cents"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

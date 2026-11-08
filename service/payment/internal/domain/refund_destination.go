package domain

import "time"

// RefundDestination is the customer's registered payout target for refunds.
// VA and retail-outlet payments cannot refund to their source, so every
// refund is paid out to this account instead. Reusable across events.
type RefundDestination struct {
	UserID            string    `json:"user_id"`
	ChannelCode       string    `json:"channel_code"`
	AccountNumber     string    `json:"account_number"`
	AccountHolderName string    `json:"account_holder_name"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

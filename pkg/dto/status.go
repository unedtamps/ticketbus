package dto

// EventStatus represents the lifecycle state of an event.
type EventStatus string

const (
	EventStatusDraft     EventStatus = "draft"
	EventStatusPending   EventStatus = "pending"
	EventStatusPublished EventStatus = "published"
	EventStatusRejected  EventStatus = "rejected"
	EventStatusCancelled EventStatus = "cancelled"
)

// PayoutStatus represents the state of an event payout, both on the ticketing
// side (event_payouts) and per transaction on the payment side.
type PayoutStatus string

const (
	PayoutStatusNone    PayoutStatus = "none"
	PayoutStatusPending PayoutStatus = "pending"
	PayoutStatusPaidOut PayoutStatus = "paid_out"
	PayoutStatusFailed  PayoutStatus = "failed"
)

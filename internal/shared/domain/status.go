package domain

// EventStatus represents the lifecycle state of an event.
type EventStatus string

const (
	EventStatusDraft     EventStatus = "draft"
	EventStatusPending   EventStatus = "pending"
	EventStatusPublished EventStatus = "published"
	EventStatusRejected  EventStatus = "rejected"
	EventStatusCancelled EventStatus = "cancelled"
)

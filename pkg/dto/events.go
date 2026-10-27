package dto

import "time"

// Shared Kafka event payloads (cross-service message schemas).
type EventCreated struct {
	EventID string    `json:"event_id"`
	Status  string    `json:"status"`
	At      time.Time `json:"at"`
}

type EventApproved struct {
	EventID     string           `json:"event_id"`
	TicketTypes []TicketTypeInfo `json:"ticket_types"`
	At          time.Time        `json:"at"`
}

type TicketTypeInfo struct {
	TicketTypeID string `json:"ticket_type_id"`
	Name         string `json:"name"`
	Quantity     int    `json:"quantity"`
	PriceRupiah  int    `json:"price_rupiah"`
}

type EventRejected struct {
	EventID string    `json:"event_id"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

type EventUpdated struct {
	EventID string    `json:"event_id"`
	At      time.Time `json:"at"`
}

type EventCancelled struct {
	EventID string    `json:"event_id"`
	At      time.Time `json:"at"`
}

type BookingItem struct {
	TicketTypeID    string `json:"ticket_type_id"`
	Quantity        int    `json:"quantity"`
	UnitPriceRupiah int    `json:"unit_price_rupiah"`
}

type PaymentInitiated struct {
	TransactionID string    `json:"transaction_id"`
	BookingID     string    `json:"booking_id"`
	EventID       string    `json:"event_id"`
	UserID        string    `json:"user_id"`
	AmountRupiah  int       `json:"amount_rupiah"`
	At            time.Time `json:"at"`
}

type PaymentCompleted struct {
	TransactionID string    `json:"transaction_id"`
	BookingID     string    `json:"booking_id"`
	EventID       string    `json:"event_id"`
	UserID        string    `json:"user_id"`
	At            time.Time `json:"at"`
}

type PaymentExpired struct {
	TransactionID string    `json:"transaction_id"`
	BookingID     string    `json:"booking_id"`
	EventID       string    `json:"event_id"`
	UserID        string    `json:"user_id"`
	Reason        string    `json:"reason"`
	At            time.Time `json:"at"`
}

type TicketIssued struct {
	TicketID     string `json:"ticket_id"`
	BookingID    string `json:"booking_id"`
	UserID       string `json:"user_id"`
	EventID      string `json:"event_id"`
	TicketTypeID string `json:"ticket_type_id"`
}

// TopicPaymentInitiate is the outbox topic for reservation → payment
// transaction creation. It is delivered by the ticketing service's REST
// dispatcher (POST /api/payments/internal), never published to Kafka.
const TopicPaymentInitiate = "payment.initiate"

// PaymentInitiateRequested is the outbox payload ticketing writes in the same
// transaction as the reservation. The REST dispatcher turns it into
// POST /api/payments/internal. The payment service is idempotent per booking_id.
type PaymentInitiateRequested struct {
	BookingID    string    `json:"booking_id"`
	EventID      string    `json:"event_id"`
	UserID       string    `json:"user_id"`
	Email        string    `json:"email"`
	AmountRupiah int       `json:"amount_rupiah"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type OrganizerCreated struct {
	UserID       string    `json:"user_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	ProfileLink  string    `json:"profile_link"`
	ContactEmail string    `json:"contact_email"`
	At           time.Time `json:"at"`
}

// Payout topics.
const (
	// TopicPayoutRequested is the Kafka topic ticketing publishes when an
	// organizer requests a payout for a finished event.
	TopicPayoutRequested = "payout.requested"
	// TopicPaymentDisbursement is the outbox topic the payment service writes
	// in the same transaction that marks transactions pending; it is delivered
	// by the payment REST dispatcher to the gateway, never published to Kafka.
	TopicPaymentDisbursement = "payment.disbursement"
	// TopicPayoutCompleted is the Kafka topic payment publishes with the
	// gateway result, consumed by ticketing to settle the event payout.
	TopicPayoutCompleted = "payout.completed"
)

// PayoutRequested is the ticketing → payment payout request. Bank details are
// snapshotted by ticketing from the organizer's payout account.
type PayoutRequested struct {
	EventID               string    `json:"event_id"`
	OrganizerID           string    `json:"organizer_id"`
	PayoutID              string    `json:"payout_id"`
	Attempt               int       `json:"attempt"`
	BankCode              string    `json:"bank_code"`
	BankAccountNumber     string    `json:"bank_account_number"`
	BankAccountHolderName string    `json:"bank_account_holder_name"`
	At                    time.Time `json:"at"`
}

// PaymentDisbursementRequested is the payment outbox payload that drives the
// gateway call. It is delivered by the payment REST dispatcher.
type PaymentDisbursementRequested struct {
	PayoutID              string `json:"payout_id"`
	EventID               string `json:"event_id"`
	Attempt               int    `json:"attempt"`
	AmountRupiah          int64  `json:"amount_rupiah"`
	Currency              string `json:"currency"`
	BankCode              string `json:"bank_code"`
	BankAccountNumber     string `json:"bank_account_number"`
	BankAccountHolderName string `json:"bank_account_holder_name"`
	Description           string `json:"description"`
}

// PayoutCompleted reports the gateway outcome back to ticketing.
type PayoutCompleted struct {
	EventID      string    `json:"event_id"`
	Attempt      int       `json:"attempt"`
	Status       string    `json:"status"`
	AmountRupiah int64     `json:"amount_rupiah"`
	ProviderRef  string    `json:"provider_ref"`
	Reason       string    `json:"reason"`
	At           time.Time `json:"at"`
}

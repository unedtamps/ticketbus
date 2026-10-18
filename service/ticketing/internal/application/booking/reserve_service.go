package booking

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	sdto "github.com/nedo/TicketSaas/pkg/dto"
	"github.com/nedo/TicketSaas/service/ticketing/internal/domain"
)

// Reserve creates a temporary reservation in one atomic database transaction:
// seat counters are decremented, the pending booking is persisted, and a
// payment.initiate outbox row is written. A background dispatcher delivers
// that row to the payment service's internal API to create the payment
// transaction — the HTTP request path never talks to payment, and a payment
// outage can no longer fail a reservation.
func (s *BookingService) Reserve(
	ctx context.Context,
	userID, userEmail, eventID string,
	items []domain.BookingItem,
) (*domain.Reservation, error) {
	if len(items) == 0 {
		return nil, domain.ErrInvalidQuantity
	}

	for _, item := range items {
		// Validate price against stored authoritative price
		realPrice, err := s.seatCounter.GetPrice(ctx, eventID, item.TicketTypeID)
		if err != nil {
			return nil, err
		}
		if item.UnitPriceRupiah != realPrice {
			return nil, fmt.Errorf(
				"%w: expected %d, got %d",
				domain.ErrPriceMismatch,
				realPrice,
				item.UnitPriceRupiah,
			)
		}
	}

	expiresAt := time.Now().Add(time.Duration(s.reservationTTL) * time.Second)
	res := &domain.Reservation{
		BookingID:   uuid.NewString(),
		UserID:      userID,
		EventID:     eventID,
		Items:       items,
		TotalRupiah: calculateTotal(items),
		Status:      "pending",
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now(),
	}
	for i := range res.Items {
		res.Items[i].ID = uuid.NewString()
		res.Items[i].BookingID = res.BookingID
		res.Items[i].TotalPriceRupiah = res.Items[i].UnitPriceRupiah * res.Items[i].Quantity
	}

	booking := &domain.Booking{
		ID:        res.BookingID,
		UserID:    res.UserID,
		EventID:   res.EventID,
		Status:    "pending",
		ExpiresAt: &res.ExpiresAt,
		Items:     res.Items,
	}

	if err := s.bookingRepo.CreateReservation(ctx, booking, domain.OutboxEvent{
		Topic: sdto.TopicPaymentInitiate,
		Key:   res.BookingID,
		Payload: sdto.PaymentInitiateRequested{
			BookingID:    res.BookingID,
			EventID:      res.EventID,
			UserID:       res.UserID,
			Email:        userEmail,
			AmountRupiah: res.TotalRupiah,
			ExpiresAt:    res.ExpiresAt,
		},
	}); err != nil {
		// ErrNoSeatsAvailable from the seat counter maps to 409 at the
		// handler; anything else (db/outbox failure) rolls back the whole
		// reservation transaction and surfaces as a 500.
		return nil, err
	}

	return res, nil
}

// calculateTotal sums the unit prices of all reservation items.
func calculateTotal(items []domain.BookingItem) int {
	total := 0
	for _, item := range items {
		total += item.UnitPriceRupiah * item.Quantity
	}
	return total
}

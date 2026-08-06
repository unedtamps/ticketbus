package booking

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nedo/TicketSaas/internal/ticketing/domain"
)

// Reserve creates a temporary reservation: it validates prices, decrements
// seats, persists a pending booking, and synchronously creates the payment
// transaction in the payment service. If the payment service is unavailable,
// the booking is rolled back and seats are released.
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
		if item.UnitPriceCents != realPrice {
			return nil, fmt.Errorf(
				"%w: expected %d, got %d",
				domain.ErrPriceMismatch,
				realPrice,
				item.UnitPriceCents,
			)
		}

		if err := s.seatCounter.Reserve(
			ctx,
			eventID,
			item.TicketTypeID,
			item.Quantity,
		); err != nil {
			// Roll back only the items reserved before this one failed.
			for _, rb := range items {
				if rb.TicketTypeID == item.TicketTypeID {
					break
				}
				_ = s.seatCounter.Release(ctx, eventID, rb.TicketTypeID, rb.Quantity)
			}
			return nil, fmt.Errorf("%w: %s", domain.ErrNoSeatsAvailable, err)
		}
	}

	expiresAt := time.Now().Add(time.Duration(s.reservationTTL) * time.Second)
	res := &domain.Reservation{
		BookingID:  uuid.NewString(),
		UserID:     userID,
		EventID:    eventID,
		Items:      items,
		TotalCents: calculateTotal(items),
		Status:     "pending",
		ExpiresAt:  expiresAt,
		CreatedAt:  time.Now(),
	}
	for i := range res.Items {
		res.Items[i].ID = uuid.NewString()
		res.Items[i].BookingID = res.BookingID
		res.Items[i].TotalPrice = res.Items[i].UnitPriceCents * res.Items[i].Quantity
	}

	booking := &domain.Booking{
		ID:        res.BookingID,
		UserID:    res.UserID,
		EventID:   res.EventID,
		Status:    "pending",
		ExpiresAt: &res.ExpiresAt,
		Items:     res.Items,
	}
	if err := s.bookingRepo.Create(ctx, booking); err != nil {
		s.rollbackSeats(ctx, eventID, items)
		return nil, err
	}

	if err := s.paymentClient.CreateTxnForBooking(
		ctx,
		res.BookingID,
		res.EventID,
		res.UserID,
		userEmail,
		res.TotalCents,
		res.ExpiresAt,
	); err != nil {
		_ = s.bookingRepo.Delete(ctx, res.BookingID)
		s.rollbackSeats(ctx, eventID, items)
		return nil, err
	}

	return res, nil
}

// rollbackSeats restores seats after a reserve failure where all items were
// already decremented (booking creation or payment creation failed).
func (s *BookingService) rollbackSeats(
	ctx context.Context,
	eventID string,
	items []domain.BookingItem,
) {
	for _, item := range items {
		_ = s.seatCounter.Release(ctx, eventID, item.TicketTypeID, item.Quantity)
	}
}

// calculateTotal sums the unit prices of all reservation items.
func calculateTotal(items []domain.BookingItem) int {
	total := 0
	for _, item := range items {
		total += item.UnitPriceCents * item.Quantity
	}
	return total
}

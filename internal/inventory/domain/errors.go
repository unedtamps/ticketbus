package domain

import "errors"

var (
	ErrNoSeatsAvailable   = errors.New("not enough seats available")
	ErrReservationNotFound = errors.New("reservation not found or expired")
	ErrReservationExpired  = errors.New("reservation has expired")
	ErrBookingNotFound     = errors.New("booking not found")
	ErrInvalidQuantity     = errors.New("invalid quantity")
	ErrEventNotActive      = errors.New("event is not active for reservations")
	ErrPriceMismatch       = errors.New("unit_price_cents does not match ticket type price")
)

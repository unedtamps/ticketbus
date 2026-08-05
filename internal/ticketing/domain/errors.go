package domain

import "errors"

var (
	ErrOrganizerExists     = errors.New("organizer already exists for this user")
	ErrOrganizerNotFound   = errors.New("organizer not found")
	ErrEventNotFound       = errors.New("event not found")
	ErrEventNotEditable    = errors.New("event is not in an editable state")
	ErrEventNotApprovable  = errors.New("only pending events can be approved or rejected")
	ErrEventNotCancellable = errors.New("only published events can be cancelled")
	ErrNotEventOwner       = errors.New("you do not own this event")
	ErrInvalidStatus       = errors.New("invalid event status")
	ErrTicketTypesEmpty    = errors.New("at least one ticket type is required")
	ErrCapacityExceeded    = errors.New("total ticket quantity exceeds venue capacity")

	ErrNoSeatsAvailable    = errors.New("not enough seats available")
	ErrReservationNotFound = errors.New("reservation not found or expired")
	ErrReservationExpired  = errors.New("reservation has expired")
	ErrBookingNotFound     = errors.New("booking not found")
	ErrInvalidQuantity     = errors.New("invalid quantity")
	ErrEventNotActive      = errors.New("event is not active for reservations")
	ErrPriceMismatch       = errors.New("unit_price_cents does not match ticket type price")
)

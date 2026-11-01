package dto

import "time"

// GatewayMinSession is the shortest payment session the gateway accepts. Xendit
// rejects any expiry less than ten minutes in the future.
//
// It is also the boundary between the two halves of a booking hold: a customer
// can only start a payment while more than this remains, because the session is
// created with the hold's own deadline. The first half of the hold is therefore
// the customer's window, the second half is the session.
const GatewayMinSession = 10 * time.Minute

// PaymentDeadline is the last moment a customer can start a payment for a hold
// that expires at expiresAt. It matches the guard in the payment service's
// ProcessPayment, so the countdown the customer sees and the point the button
// stops working are the same instant.
func PaymentDeadline(expiresAt time.Time) time.Time {
	return expiresAt.Add(-GatewayMinSession)
}

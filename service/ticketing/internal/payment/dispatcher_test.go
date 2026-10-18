package payment

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	sdto "github.com/nedo/TicketSaas/pkg/dto"
)

// fakeClient records InitiateTxnForBooking calls.
type fakeClient struct {
	calls []fakeCall
	err   error
}

type fakeCall struct {
	bookingID    string
	eventID      string
	userID       string
	email        string
	amountRupiah int
	expiresAt    time.Time
}

func (f *fakeClient) InitiateTxnForBooking(
	_ context.Context,
	bookingID, eventID, userID, email string,
	amountRupiah int,
	expiresAt time.Time,
) error {
	f.calls = append(f.calls, fakeCall{
		bookingID:    bookingID,
		eventID:      eventID,
		userID:       userID,
		email:        email,
		amountRupiah: amountRupiah,
		expiresAt:    expiresAt,
	})
	return f.err
}

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

func TestDispatcherHandle_MapsPayloadToClientCall(t *testing.T) {
	client := &fakeClient{}
	d := NewDispatcher(nil, client, testLogger, 200)
	ctx := context.Background()
	expiresAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	err := d.handle(ctx, sdto.PaymentInitiateRequested{
		BookingID:    "book-1",
		EventID:      "event-1",
		UserID:       "user-1",
		Email:        "u@example.com",
		AmountRupiah: 20000,
		ExpiresAt:    expiresAt,
	})
	assert.NoError(t, err)
	assert.Len(t, client.calls, 1)
	assert.Equal(t, fakeCall{
		bookingID:    "book-1",
		eventID:      "event-1",
		userID:       "user-1",
		email:        "u@example.com",
		amountRupiah: 20000,
		expiresAt:    expiresAt,
	}, client.calls[0])
}

func TestDispatcherHandle_ClientErrorPropagates(t *testing.T) {
	client := &fakeClient{err: errors.New("payment down")}
	d := NewDispatcher(nil, client, testLogger, 200)

	err := d.handle(context.Background(), sdto.PaymentInitiateRequested{BookingID: "book-1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "payment down")
}

package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nedo/TicketSaas/internal/ticketing/domain"
)

// Client talks to the payment service' internal API to create the payment
// transaction for a booking during reserve.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new payment service client.
func NewClient(baseURL, apiKey string, timeoutSec int) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
	}
}

// InitiateTxnForBooking creates a pending transaction in the payment service.
// Idempotent per booking; safe to retry once on 5xx.
func (c *Client) InitiateTxnForBooking(
	ctx context.Context,
	bookingID, eventID, userID, email string,
	amountRupiah int,
	expiresAt time.Time,
) error {
	body, err := json.Marshal(map[string]interface{}{
		"booking_id":   bookingID,
		"event_id":     eventID,
		"user_id":      userID,
		"email":        email,
		"amount_rupiah": amountRupiah,
		"expires_at":   expiresAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrPaymentUnavailable, err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/api/payments/internal",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrPaymentUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", c.apiKey)

	for attempt := 0; attempt < 2; attempt++ {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt == 0 {
				continue
			}
			return fmt.Errorf("%w: %v", domain.ErrPaymentUnavailable, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		if attempt == 0 && resp.StatusCode >= 500 {
			continue
		}
		return fmt.Errorf("%w: payment returned %d", domain.ErrPaymentUnavailable, resp.StatusCode)
	}
	return domain.ErrPaymentUnavailable
}

package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/nedo/TicketSaas/internal/inventory/domain"
)

// SeatCounter implements domain.SeatCounter using Redis atomic counters.
type SeatCounter struct {
	client *redis.Client
}

// NewSeatCounter creates a new Redis seat counter.
func NewSeatCounter(client *redis.Client) *SeatCounter {
	return &SeatCounter{client: client}
}

func counterKey(eventID, ticketTypeID string) string {
	return fmt.Sprintf("counter:seat:%s:%s", eventID, ticketTypeID)
}

func priceKey(eventID, ticketTypeID string) string {
	return fmt.Sprintf("counter:price:%s:%s", eventID, ticketTypeID)
}

// Init sets the initial available seats.
func (c *SeatCounter) Init(ctx context.Context, eventID, ticketTypeID string, total int) error {
	return c.client.Set(ctx, counterKey(eventID, ticketTypeID), total, 0).Err()
}

// Reserve decrements the seat counter atomically.
func (c *SeatCounter) Reserve(ctx context.Context, eventID, ticketTypeID string, qty int) error {
	key := counterKey(eventID, ticketTypeID)

	// Check current value
	val, err := c.client.Get(ctx, key).Int()
	if err != nil {
		return fmt.Errorf("counter not initialized for %s/%s", eventID, ticketTypeID)
	}
	if val < qty {
		return domain.ErrNoSeatsAvailable
	}

	newVal, err := c.client.DecrBy(ctx, key, int64(qty)).Result()
	if err != nil {
		return err
	}
	if newVal < 0 {
		// Rollback
		c.client.IncrBy(ctx, key, int64(qty))
		return domain.ErrNoSeatsAvailable
	}
	return nil
}

// Release increments the seat counter.
func (c *SeatCounter) Release(ctx context.Context, eventID, ticketTypeID string, qty int) error {
	return c.client.IncrBy(ctx, counterKey(eventID, ticketTypeID), int64(qty)).Err()
}

// Available returns current available seats.
func (c *SeatCounter) Available(ctx context.Context, eventID, ticketTypeID string) (int, error) {
	val, err := c.client.Get(ctx, counterKey(eventID, ticketTypeID)).Int()
	if err != nil {
		return 0, fmt.Errorf("counter not found: %w", err)
	}
	return val, nil
}

// SetPrice stores the authoritative price for a ticket type.
func (c *SeatCounter) SetPrice(ctx context.Context, eventID, ticketTypeID string, price int) error {
	return c.client.Set(ctx, priceKey(eventID, ticketTypeID), price, 0).Err()
}

// GetPrice returns the stored authoritative price for a ticket type.
func (c *SeatCounter) GetPrice(ctx context.Context, eventID, ticketTypeID string) (int, error) {
	val, err := c.client.Get(ctx, priceKey(eventID, ticketTypeID)).Int()
	if err != nil {
		return 0, fmt.Errorf("price not found for %s/%s", eventID, ticketTypeID)
	}
	return val, nil
}

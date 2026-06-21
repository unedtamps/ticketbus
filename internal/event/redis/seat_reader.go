package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// SeatReader reads live seat availability counters from Redis (read-only).
type SeatReader struct {
	client *redis.Client
}

// NewSeatReader creates a read-only seat availability reader.
func NewSeatReader(client *redis.Client) *SeatReader {
	return &SeatReader{client: client}
}

func counterKey(eventID, ticketTypeID string) string {
	return fmt.Sprintf("counter:seat:%s:%s", eventID, ticketTypeID)
}

// Available returns current available seats for a ticket type.
// Returns 0 if the counter is not yet initialized or missing.
func (r *SeatReader) Available(ctx context.Context, eventID, ticketTypeID string) int {
	val, err := r.client.Get(ctx, counterKey(eventID, ticketTypeID)).Int()
	if err != nil {
		return 0
	}
	return val
}

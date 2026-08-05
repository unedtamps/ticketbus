package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ReservationCache triggers reservation expiry through a Redis TTL marker.
// It stores no business data; PostgreSQL is the single source of truth for
// the booking and its items.
type ReservationCache struct {
	client *redis.Client
}

// NewReservationCache creates a new Redis reservation trigger.
func NewReservationCache(client *redis.Client) *ReservationCache {
	return &ReservationCache{client: client}
}

func reservationKey(bookingID string) string {
	return fmt.Sprintf("reservation:%s", bookingID)
}

// Save stores a TTL marker for the booking. When the marker expires, a
// keyspace notification fires so the service can expire the booking.
func (c *ReservationCache) Save(ctx context.Context, bookingID string, ttlSeconds int) error {
	return c.client.Set(ctx, reservationKey(bookingID), "1", time.Duration(ttlSeconds)*time.Second).Err()
}

// Delete removes the TTL marker.
func (c *ReservationCache) Delete(ctx context.Context, bookingID string) error {
	return c.client.Del(ctx, reservationKey(bookingID)).Err()
}

// SubscribeExpiry subscribes to keyspace expiry events and delivers the
// booking IDs whose reservation markers expired.
func (c *ReservationCache) SubscribeExpiry(ctx context.Context) (<-chan string, error) {
	ch := make(chan string, 100)
	pubsub := c.client.PSubscribe(ctx, "__keyevent@0__:expired")

	go func() {
		defer pubsub.Close()
		defer close(ch)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				msg, err := pubsub.ReceiveMessage(ctx)
				if err != nil {
					return
				}
				if !strings.HasPrefix(msg.Payload, "reservation:") {
					continue
				}
				ch <- strings.TrimPrefix(msg.Payload, "reservation:")
			}
		}
	}()

	return ch, nil
}

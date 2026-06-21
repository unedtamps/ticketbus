package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/nedo/TicketSaas/internal/inventory/domain"
)

// ReservationCache implements domain.ReservationCache using Redis.
type ReservationCache struct {
	client *redis.Client
}

// NewReservationCache creates a new Redis reservation cache.
func NewReservationCache(client *redis.Client) *ReservationCache {
	return &ReservationCache{client: client}
}

func reservationKey(bookingID string) string {
	return fmt.Sprintf("reservation:%s", bookingID)
}

func shadowKey(bookingID string) string {
	return fmt.Sprintf("rsvn-data:%s", bookingID)
}

// Save stores a reservation with a TTL plus a shadow copy without TTL.
func (c *ReservationCache) Save(ctx context.Context, res *domain.Reservation, ttlSeconds int) error {
	data, err := json.Marshal(res)
	if err != nil {
		return err
	}
	key := reservationKey(res.BookingID)
	if err := c.client.Set(ctx, key, data, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return err
	}
	return c.client.Set(ctx, shadowKey(res.BookingID), data, 0).Err()
}

// Find retrieves a reservation by booking ID.
func (c *ReservationCache) Find(ctx context.Context, bookingID string) (*domain.Reservation, error) {
	data, err := c.client.Get(ctx, reservationKey(bookingID)).Result()
	if err != nil {
		return nil, err
	}
	var res domain.Reservation
	if err := json.Unmarshal([]byte(data), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Delete removes a reservation and its shadow.
func (c *ReservationCache) Delete(ctx context.Context, bookingID string) error {
	_ = c.client.Del(ctx, shadowKey(bookingID))
	return c.client.Del(ctx, reservationKey(bookingID)).Err()
}

// SubscribeExpiry subscribes to keyspace expiry events. When a reservation key
// expires, the shadow copy (no TTL) is read and delivered via the channel
// before being cleaned up.
func (c *ReservationCache) SubscribeExpiry(ctx context.Context) (<-chan *domain.Reservation, error) {
	ch := make(chan *domain.Reservation, 100)
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
				bookingID := strings.TrimPrefix(msg.Payload, "reservation:")
				data, err := c.client.Get(ctx, shadowKey(bookingID)).Result()
				if err != nil {
					continue
				}
				var res domain.Reservation
				if err := json.Unmarshal([]byte(data), &res); err != nil {
					continue
				}
				_ = c.client.Del(ctx, shadowKey(bookingID))
				ch <- &res
			}
		}
	}()

	return ch, nil
}

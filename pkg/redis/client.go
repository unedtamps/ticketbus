package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps go-redis with application helpers.
type Client struct {
	client *redis.Client
}

// NewClient creates a new Redis client.
func NewClient(addr, password string, db int) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &Client{client: rdb}, nil
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	return c.client.Close()
}

// GetClient returns the underlying go-redis client.
func (c *Client) GetClient() *redis.Client {
	return c.client
}

// SetJSON stores a JSON value with a TTL.
func (c *Client) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

// GetJSON retrieves a raw string value.
func (c *Client) GetJSON(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

// Delete removes a key.
func (c *Client) Delete(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

// Incr increments a key atomically.
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}

// Decr decrements a key atomically.
func (c *Client) Decr(ctx context.Context, key string) (int64, error) {
	return c.client.Decr(ctx, key).Result()
}

// IncrBy increments a key by a delta atomically.
func (c *Client) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	return c.client.IncrBy(ctx, key, delta).Result()
}

// DecrBy decrements a key by a delta atomically.
func (c *Client) DecrBy(ctx context.Context, key string, delta int64) (int64, error) {
	return c.client.DecrBy(ctx, key, delta).Result()
}

// SetCounter sets a counter value.
func (c *Client) SetCounter(ctx context.Context, key string, value int64) error {
	return c.client.Set(ctx, key, value, 0).Err()
}

// GetCounter gets a counter value.
func (c *Client) GetCounter(ctx context.Context, key string) (int64, error) {
	return c.client.Get(ctx, key).Int64()
}

// SubscribeKeyspaceEvents subscribes to Redis keyspace expired events on the configured DB.
func (c *Client) SubscribeKeyspaceEvents(ctx context.Context, db int) *redis.PubSub {
	channel := fmt.Sprintf("__keyevent@%d__:expired", db)
	return c.client.PSubscribe(ctx, channel)
}

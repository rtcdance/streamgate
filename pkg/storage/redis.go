package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisCache handles Redis cache
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache creates a new Redis cache instance
func NewRedisCache() *RedisCache {
	return &RedisCache{}
}

// Connect connects to Redis. Uses a background context for the initial
// connection test since the caller has not yet provided a request context.
func (rc *RedisCache) Connect(addr string) error {
	rc.client = redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     "", // no password set
		DB:           0,  // use default DB
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rc.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return nil
}

// Get gets value from Redis. Derives a 3s timeout from ctx.
func (rc *RedisCache) Get(ctx context.Context, key string) (string, error) {
	if rc.client == nil {
		return "", fmt.Errorf("redis not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	val, err := rc.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", fmt.Errorf("key not found: %s", key)
	} else if err != nil {
		return "", fmt.Errorf("failed to get key: %w", err)
	}

	return val, nil
}

// Set sets value in Redis
func (rc *RedisCache) Set(ctx context.Context, key, value string) error {
	return rc.SetWithExpiration(ctx, key, value, 0)
}

// SetWithExpiration sets value in Redis with expiration. Derives a 3s timeout from ctx.
func (rc *RedisCache) SetWithExpiration(ctx context.Context, key, value string, expiration time.Duration) error {
	if rc.client == nil {
		return fmt.Errorf("redis not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := rc.client.Set(ctx, key, value, expiration).Err(); err != nil {
		return fmt.Errorf("failed to set key: %w", err)
	}

	return nil
}

// Delete deletes a key from Redis. Derives a 3s timeout from ctx.
func (rc *RedisCache) Delete(ctx context.Context, key string) error {
	if rc.client == nil {
		return fmt.Errorf("redis not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := rc.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete key: %w", err)
	}

	return nil
}

// Exists checks if a key exists in Redis. Derives a 3s timeout from ctx.
func (rc *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	if rc.client == nil {
		return false, fmt.Errorf("redis not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	count, err := rc.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check key existence: %w", err)
	}

	return count > 0, nil
}

// Expire sets expiration on a key. Derives a 3s timeout from ctx.
func (rc *RedisCache) Expire(ctx context.Context, key string, expiration time.Duration) error {
	if rc.client == nil {
		return fmt.Errorf("redis not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := rc.client.Expire(ctx, key, expiration).Err(); err != nil {
		return fmt.Errorf("failed to set expiration: %w", err)
	}

	return nil
}

// Close closes Redis connection
func (rc *RedisCache) Close() error {
	if rc.client == nil {
		return nil
	}
	return rc.client.Close()
}

// Ping checks if Redis is alive. Derives a 3s timeout from ctx.
func (rc *RedisCache) Ping(ctx context.Context) error {
	if rc.client == nil {
		return fmt.Errorf("redis not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	return rc.client.Ping(ctx).Err()
}

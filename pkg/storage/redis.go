package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisCache handles Redis cache
type RedisCache struct {
	client *redis.Client
	ctx    context.Context
}

// NewRedisCache creates a new Redis cache instance
func NewRedisCache() *RedisCache {
	return &RedisCache{
		ctx: context.Background(),
	}
}

// Connect connects to Redis
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
	ctx, cancel := context.WithTimeout(rc.ctx, 5*time.Second)
	defer cancel()

	if err := rc.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return nil
}

// Get gets value from Redis
func (rc *RedisCache) Get(key string) (string, error) {
	if rc.client == nil {
		return "", fmt.Errorf("Redis not connected")
	}

	ctx, cancel := context.WithTimeout(rc.ctx, 3*time.Second)
	defer cancel()

	val, err := rc.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key not found: %s", key)
	} else if err != nil {
		return "", fmt.Errorf("failed to get key: %w", err)
	}

	return val, nil
}

// Set sets value in Redis
func (rc *RedisCache) Set(key string, value string) error {
	return rc.SetWithExpiration(key, value, 0)
}

// SetWithExpiration sets value in Redis with expiration
func (rc *RedisCache) SetWithExpiration(key string, value string, expiration time.Duration) error {
	if rc.client == nil {
		return fmt.Errorf("Redis not connected")
	}

	ctx, cancel := context.WithTimeout(rc.ctx, 3*time.Second)
	defer cancel()

	if err := rc.client.Set(ctx, key, value, expiration).Err(); err != nil {
		return fmt.Errorf("failed to set key: %w", err)
	}

	return nil
}

// Delete deletes a key from Redis
func (rc *RedisCache) Delete(key string) error {
	if rc.client == nil {
		return fmt.Errorf("Redis not connected")
	}

	ctx, cancel := context.WithTimeout(rc.ctx, 3*time.Second)
	defer cancel()

	if err := rc.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete key: %w", err)
	}

	return nil
}

// Exists checks if a key exists in Redis
func (rc *RedisCache) Exists(key string) (bool, error) {
	if rc.client == nil {
		return false, fmt.Errorf("Redis not connected")
	}

	ctx, cancel := context.WithTimeout(rc.ctx, 3*time.Second)
	defer cancel()

	count, err := rc.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check key existence: %w", err)
	}

	return count > 0, nil
}

// Expire sets expiration on a key
func (rc *RedisCache) Expire(key string, expiration time.Duration) error {
	if rc.client == nil {
		return fmt.Errorf("Redis not connected")
	}

	ctx, cancel := context.WithTimeout(rc.ctx, 3*time.Second)
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

// Ping checks if Redis is alive
func (rc *RedisCache) Ping() error {
	if rc.client == nil {
		return fmt.Errorf("Redis not connected")
	}

	ctx, cancel := context.WithTimeout(rc.ctx, 3*time.Second)
	defer cancel()

	return rc.client.Ping(ctx).Err()
}

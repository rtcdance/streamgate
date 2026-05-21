package storage

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/rtcdance/streamgate/pkg/resilience"
)

const (
	redisMaxRetries   = 3
	redisRetryBackoff = time.Second
)

type RedisCache struct {
	client *redis.Client
	cb     *resilience.CircuitBreaker
}

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Addr       string
	Password   string
	DB         int
	TLSEnabled bool
}

func NewRedisCache() *RedisCache {
	return &RedisCache{}
}

func (rc *RedisCache) SetCircuitBreaker(cb *resilience.CircuitBreaker) {
	rc.cb = cb
}

func (rc *RedisCache) Connect(cfg RedisConfig) error {
	opts := &redis.Options{
		Addr:               cfg.Addr,
		Password:           cfg.Password,
		DB:                 cfg.DB,
		DialTimeout:        5 * time.Second,
		ReadTimeout:        3 * time.Second,
		WriteTimeout:       3 * time.Second,
		PoolSize:           10,
		MinIdleConns:       5,
		PoolTimeout:        4 * time.Second,
		IdleTimeout:        5 * time.Minute,
		IdleCheckFrequency: 1 * time.Minute,
		MaxRetries:         3,
	}
	if cfg.TLSEnabled {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	var lastErr error
	for attempt := 0; attempt <= redisMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(redisRetryBackoff * time.Duration(1<<(attempt-1)))
		}

		client := redis.NewClient(opts)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := client.Ping(ctx).Err()
		cancel()

		if err != nil {
			_ = client.Close()
			lastErr = fmt.Errorf("failed to connect to Redis: %w", err)
			continue
		}

		rc.client = client
		return nil
	}

	return fmt.Errorf("redis connect failed after %d attempts: %w", redisMaxRetries+1, lastErr)
}

func (rc *RedisCache) Get(ctx context.Context, key string) (string, error) {
	if rc.client == nil {
		return "", fmt.Errorf("redis not connected")
	}
	if rc.cb != nil && !rc.cb.Allow() {
		return "", fmt.Errorf("circuit breaker is open for Redis")
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()

	val, err := rc.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		if rc.cb != nil {
			rc.cb.RecordSuccess()
		}
		return "", fmt.Errorf("key not found: %s", key)
	} else if err != nil {
		if rc.cb != nil {
			rc.cb.RecordFailure()
		}
		return "", fmt.Errorf("failed to get key: %w", err)
	}

	if rc.cb != nil {
		rc.cb.RecordSuccess()
	}
	return val, nil
}

func (rc *RedisCache) Set(ctx context.Context, key, value string) error {
	return rc.SetWithExpiration(ctx, key, value, 0)
}

func (rc *RedisCache) SetWithExpiration(ctx context.Context, key, value string, expiration time.Duration) error {
	if rc.client == nil {
		return fmt.Errorf("redis not connected")
	}
	if rc.cb != nil && !rc.cb.Allow() {
		return fmt.Errorf("circuit breaker is open for Redis")
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()

	if err := rc.client.Set(ctx, key, value, expiration).Err(); err != nil {
		if rc.cb != nil {
			rc.cb.RecordFailure()
		}
		return fmt.Errorf("failed to set key: %w", err)
	}

	if rc.cb != nil {
		rc.cb.RecordSuccess()
	}
	return nil
}

func (rc *RedisCache) Delete(ctx context.Context, key string) error {
	if rc.client == nil {
		return fmt.Errorf("redis not connected")
	}
	if rc.cb != nil && !rc.cb.Allow() {
		return fmt.Errorf("circuit breaker is open for Redis")
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()

	if err := rc.client.Del(ctx, key).Err(); err != nil {
		if rc.cb != nil {
			rc.cb.RecordFailure()
		}
		return fmt.Errorf("failed to delete key: %w", err)
	}

	if rc.cb != nil {
		rc.cb.RecordSuccess()
	}
	return nil
}

func (rc *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	if rc.client == nil {
		return false, fmt.Errorf("redis not connected")
	}
	if rc.cb != nil && !rc.cb.Allow() {
		return false, fmt.Errorf("circuit breaker is open for Redis")
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()

	count, err := rc.client.Exists(ctx, key).Result()
	if err != nil {
		if rc.cb != nil {
			rc.cb.RecordFailure()
		}
		return false, fmt.Errorf("failed to check key existence: %w", err)
	}

	if rc.cb != nil {
		rc.cb.RecordSuccess()
	}
	return count > 0, nil
}

func (rc *RedisCache) Expire(ctx context.Context, key string, expiration time.Duration) error {
	if rc.client == nil {
		return fmt.Errorf("redis not connected")
	}
	if rc.cb != nil && !rc.cb.Allow() {
		return fmt.Errorf("circuit breaker is open for Redis")
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()

	if err := rc.client.Expire(ctx, key, expiration).Err(); err != nil {
		if rc.cb != nil {
			rc.cb.RecordFailure()
		}
		return fmt.Errorf("failed to set expiration: %w", err)
	}

	if rc.cb != nil {
		rc.cb.RecordSuccess()
	}
	return nil
}

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

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()

	return rc.client.Ping(ctx).Err()
}

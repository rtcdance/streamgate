package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisChallengeStore stores challenges in Redis.
type RedisChallengeStore struct {
	client *redis.Client
	ttl    time.Duration
}

// redisChallengeStoreConfig holds Redis connection options.
type redisChallengeStoreConfig struct {
	password     string
	db           int
	poolSize     int
	dialTimeout  time.Duration
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// RedisChallengeStoreOption configures a RedisChallengeStore.
type RedisChallengeStoreOption func(*redisChallengeStoreConfig)

// WithRedisPassword sets the Redis password.
func WithRedisPassword(password string) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.password = password }
}

// WithRedisDB sets the Redis database index.
func WithRedisDB(db int) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.db = db }
}

// WithRedisPoolSize sets the connection pool size.
func WithRedisPoolSize(size int) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.poolSize = size }
}

// WithRedisDialTimeout sets the dial timeout.
func WithRedisDialTimeout(d time.Duration) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.dialTimeout = d }
}

// WithRedisReadTimeout sets the read timeout.
func WithRedisReadTimeout(d time.Duration) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.readTimeout = d }
}

// WithRedisWriteTimeout sets the write timeout.
func WithRedisWriteTimeout(d time.Duration) RedisChallengeStoreOption {
	return func(c *redisChallengeStoreConfig) { c.writeTimeout = d }
}

// NewRedisChallengeStore creates a Redis-backed challenge store.
func NewRedisChallengeStore(addr string, ttl time.Duration, opts ...RedisChallengeStoreOption) (*RedisChallengeStore, error) {
	cfg := redisChallengeStoreConfig{
		poolSize:     100,
		dialTimeout:  5 * time.Second,
		readTimeout:  3 * time.Second,
		writeTimeout: 3 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     cfg.password,
		DB:           cfg.db,
		PoolSize:     cfg.poolSize,
		DialTimeout:  cfg.dialTimeout,
		ReadTimeout:  cfg.readTimeout,
		WriteTimeout: cfg.writeTimeout,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisChallengeStore{
		client: client,
		ttl:    ttl,
	}, nil
}

// NewRedisChallengeStoreWithClient creates a Redis-backed challenge store
// using an existing client. The caller manages the client lifecycle.
func NewRedisChallengeStoreWithClient(client *redis.Client, ttl time.Duration) *RedisChallengeStore {
	return &RedisChallengeStore{
		client: client,
		ttl:    ttl,
	}
}

func (r *RedisChallengeStore) GetChallenge(ctx context.Context, id string) (*WalletChallenge, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	data, err := r.client.Get(ctx, id).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("challenge not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get challenge: %w", err)
	}

	var challenge WalletChallenge
	if err := json.Unmarshal(data, &challenge); err != nil {
		return nil, fmt.Errorf("failed to decode challenge: %w", err)
	}
	return &challenge, nil
}

func (r *RedisChallengeStore) SaveChallenge(ctx context.Context, challenge *WalletChallenge) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	data, err := json.Marshal(challenge)
	if err != nil {
		return fmt.Errorf("failed to encode challenge: %w", err)
	}

	if err := r.client.Set(ctx, challenge.ID, data, r.ttl).Err(); err != nil {
		return fmt.Errorf("failed to save challenge: %w", err)
	}
	return nil
}

// markChallengeUsedLua is a Redis Lua script that atomically checks if a
// challenge is unused and marks it used. Returns the previous used_at value
// (empty string if unused, non-empty if already used). This prevents the
// TOCTOU race where two concurrent requests both read a challenge as unused
// and both receive valid JWTs.
var markChallengeUsedLua = redis.NewScript(`
local data = redis.call('GET', KEYS[1])
if not data then
  return 'NOT_FOUND'
end
local decoded = cjson.decode(data)
if decoded.used_at and decoded.used_at ~= '' and decoded.used_at ~= '0001-01-01T00:00:00Z' then
  return 'ALREADY_USED'
end
decoded.used_at = ARGV[1]
local encoded = cjson.encode(decoded)
redis.call('SET', KEYS[1], encoded, 'PX', ARGV[2])
return 'OK'
`)

// MarkChallengeUsed marks a challenge used in Redis atomically via Lua script.
func (r *RedisChallengeStore) MarkChallengeUsed(ctx context.Context, id string, usedAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Calculate remaining TTL in milliseconds
	ttlMs := int64(r.ttl / time.Millisecond)

	result, err := markChallengeUsedLua.Run(ctx, r.client, []string{id},
		usedAt.UTC().Format(time.RFC3339Nano), ttlMs).Result()
	if err != nil {
		return fmt.Errorf("failed to mark challenge used: %w", err)
	}

	str, ok := result.(string)
	if !ok {
		return fmt.Errorf("unexpected Lua script result type: %T", result)
	}

	switch str {
	case "OK":
		return nil
	case "ALREADY_USED":
		return ErrChallengeUsed
	case "NOT_FOUND":
		return ErrChallengeNotFound
	}
	return nil
}

// Close closes the Redis connection.
func (r *RedisChallengeStore) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

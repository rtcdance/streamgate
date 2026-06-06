package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	lru "github.com/hashicorp/golang-lru"
)

const blacklistKeyPrefix = "token_blacklist:"
const localLRUSize = 10000

// revocationEntry holds a locally-cached revocation with its expiry.
type revocationEntry struct {
	expiresAt time.Time
}

// isExpired checks if the revocation entry has passed its expiry.
func (e *revocationEntry) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

// RedisTokenBlacklist implements token revocation backed by Redis.
// Tokens are stored as keys with TTL equal to their remaining validity,
// so expired entries are automatically evicted by Redis.
//
// A local LRU cache provides fallback when Redis is unreachable:
// recently revoked JTIs are cached in-process so that a Redis outage
// does not silently allow revoked tokens through.
type RedisTokenBlacklist struct {
	client     *redis.Client
	local      *lru.Cache
	FailClosed bool
}

// NewRedisTokenBlacklist creates a new Redis-backed token blacklist.
// Returns an error if the Redis PING fails.
func NewRedisTokenBlacklist(client *redis.Client) (*RedisTokenBlacklist, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis token blacklist: ping failed: %w", err)
	}

	cache, _ := lru.New(localLRUSize)

	return &RedisTokenBlacklist{
		client:     client,
		local:      cache,
		FailClosed: true,
	}, nil
}

// Close releases the underlying Redis connection.
func (b *RedisTokenBlacklist) Close() error {
	return b.client.Close()
}

// Revoke adds a JTI to the blacklist. The key's TTL is set to the token's
// remaining lifetime so Redis evicts it automatically once it expires.
// The revocation is also cached locally for fallback when Redis is down.
func (b *RedisTokenBlacklist) Revoke(ctx context.Context, jti string, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		// Token already expired — nothing to store
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	key := blacklistKeyPrefix + jti
	if err := b.client.Set(ctx, key, "1", ttl).Err(); err != nil {
		// Still cache locally even if Redis write fails
		b.local.Add(jti, &revocationEntry{expiresAt: expiresAt})
		return err
	}

	// Cache locally for fallback
	b.local.Add(jti, &revocationEntry{expiresAt: expiresAt})
	return nil
}

// IsRevoked checks if a JTI is blacklisted.
// On Redis errors, falls back to the local LRU cache so that recently
// revoked tokens remain blocked even when Redis is unreachable.
func (b *RedisTokenBlacklist) IsRevoked(ctx context.Context, jti string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	key := blacklistKeyPrefix + jti
	val, err := b.client.Exists(ctx, key).Result()
	if err != nil {
		// Fail-closed is the secure default for token-gated systems: when
		// the revocation store is unreachable, treat unknown tokens as
		// revoked rather than silently allowing them through.
		if b.FailClosed {
			return true
		}
		if entry, ok := b.local.Get(jti); ok {
			e := entry.(*revocationEntry)
			if !e.isExpired() {
				return true
			}
			b.local.Remove(jti)
		}
		return false
	}
	if val > 0 {
		return true
	}
	return false
}

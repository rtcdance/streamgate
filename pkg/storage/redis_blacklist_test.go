package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMockRedis = errors.New("mock redis error")

func setupRedisTokenBlacklist(t *testing.T) (*RedisTokenBlacklist, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = bl.Close()
		mr.Close()
	})

	return bl, mr
}

func TestNewRedisTokenBlacklist_PingFails(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:0"})
	_, err := NewRedisTokenBlacklist(client)
	assert.Error(t, err)
}

func TestRedisTokenBlacklist_Revoke_AlreadyExpired(t *testing.T) {
	bl, _ := setupRedisTokenBlacklist(t)

	err := bl.Revoke(context.Background(), "jti-expired", time.Now().Add(-1*time.Hour))
	assert.NoError(t, err)
}

func TestRedisTokenBlacklist_Revoke_Success(t *testing.T) {
	bl, _ := setupRedisTokenBlacklist(t)

	err := bl.Revoke(context.Background(), "jti-123", time.Now().Add(time.Hour))
	require.NoError(t, err)

	assert.True(t, bl.IsRevoked(context.Background(), "jti-123"))
}

func TestRedisTokenBlacklist_IsRevoked_NotRevoked(t *testing.T) {
	bl, _ := setupRedisTokenBlacklist(t)

	assert.False(t, bl.IsRevoked(context.Background(), "jti-not-revoked"))
}

func TestRedisTokenBlacklist_Close(t *testing.T) {
	bl, _ := setupRedisTokenBlacklist(t)

	err := bl.Close()
	assert.NoError(t, err)
}

func TestRedisTokenBlacklist_LocalLRUFallback(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)
	defer bl.Close()

	err = bl.Revoke(context.Background(), "jti-local", time.Now().Add(time.Hour))
	require.NoError(t, err)

	mr.Close()

	assert.True(t, bl.IsRevoked(context.Background(), "jti-local"))
}

func TestRedisTokenBlacklist_FailOpen(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)
	defer bl.Close()

	bl.FailClosed = false
	mr.Close()

	assert.False(t, bl.IsRevoked(context.Background(), "jti-not-in-local"))
}

func TestRedisTokenBlacklist_Revoke_RedisFails(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)
	defer bl.Close()

	mr.Close()

	err = bl.Revoke(context.Background(), "jti-redis-fail", time.Now().Add(time.Hour))
	assert.Error(t, err)

	assert.True(t, bl.local.Contains("jti-redis-fail"))
}

func TestRedisTokenBlacklist_IsRevoked_ExpiredLocalEntry(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)
	defer bl.Close()

	bl.local.Add("jti-expired-local", &revocationEntry{expiresAt: time.Now().Add(-time.Hour)})

	mr.Close()

	bl.FailClosed = false
	assert.False(t, bl.IsRevoked(context.Background(), "jti-expired-local"))
}

func TestRedisTokenBlacklist_Constants(t *testing.T) {
	assert.Equal(t, "token_blacklist:", blacklistKeyPrefix)
	assert.Equal(t, 10000, localLRUSize)
}

func TestRedisTokenBlacklist_Revoke_ValidTTL(t *testing.T) {
	bl, _ := setupRedisTokenBlacklist(t)

	expiresAt := time.Now().Add(time.Hour)
	err := bl.Revoke(context.Background(), "jti-ttl", expiresAt)
	require.NoError(t, err)

	assert.True(t, bl.IsRevoked(context.Background(), "jti-ttl"))
}

func TestRedisTokenBlacklist_IsRevoked_FailClosed_NoLocalEntry(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bl, err := NewRedisTokenBlacklist(client)
	require.NoError(t, err)
	defer bl.Close()

	mr.Close()

	assert.True(t, bl.IsRevoked(context.Background(), "jti-not-in-local"))
}



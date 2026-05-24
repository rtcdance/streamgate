package storage

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRedisChallengeStore(t *testing.T) (*RedisChallengeStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := &RedisChallengeStore{
		client: client,
		ttl:    5 * time.Minute,
	}

	t.Cleanup(func() {
		_ = store.Close()
		mr.Close()
	})

	return store, mr
}

func TestRedisChallengeStore_SaveAndGet(t *testing.T) {
	store, _ := setupRedisChallengeStore(t)
	ctx := context.Background()

	challenge := &WalletChallenge{
		ID:            "test-challenge-1",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		SigningType:   "personal_sign",
		Nonce:         "abc123",
		Message:       "Sign this message",
		IssuedAt:      time.Now().UTC().Truncate(time.Millisecond),
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute).Truncate(time.Millisecond),
	}

	err := store.SaveChallenge(ctx, challenge)
	require.NoError(t, err)

	got, err := store.GetChallenge(ctx, challenge.ID)
	require.NoError(t, err)

	assert.Equal(t, challenge.ID, got.ID)
	assert.Equal(t, challenge.WalletAddress, got.WalletAddress)
	assert.Equal(t, challenge.ChainID, got.ChainID)
	assert.Equal(t, challenge.Nonce, got.Nonce)
	assert.Equal(t, challenge.Message, got.Message)
	assert.True(t, challenge.IssuedAt.Equal(got.IssuedAt))
	assert.True(t, challenge.ExpiresAt.Equal(got.ExpiresAt))
}

func TestRedisChallengeStore_GetNotFound(t *testing.T) {
	store, _ := setupRedisChallengeStore(t)
	ctx := context.Background()

	_, err := store.GetChallenge(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "challenge not found")
}

func TestRedisChallengeStore_MarkChallengeUsed(t *testing.T) {
	store, _ := setupRedisChallengeStore(t)
	ctx := context.Background()

	challenge := &WalletChallenge{
		ID:            "test-challenge-2",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		SigningType:   "personal_sign",
		Nonce:         "def456",
		Message:       "Sign this message",
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := store.SaveChallenge(ctx, challenge)
	require.NoError(t, err)

	usedAt := time.Now().UTC().Truncate(time.Millisecond)
	err = store.MarkChallengeUsed(ctx, challenge.ID, usedAt)
	require.NoError(t, err)

	// Verify used_at was persisted
	got, err := store.GetChallenge(ctx, challenge.ID)
	require.NoError(t, err)
	assert.True(t, usedAt.Equal(got.UsedAt))
}

func TestRedisChallengeStore_MarkChallengeUsed_AlreadyUsed(t *testing.T) {
	store, _ := setupRedisChallengeStore(t)
	ctx := context.Background()

	challenge := &WalletChallenge{
		ID:            "test-challenge-3",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		SigningType:   "personal_sign",
		Nonce:         "ghi789",
		Message:       "Sign this message",
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := store.SaveChallenge(ctx, challenge)
	require.NoError(t, err)

	usedAt := time.Now().UTC()
	err = store.MarkChallengeUsed(ctx, challenge.ID, usedAt)
	require.NoError(t, err)

	// Second mark should fail with "already used"
	err = store.MarkChallengeUsed(ctx, challenge.ID, time.Now().UTC())
	assert.Error(t, err)
	assert.Equal(t, "challenge already used", err.Error())
}

func TestRedisChallengeStore_MarkChallengeUsed_NotFound(t *testing.T) {
	store, _ := setupRedisChallengeStore(t)
	ctx := context.Background()

	err := store.MarkChallengeUsed(ctx, "nonexistent", time.Now().UTC())
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrChallengeNotFound)
}

func TestRedisChallengeStore_TTLExpiration(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := &RedisChallengeStore{
		client: client,
		ttl:    100 * time.Millisecond,
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	challenge := &WalletChallenge{
		ID:            "test-challenge-ttl",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		Nonce:         "ttl123",
		Message:       "Sign this message",
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err = store.SaveChallenge(ctx, challenge)
	require.NoError(t, err)

	// Should exist immediately
	_, err = store.GetChallenge(ctx, challenge.ID)
	require.NoError(t, err)

	// Fast-forward miniredis clock past TTL
	mr.FastForward(150 * time.Millisecond)

	// Should be gone after TTL
	_, err = store.GetChallenge(ctx, challenge.ID)
	assert.Error(t, err)
}

func TestMemoryChallengeStore_SaveAndGet(t *testing.T) {
	store := NewMemoryChallengeStore()
	ctx := context.Background()

	challenge := &WalletChallenge{
		ID:            "mem-challenge-1",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		Nonce:         "mem123",
		Message:       "Sign this message",
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := store.SaveChallenge(ctx, challenge)
	require.NoError(t, err)

	got, err := store.GetChallenge(ctx, challenge.ID)
	require.NoError(t, err)
	assert.Equal(t, challenge.ID, got.ID)
	assert.Equal(t, challenge.WalletAddress, got.WalletAddress)

	// Returns a copy — mutation should not affect the stored original
	got.Nonce = "changed"
	original, _ := store.GetChallenge(ctx, challenge.ID)
	assert.Equal(t, "mem123", original.Nonce)
}

func TestMemoryChallengeStore_MarkUsed_AlreadyUsed(t *testing.T) {
	store := NewMemoryChallengeStore()
	ctx := context.Background()

	challenge := &WalletChallenge{
		ID:            "mem-challenge-2",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		Nonce:         "mem456",
		Message:       "Sign this message",
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	require.NoError(t, store.SaveChallenge(ctx, challenge))

	err := store.MarkChallengeUsed(ctx, challenge.ID, time.Now().UTC())
	require.NoError(t, err)

	err = store.MarkChallengeUsed(ctx, challenge.ID, time.Now().UTC())
	assert.EqualError(t, err, "challenge already used")
}

func TestMemoryChallengeStore_NotFound(t *testing.T) {
	store := NewMemoryChallengeStore()
	ctx := context.Background()

	_, err := store.GetChallenge(ctx, "nonexistent")
	assert.EqualError(t, err, "challenge not found")

	err = store.MarkChallengeUsed(ctx, "nonexistent", time.Now().UTC())
	assert.EqualError(t, err, "challenge not found")
}

func TestWithRedisPassword(t *testing.T) {
	cfg := redisChallengeStoreConfig{}
	opt := WithRedisPassword("mypassword")
	opt(&cfg)
	assert.Equal(t, "mypassword", cfg.password)
}

func TestWithRedisDB(t *testing.T) {
	cfg := redisChallengeStoreConfig{}
	opt := WithRedisDB(2)
	opt(&cfg)
	assert.Equal(t, 2, cfg.db)
}

func TestWithRedisPoolSize(t *testing.T) {
	cfg := redisChallengeStoreConfig{}
	opt := WithRedisPoolSize(50)
	opt(&cfg)
	assert.Equal(t, 50, cfg.poolSize)
}

func TestWithRedisDialTimeout(t *testing.T) {
	cfg := redisChallengeStoreConfig{}
	opt := WithRedisDialTimeout(10 * time.Second)
	opt(&cfg)
	assert.Equal(t, 10*time.Second, cfg.dialTimeout)
}

func TestWithRedisReadTimeout(t *testing.T) {
	cfg := redisChallengeStoreConfig{}
	opt := WithRedisReadTimeout(5 * time.Second)
	opt(&cfg)
	assert.Equal(t, 5*time.Second, cfg.readTimeout)
}

func TestWithRedisWriteTimeout(t *testing.T) {
	cfg := redisChallengeStoreConfig{}
	opt := WithRedisWriteTimeout(5 * time.Second)
	opt(&cfg)
	assert.Equal(t, 5*time.Second, cfg.writeTimeout)
}

func TestNewRedisChallengeStore_ConnectionFailed(t *testing.T) {
	_, err := NewRedisChallengeStore("localhost:0", 5*time.Minute)
	assert.Error(t, err)
}

func TestNewRedisChallengeStore_WithOptions(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	store, err := NewRedisChallengeStore(mr.Addr(), 5*time.Minute,
		WithRedisPassword(""),
		WithRedisDB(0),
		WithRedisPoolSize(10),
		WithRedisDialTimeout(2*time.Second),
		WithRedisReadTimeout(1*time.Second),
		WithRedisWriteTimeout(1*time.Second),
	)
	require.NoError(t, err)
	defer store.Close()
}

func TestNewRedisChallengeStoreWithClient(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRedisChallengeStoreWithClient(client, 5*time.Minute)
	require.NotNil(t, store)
	defer store.Close()
}

func TestRedisChallengeStore_Close_NilClient(t *testing.T) {
	store := &RedisChallengeStore{client: nil}
	err := store.Close()
	assert.NoError(t, err)
}

func TestRedisChallengeStore_SaveChallenge_NilClient(t *testing.T) {
	store := &RedisChallengeStore{client: nil, ttl: 5 * time.Minute}
	ctx := context.Background()
	ch := &WalletChallenge{ID: "test"}
	assert.Panics(t, func() {
		_ = store.SaveChallenge(ctx, ch)
	})
}

func TestRedisChallengeStore_GetChallenge_InvalidJSON(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRedisChallengeStoreWithClient(client, 5*time.Minute)
	defer store.Close()

	ctx := context.Background()
	client.Set(ctx, "bad-json", "not-json-at-all", 5*time.Minute)

	_, err = store.GetChallenge(ctx, "bad-json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode challenge")
}

func TestRedisChallengeStore_MarkChallengeUsed_UnexpectedResult(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRedisChallengeStoreWithClient(client, 5*time.Minute)
	defer store.Close()

	ctx := context.Background()
	ch := &WalletChallenge{
		ID:        "unexpected-result",
		Nonce:     "n",
		Message:   "m",
		IssuedAt:  time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	require.NoError(t, store.SaveChallenge(ctx, ch))

	mr.Close()

	err = store.MarkChallengeUsed(ctx, ch.ID, time.Now().UTC())
	assert.Error(t, err)
}

func TestMemoryChallengeStore_Close(t *testing.T) {
	store := NewMemoryChallengeStore()
	err := store.Close()
	assert.NoError(t, err)
}

func TestMemoryChallengeStore_Close_DoubleClose(t *testing.T) {
	store := NewMemoryChallengeStore()
	err := store.Close()
	require.NoError(t, err)
	err = store.Close()
	assert.NoError(t, err)
}

func TestMemoryChallengeStore_EvictExpired(t *testing.T) {
	store := NewMemoryChallengeStore()
	defer store.Close()

	store.mu.Lock()
	store.challenges["expired-1"] = &WalletChallenge{
		ID:        "expired-1",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	store.challenges["valid-1"] = &WalletChallenge{
		ID:        "valid-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	store.mu.Unlock()

	store.evictExpired()

	store.mu.RLock()
	_, hasExpired := store.challenges["expired-1"]
	_, hasValid := store.challenges["valid-1"]
	store.mu.RUnlock()

	assert.False(t, hasExpired)
	assert.True(t, hasValid)
}

func TestMemoryChallengeStore_EvictExpired_AllValid(t *testing.T) {
	store := NewMemoryChallengeStore()
	defer store.Close()

	store.mu.Lock()
	store.challenges["valid-1"] = &WalletChallenge{
		ID:        "valid-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	store.mu.Unlock()

	store.evictExpired()

	store.mu.RLock()
	count := len(store.challenges)
	store.mu.RUnlock()

	assert.Equal(t, 1, count)
}

func TestMemoryChallengeStore_EvictExpired_Empty(t *testing.T) {
	store := NewMemoryChallengeStore()
	defer store.Close()

	assert.NotPanics(t, store.evictExpired)
}

func TestMemoryChallengeStore_SaveChallenge_ReturnsCopy(t *testing.T) {
	store := NewMemoryChallengeStore()
	defer store.Close()

	ctx := context.Background()
	ch := &WalletChallenge{
		ID:        "copy-test",
		Nonce:     "original",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	require.NoError(t, store.SaveChallenge(ctx, ch))

	got, err := store.GetChallenge(ctx, ch.ID)
	require.NoError(t, err)
	got.Nonce = "modified"

	original, err := store.GetChallenge(ctx, ch.ID)
	require.NoError(t, err)
	assert.Equal(t, "original", original.Nonce)
}

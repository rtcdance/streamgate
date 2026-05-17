package service

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

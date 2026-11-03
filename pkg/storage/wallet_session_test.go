package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func walletSessionFixture() *WalletSession {
	now := time.Now().UTC()
	return &WalletSession{Schema: 1, SessionID: "wallet-session", ProofID: "server-proof", Version: 1,
		Wallet: "0x1234567890123456789012345678901234567890", ChainID: 31337, Kind: "erc1271", AuthEpoch: 1, RestoreEpoch: "restore-1", CurrentJTI: "first-jti", Scheme: "siwe", Message: "admitted message", Signature: "0x010203", Digest: "digest", CodeHash: "code", BlockNumber: 17, BlockHash: "block", ObservedAt: now, AdmittedAt: now, AbsoluteExpiresAt: now.Add(2 * time.Hour), LeaseExpiresAt: now.Add(time.Minute)}
}

func newWalletSessionTestStores(t *testing.T) (*miniredis.Miniredis, *redis.Client, *RedisSessionStore, *RedisWalletSessionStore) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	sessions, err := NewRedisSessionStore(client)
	require.NoError(t, err)
	proofs, err := NewRedisWalletSessionStore(client)
	require.NoError(t, err)
	return server, client, sessions, proofs
}

func nextWalletSession(old *WalletSession, jti string) *WalletSession {
	next := *old
	next.Version++
	next.CurrentJTI = jti
	next.ObservedAt = old.ObservedAt.Add(time.Second)
	next.LeaseExpiresAt = next.ObservedAt.Add(time.Minute)
	return &next
}

func TestWalletSession_CreateAndLoadAcrossInstances(t *testing.T) {
	server, _, sessions, proofs := newWalletSessionTestStores(t)
	ctx := context.Background()
	record := walletSessionFixture()
	require.ErrorIs(t, proofs.Create(ctx, record), ErrWalletSessionConflict, "proof cannot create an epoch")
	require.NoError(t, sessions.Create(ctx, record.SessionID))
	require.NoError(t, proofs.Create(ctx, record))
	require.ErrorIs(t, proofs.Create(ctx, record), ErrWalletSessionConflict)
	client2 := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { require.NoError(t, client2.Close()) }()
	other, err := NewRedisWalletSessionStore(client2)
	require.NoError(t, err)
	loaded, err := other.Load(ctx, record.SessionID)
	require.NoError(t, err)
	require.Equal(t, record, loaded)
	next := nextWalletSession(loaded, "next-jti")
	require.NoError(t, other.CompareAndSwap(ctx, loaded, next))
	current, err := proofs.Load(ctx, record.SessionID)
	require.NoError(t, err)
	require.Equal(t, uint64(2), current.Version)
	require.Equal(t, "next-jti", current.CurrentJTI)
	require.True(t, current.AbsoluteExpiresAt.Equal(record.AbsoluteExpiresAt))
	require.ErrorIs(t, proofs.CompareAndSwap(ctx, loaded, nextWalletSession(loaded, "replayed-jti")), ErrWalletSessionConflict)
}

func TestWalletSession_ConcurrentRefreshOnlyOneWinner(t *testing.T) {
	_, _, sessions, proofs := newWalletSessionTestStores(t)
	ctx := context.Background()
	record := walletSessionFixture()
	require.NoError(t, sessions.Create(ctx, record.SessionID))
	require.NoError(t, proofs.Create(ctx, record))
	start := make(chan struct{})
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			results <- proofs.CompareAndSwap(ctx, record, nextWalletSession(record, fmt.Sprintf("new-%d", index)))
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, ErrWalletSessionConflict)
		}
	}
	require.Equal(t, 1, winners)
}

func TestWalletSession_LogoutAndMissingEpochCannotResurrect(t *testing.T) {
	server, _, sessions, proofs := newWalletSessionTestStores(t)
	ctx := context.Background()
	record := walletSessionFixture()
	require.NoError(t, sessions.Create(ctx, record.SessionID))
	require.NoError(t, proofs.Create(ctx, record))
	epoch, err := sessions.Bump(ctx, record.SessionID)
	require.NoError(t, err)
	require.Equal(t, int64(2), epoch)
	require.ErrorIs(t, proofs.CompareAndSwap(ctx, record, nextWalletSession(record, "late-success")), ErrWalletSessionConflict)
	server.Del(sessionStorageKey(record.SessionID))
	require.ErrorIs(t, proofs.CompareAndSwap(ctx, record, nextWalletSession(record, "missing-epoch")), ErrWalletSessionConflict)
	_, err = sessions.Bump(ctx, record.SessionID)
	require.Error(t, err)
	require.False(t, server.Exists(sessionStorageKey(record.SessionID)))
}

func TestWalletSession_ImmutableAdmissionAndLifetime(t *testing.T) {
	_, _, sessions, proofs := newWalletSessionTestStores(t)
	ctx := context.Background()
	record := walletSessionFixture()
	require.NoError(t, sessions.Create(ctx, record.SessionID))
	require.NoError(t, proofs.Create(ctx, record))
	tests := []struct {
		name   string
		change func(*WalletSession)
	}{
		{"wallet", func(r *WalletSession) { r.Wallet = "different" }},
		{"chain", func(r *WalletSession) { r.ChainID = 1 }},
		{"restore", func(r *WalletSession) { r.RestoreEpoch = "restore-2" }},
		{"epoch", func(r *WalletSession) { r.AuthEpoch = 2 }},
		{"proof", func(r *WalletSession) { r.ProofID = "client-provided" }},
		{"signature", func(r *WalletSession) { r.Signature = "0xdead" }},
		{"message", func(r *WalletSession) { r.Message = "new nonce" }},
		{"digest", func(r *WalletSession) { r.Digest = "another digest" }},
		{"absolute", func(r *WalletSession) { r.AbsoluteExpiresAt = r.AbsoluteExpiresAt.Add(time.Hour) }},
		{"version", func(r *WalletSession) { r.Version++ }},
		{"same jti", func(r *WalletSession) { r.CurrentJTI = record.CurrentJTI }},
		{"rejuvenated time", func(r *WalletSession) { r.ObservedAt = record.ObservedAt.Add(-time.Second) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := nextWalletSession(record, "candidate")
			tt.change(next)
			require.Error(t, proofs.CompareAndSwap(ctx, record, next))
			loaded, err := proofs.Load(ctx, record.SessionID)
			require.NoError(t, err)
			require.Equal(t, record, loaded)
		})
	}
}

func TestWalletSession_MissingCorruptExpiredAndUnavailable(t *testing.T) {
	server, _, sessions, proofs := newWalletSessionTestStores(t)
	ctx := context.Background()
	record := walletSessionFixture()
	_, err := proofs.Load(ctx, "absent")
	require.ErrorIs(t, err, ErrWalletSessionMissing)
	require.NoError(t, server.Set(walletSessionKey(record.SessionID), "{broken"))
	_, err = proofs.Load(ctx, record.SessionID)
	require.ErrorIs(t, err, ErrWalletSessionMissing)
	server.Del(walletSessionKey(record.SessionID))
	require.NoError(t, sessions.Create(ctx, record.SessionID))
	require.NoError(t, proofs.Create(ctx, record))
	server.FastForward(2*time.Hour + time.Second)
	_, err = proofs.Load(ctx, record.SessionID)
	require.ErrorIs(t, err, ErrWalletSessionMissing)
	server.Close()
	_, err = proofs.Load(ctx, record.SessionID)
	require.ErrorIs(t, err, ErrWalletSessionUnavailable)
}

func TestMemorySessionStore_CreateCannotResetRevokedEpoch(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()
	require.NoError(t, store.Create(ctx, "session"))
	epoch, err := store.Bump(ctx, "session")
	require.NoError(t, err)
	require.Equal(t, int64(2), epoch)
	require.Error(t, store.Create(ctx, "session"))
	epoch, ok := store.Epoch(ctx, "session")
	require.True(t, ok)
	require.Equal(t, int64(2), epoch)
	_, err = NewRedisWalletSessionStore(nil)
	require.True(t, errors.Is(err, ErrWalletSessionUnavailable))
}

// Expiry between a separate GET and INCR must never return epoch 1. This hook
// simulates precisely that interleaving; atomic Lua has no exposed INCR command.
type expireBeforeIncrement struct {
	server *miniredis.Miniredis
	key    string
}

func (h expireBeforeIncrement) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if cmd.Name() == "incr" {
		h.server.Del(h.key)
	}
	return ctx, nil
}
func (h expireBeforeIncrement) AfterProcess(context.Context, redis.Cmder) error { return nil }
func (h expireBeforeIncrement) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (h expireBeforeIncrement) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func TestRedisSessionStore_BumpCannotResetEpochAcrossExpiry(t *testing.T) {
	server, client, sessions, _ := newWalletSessionTestStores(t)
	ctx := context.Background()
	require.NoError(t, sessions.Create(ctx, "race"))
	client.AddHook(expireBeforeIncrement{server: server, key: sessionStorageKey("race")})
	epoch, err := sessions.Bump(ctx, "race")
	require.NoError(t, err)
	require.Equal(t, int64(2), epoch, "revocation must never recreate the session at epoch 1")
}

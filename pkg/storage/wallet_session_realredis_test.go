//go:build integration

package storage

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// realRedisAddr returns the durability profile's real Redis address. Unlike a
// unit fixture this is never skipped: the durability gate provisions the
// service, and an unset address is a configuration failure, not a pass.
func realRedisAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("W3_REDIS_ADDR")
	if addr == "" {
		t.Fatal("W3_REDIS_ADDR is required: this suite proves the wallet-proof store on a real Redis, and running it without one would be a skipped gate, not evidence")
	}
	return addr
}

func newRealWalletSessionStores(t *testing.T) (client *redis.Client, sessions *RedisSessionStore, proof, peerProof *RedisWalletSessionStore) {
	t.Helper()
	addr := realRedisAddr(t)
	newClient := func() *redis.Client {
		client := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second})
		t.Cleanup(func() { _ = client.Close() })
		require.NoError(t, client.Ping(context.Background()).Err())
		return client
	}
	client1, client2 := newClient(), newClient()
	sessions1, err := NewRedisSessionStore(client1)
	require.NoError(t, err)
	proofs1, err := NewRedisWalletSessionStore(client1)
	require.NoError(t, err)
	proofs2, err := NewRedisWalletSessionStore(client2)
	require.NoError(t, err)
	return client1, sessions1, proofs1, proofs2
}

// TestWalletSession_RealRedisDurability proves the CW-12 storage semantics
// (plan §10.6) on a REAL Redis server — the miniredis suite is a Lua
// simulation and is deliberately not a substitute: cross-instance reads, the
// one-winner CAS renewal under real atomicity, immutable admission fields,
// epoch interplay, and the fail-closed behavior after proof data loss.
func TestWalletSession_RealRedisDurability(t *testing.T) {
	client1, sessions1, proofs1, proofs2 := newRealWalletSessionStores(t)
	ctx := context.Background()
	record := walletSessionFixture()
	record.SessionID = "wallet-durability-" + uuid.NewString()
	freshID := record.SessionID + "-after-loss"
	unrelatedKey := "wallet-durability-unrelated-" + uuid.NewString()
	t.Cleanup(func() {
		_ = client1.Del(context.Background(), sessionStorageKey(record.SessionID), walletSessionKey(record.SessionID), sessionStorageKey(freshID), walletSessionKey(freshID), unrelatedKey).Err()
	})
	require.NoError(t, client1.Set(ctx, unrelatedKey, "must-survive-proof-loss", time.Minute).Err())

	// Creation on one client must be readable by an independent client — the
	// "正常未失效记录能跨进程读取" requirement.
	require.NoError(t, sessions1.Create(ctx, record.SessionID))
	require.NoError(t, proofs1.Create(ctx, record))
	loaded, err := proofs2.Load(ctx, record.SessionID)
	require.NoError(t, err)
	require.Equal(t, record, loaded)

	// Immutable admission identity and lifetime under a real Lua CAS.
	for _, tt := range []struct {
		name   string
		change func(*WalletSession)
	}{
		{"wallet", func(r *WalletSession) { r.Wallet = "0xdead" }},
		{"chain", func(r *WalletSession) { r.ChainID = 1 }},
		{"proof", func(r *WalletSession) { r.ProofID = "client-provided" }},
		{"signature", func(r *WalletSession) { r.Signature = "0xdead" }},
		{"absolute", func(r *WalletSession) { r.AbsoluteExpiresAt = r.AbsoluteExpiresAt.Add(time.Hour) }},
		{"version", func(r *WalletSession) { r.Version++ }},
	} {
		t.Run("immutable/"+tt.name, func(t *testing.T) {
			next := nextWalletSession(record, "candidate")
			tt.change(next)
			require.Error(t, proofs2.CompareAndSwap(ctx, record, next))
			reloaded, err := proofs1.Load(ctx, record.SessionID)
			require.NoError(t, err)
			require.Equal(t, record, reloaded)
		})
	}

	// Concurrent renewal on real Redis must admit exactly one winner.
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			results <- proofs2.CompareAndSwap(ctx, record, nextWalletSession(record, fmt.Sprintf("real-%d", index)))
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

	// Epoch interplay: after a logout bump, the stale view cannot renew, and a
	// proof store whose session epoch vanished cannot resurrect either.
	epoch, err := sessions1.Bump(ctx, record.SessionID)
	require.NoError(t, err)
	require.Equal(t, int64(2), epoch)
	require.ErrorIs(t, proofs2.CompareAndSwap(ctx, record, nextWalletSession(record, "stale-view")), ErrWalletSessionConflict)

	// Proof data loss (restart semantics, emulated at the data layer by deleting only this test
	// session and proof): every dependent read and renewal must FAIL CLOSED — no fallback to
	// an unproven session, no resurrection of the admitted record.
	require.NoError(t, client1.Del(ctx, sessionStorageKey(record.SessionID), walletSessionKey(record.SessionID)).Err())
	untouched, err := client1.Get(ctx, unrelatedKey).Result()
	require.NoError(t, err)
	require.Equal(t, "must-survive-proof-loss", untouched)
	_, err = proofs2.Load(ctx, record.SessionID)
	require.Error(t, err)
	require.ErrorIs(t, proofs1.CompareAndSwap(ctx, record, nextWalletSession(record, "after-loss")), ErrWalletSessionConflict)

	// A fresh admission after the loss is a NEW login, not a resurrection: the
	// previous record's identity stays gone.
	fresh := walletSessionFixture()
	fresh.SessionID = freshID
	fresh.CurrentJTI = "fresh-jti"
	require.NoError(t, sessions1.Create(ctx, fresh.SessionID))
	require.NoError(t, proofs1.Create(ctx, fresh))
	revived, err := proofs2.Load(ctx, record.SessionID)
	require.Error(t, err)
	require.Nil(t, revived)
	got, err := proofs2.Load(ctx, fresh.SessionID)
	require.NoError(t, err)
	require.Equal(t, fresh, got)
}

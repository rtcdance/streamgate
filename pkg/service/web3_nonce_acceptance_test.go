package service

// SG-13 A.3 acceptance: ONE (chain, signer) must have ONE nonce authority. A
// Web3Service nonce path (the SendTransaction allocator) and the registry
// publisher's allocator draw from the SAME Postgres tx.NonceLedger, so under
// concurrency they can never return the same nonce — the failure mode that
// displaced the dev-mint transaction in the live fullchain run (mint receipt
// never appears because another allocator signed the same nonce).
//
// Runs against a SCRATCH Postgres: point STREAMGATE_TEST_DB_DSN at one. The
// test deletes only its own rows (scoped to chain id 991338 — deliberately
// different from the registry package's 991337, because go test runs packages
// in parallel against the same database).

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/rtcdance/streamgate/pkg/service/registry"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"
	txtx "github.com/rtcdance/streamgate/pkg/web3/tx"
	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	sharedAuthChainID  int64 = 991338
	sharedAuthSigner         = "0x51337000000000000000000000000000000000f1"
	sharedAuthRegistry       = "0x00000000000000000000000000000000000REG"
	sharedAuthPending        = uint64(7)
)

// fixedChainPending is the fake chain client: its pending nonce is the floor
// every first-use priming consults.
type fixedChainPending struct{}

func (fixedChainPending) GetNonce(context.Context, string) (uint64, error) {
	return sharedAuthPending, nil
}

// mutableChainPending lets a test move the fake chain's pending nonce.
type mutableChainPending struct{ n uint64 }

func (c *mutableChainPending) GetNonce(context.Context, string) (uint64, error) {
	return c.n, nil
}

// TestAcceptance_ChainNonceFloor pins the live-fullchain failure (SG-13 B T9):
// a DB ledger that falls BEHIND the chain (writes that never recorded a
// reservation) must not keep handing out already-consumed nonces — the chain's
// pending nonce is a floor on EVERY allocation, not only the first. Before the
// fix the ledger handed out max(DB)+1 forever and every mint died with
// "nonce too low" while the chain nonce kept rising.
func TestAcceptance_ChainNonceFloor(t *testing.T) {
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("STREAMGATE_TEST_DB_DSN not set; SG-13 shared-nonce tests need a scratch Postgres")
	}
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	if err := db.Ping(context.Background()); err != nil {
		t.Skipf("test DB unreachable: %v", err)
	}
	require.NoError(t, helpers.ApplyMigrations(t, db))

	ctx := context.Background()
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM tx_nonce_reservations WHERE chain_id = $1`,
			`DELETE FROM tx_signed_attempts WHERE chain_id = $1`,
		} {
			if _, err := db.Exec(ctx, q, sharedAuthChainID); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	chain := &mutableChainPending{n: 100}
	ledger := txtx.NewNonceLedger(db, sharedAuthChainID, chain, zap.NewNop())

	first, err := ledger.Reserve(ctx, sharedAuthSigner)
	require.NoError(t, err)
	assert.Equal(t, uint64(100), first, "first allocation is primed with the chain pending nonce")

	// The chain advances without the ledger recording anything (a process
	// crashed after broadcast, or an older binary wrote directly). The next
	// allocation must jump to the chain floor instead of re-issuing 101.
	chain.n = 250
	next, err := ledger.Reserve(ctx, sharedAuthSigner)
	require.NoError(t, err)
	assert.Equal(t, uint64(250), next,
		"chain pending nonce must floor EVERY allocation, not just the first")

	// A second ledger instance (a freshly started process) sees the same
	// floor — no per-process warm-up window where a stale nonce can be signed.
	otherProcess := txtx.NewNonceLedger(db, sharedAuthChainID, chain, zap.NewNop())
	next2, err := otherProcess.Reserve(ctx, sharedAuthSigner)
	require.NoError(t, err)
	assert.Equal(t, uint64(251), next2)
}

func TestAcceptance_SharedNonceAuthority(t *testing.T) {
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("STREAMGATE_TEST_DB_DSN not set; SG-13 shared-nonce tests need a scratch Postgres")
	}
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err)
	if err := db.Ping(context.Background()); err != nil {
		t.Skipf("test DB unreachable: %v", err)
	}
	require.NoError(t, helpers.ApplyMigrations(t, db))

	ctx := context.Background()
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM tx_nonce_reservations WHERE chain_id = $1`,
			`DELETE FROM tx_signed_attempts WHERE chain_id = $1`,
		} {
			if _, err := db.Exec(ctx, q, sharedAuthChainID); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	// Two SEPARATE ledger instances over the SAME database — exactly like
	// production, where the gateway ledger and the registry Store's ledger are
	// different objects sharing one table. Uniqueness must come from the DB
	// (advisory lock + PRIMARY KEY), not from object identity.
	web3Ledger := txtx.NewNonceLedger(db, sharedAuthChainID, fixedChainPending{}, zap.NewNop())
	ws := &Web3Service{
		logger:        zap.NewNop(),
		nonceManagers: make(map[int64]web3.NonceProvider),
		nonceLedger:   web3Ledger,
	}
	registryStore := registry.NewStore(db, sharedAuthChainID, sharedAuthRegistry)

	const perAllocator = 20
	var wg sync.WaitGroup
	nonces := make(chan uint64, perAllocator*2)
	errs := make(chan error, perAllocator*2)
	for i := 0; i < perAllocator; i++ {
		wg.Add(2)
		// The Web3Service nonce path (what SendTransaction resolves).
		go func() {
			defer wg.Done()
			n, err := ws.getNonceManager(sharedAuthChainID, nil).NextNonce(ctx, sharedAuthSigner)
			if err != nil {
				errs <- err
				return
			}
			nonces <- n
		}()
		// The registry publisher's allocation (its ReserveNonce entry point).
		go func() {
			defer wg.Done()
			n, err := registryStore.ReserveNonce(ctx, sharedAuthSigner, sharedAuthPending)
			if err != nil {
				errs <- err
				return
			}
			nonces <- n
		}()
	}
	wg.Wait()
	close(nonces)
	close(errs)
	for err := range errs {
		require.NoError(t, err, "every allocation must succeed against the shared ledger")
	}

	got := make([]uint64, 0, perAllocator*2)
	seen := make(map[uint64]bool)
	for n := range nonces {
		assert.False(t, seen[n], "duplicate nonce %d allocated by the two authorities", n)
		seen[n] = true
		got = append(got, n)
	}
	require.Len(t, got, perAllocator*2, "both allocators must yield every nonce")

	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	assert.Equal(t, sharedAuthPending, got[0], "first allocation is primed with the chain pending nonce")
	for i := 1; i < len(got); i++ {
		require.Equal(t, got[i-1]+1, got[i],
			"allocations must be monotonic and gapless: %d then %d", got[i-1], got[i])
	}

	var rows int
	require.NoError(t, db.QueryRow(ctx,
		`SELECT COUNT(*) FROM tx_nonce_reservations WHERE chain_id = $1 AND signer = $2`,
		sharedAuthChainID, sharedAuthSigner).Scan(&rows))
	assert.Equal(t, perAllocator*2, rows, "every nonce must be persisted exactly once")
}

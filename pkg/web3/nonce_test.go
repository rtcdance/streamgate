package web3

import (
	"fmt"
	"sync"
	"testing"

	"go.uber.org/zap"
)

func TestNonceManager_SequentialIncrement(t *testing.T) {
	logger := zap.NewNop()
	nm := &NonceManager{
		client: nil,
		logger: logger,
		cached: make(map[string]uint64),
	}

	// Pre-populate cached nonce
	addr := "0x1234"
	nm.cached[addr] = 10

	// Simulate sequential nonce increments
	n1 := nextCachedNonce(nm, addr)
	n2 := nextCachedNonce(nm, addr)
	n3 := nextCachedNonce(nm, addr)

	if n1 != 10 || n2 != 11 || n3 != 12 {
		t.Errorf("expected 10,11,12 got %d,%d,%d", n1, n2, n3)
	}
}

func TestNonceManager_ConcurrentIncrement(t *testing.T) {
	logger := zap.NewNop()
	nm := &NonceManager{
		client: nil,
		logger: logger,
		cached: map[string]uint64{"0xaddr": 0},
	}

	var wg sync.WaitGroup
	seen := make(map[uint64]bool)
	var mu sync.Mutex
	var dupes int

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := nextCachedNonce(nm, "0xaddr")
			mu.Lock()
			if seen[n] {
				dupes++
			}
			seen[n] = true
			mu.Unlock()
		}()
	}
	wg.Wait()

	if dupes > 0 {
		t.Errorf("found %d duplicate nonces under concurrent access", dupes)
	}
	if len(seen) != 100 {
		t.Errorf("expected 100 unique nonces, got %d", len(seen))
	}
}

func TestNonceManager_Reset(t *testing.T) {
	logger := zap.NewNop()
	nm := &NonceManager{
		client: nil,
		logger: logger,
		cached: map[string]uint64{"0xaddr": 42},
	}

	nm.Reset("0xaddr")
	nm.mu.Lock()
	_, exists := nm.cached["0xaddr"]
	nm.mu.Unlock()
	if exists {
		t.Error("Reset should remove cached nonce")
	}
}

func TestNonceManager_ResetAll(t *testing.T) {
	logger := zap.NewNop()
	nm := &NonceManager{
		client: nil,
		logger: logger,
		cached: map[string]uint64{"0xa": 1, "0xb": 2},
	}

	nm.ResetAll()
	nm.mu.Lock()
	count := len(nm.cached)
	nm.mu.Unlock()
	if count != 0 {
		t.Errorf("ResetAll should clear all, got %d remaining", count)
	}
}

// nextCachedNonce returns the next cached nonce without querying the network.
// This is a test helper that accesses the NonceManager's internal cache.
func nextCachedNonce(nm *NonceManager, address string) uint64 {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	next, ok := nm.cached[address]
	if !ok {
		panic(fmt.Sprintf("nonce not cached for %s", address))
	}
	nm.cached[address] = next + 1
	return next
}

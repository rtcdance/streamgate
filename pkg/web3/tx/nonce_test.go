package tx

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
		states: make(map[string]*nonceState),
	}

	addr := "0x1234"
	nm.states[addr] = &nonceState{
		next:    10,
		pending: make(map[uint64]struct{}),
	}

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
		states: map[string]*nonceState{
			"0xaddr": {
				next:    0,
				pending: make(map[uint64]struct{}),
			},
		},
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
		states: map[string]*nonceState{
			"0xaddr": {
				next:    42,
				pending: make(map[uint64]struct{}),
			},
		},
	}

	nm.Reset("0xaddr")
	nm.mu.Lock()
	_, exists := nm.states["0xaddr"]
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
		states: map[string]*nonceState{
			"0xa": {next: 1, pending: make(map[uint64]struct{})},
			"0xb": {next: 2, pending: make(map[uint64]struct{})},
		},
	}

	nm.ResetAll()
	nm.mu.Lock()
	count := len(nm.states)
	nm.mu.Unlock()
	if count != 0 {
		t.Errorf("ResetAll should clear all, got %d remaining", count)
	}
}

func TestNonceManager_Rollback_GapFill(t *testing.T) {
	logger := zap.NewNop()
	nm := &NonceManager{
		client: nil,
		logger: logger,
		states: make(map[string]*nonceState),
	}

	addr := "0xaddr"
	nm.states[addr] = &nonceState{
		next:    10,
		pending: make(map[uint64]struct{}),
	}

	n1 := nextCachedNonce(nm, addr)
	n2 := nextCachedNonce(nm, addr)
	n3 := nextCachedNonce(nm, addr)

	if n1 != 10 || n2 != 11 || n3 != 12 {
		t.Fatalf("expected 10,11,12 got %d,%d,%d", n1, n2, n3)
	}

	nm.Rollback(addr, 11)

	st := nm.states[addr]
	if _, ok := st.pending[11]; !ok {
		t.Error("rolled back nonce 11 should be in pending")
	}
	if st.next != 13 {
		t.Errorf("next should still be 13, got %d", st.next)
	}

	n4 := nextCachedNonce(nm, addr)
	if n4 != 11 {
		t.Errorf("should fill gap with nonce 11, got %d", n4)
	}

	n5 := nextCachedNonce(nm, addr)
	if n5 != 13 {
		t.Errorf("should continue with nonce 13, got %d", n5)
	}
}

func TestNonceManager_Rollback_DecrementNext(t *testing.T) {
	logger := zap.NewNop()
	nm := &NonceManager{
		client: nil,
		logger: logger,
		states: make(map[string]*nonceState),
	}

	addr := "0xaddr"
	nm.states[addr] = &nonceState{
		next:    10,
		pending: make(map[uint64]struct{}),
	}

	n1 := nextCachedNonce(nm, addr)
	if n1 != 10 {
		t.Fatalf("expected 10, got %d", n1)
	}

	nm.Rollback(addr, 10)

	st := nm.states[addr]
	if st.next != 10 {
		t.Errorf("next should be decremented to 10, got %d", st.next)
	}

	n2 := nextCachedNonce(nm, addr)
	if n2 != 10 {
		t.Errorf("should re-use rolled back nonce 10, got %d", n2)
	}
}

func TestNonceManager_Rollback_MultipleGaps(t *testing.T) {
	logger := zap.NewNop()
	nm := &NonceManager{
		client: nil,
		logger: logger,
		states: make(map[string]*nonceState),
	}

	addr := "0xaddr"
	nm.states[addr] = &nonceState{
		next:    10,
		pending: make(map[uint64]struct{}),
	}

	nextCachedNonce(nm, addr)
	nextCachedNonce(nm, addr)
	nextCachedNonce(nm, addr)
	nextCachedNonce(nm, addr)

	nm.Rollback(addr, 11)
	nm.Rollback(addr, 13)

	st := nm.states[addr]
	if len(st.pending) != 1 {
		t.Errorf("expected 1 pending nonce, got %d", len(st.pending))
	}
	if st.next != 13 {
		t.Errorf("next should be 13 after rollback 13 decremented it, got %d", st.next)
	}

	n4 := nextCachedNonce(nm, addr)
	if n4 != 11 {
		t.Errorf("should fill smallest gap nonce 11, got %d", n4)
	}

	n5 := nextCachedNonce(nm, addr)
	if n5 != 13 {
		t.Errorf("should allocate nonce 13, got %d", n5)
	}

	n6 := nextCachedNonce(nm, addr)
	if n6 != 14 {
		t.Errorf("should allocate fresh nonce 14, got %d", n6)
	}
}

func TestNonceManager_Rollback_CollapseChain(t *testing.T) {
	logger := zap.NewNop()
	nm := &NonceManager{
		client: nil,
		logger: logger,
		states: make(map[string]*nonceState),
	}

	addr := "0xaddr"
	nm.states[addr] = &nonceState{
		next:    10,
		pending: make(map[uint64]struct{}),
	}

	nextCachedNonce(nm, addr)
	nextCachedNonce(nm, addr)
	nextCachedNonce(nm, addr)

	nm.Rollback(addr, 11)
	nm.Rollback(addr, 12)

	st := nm.states[addr]
	if st.next != 11 {
		t.Errorf("next should collapse to 11, got %d", st.next)
	}
	if len(st.pending) != 0 {
		t.Errorf("pending should be empty after collapse, got %d", len(st.pending))
	}
}

func nextCachedNonce(nm *NonceManager, address string) uint64 {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	st, ok := nm.states[address]
	if !ok {
		panic(fmt.Sprintf("nonce not cached for %s", address))
	}

	if len(st.pending) > 0 {
		var smallest uint64
		found := false
		for n := range st.pending {
			if !found || n < smallest {
				smallest = n
				found = true
			}
		}
		if found {
			delete(st.pending, smallest)
			return smallest
		}
	}

	nonce := st.next
	st.next++
	return nonce
}

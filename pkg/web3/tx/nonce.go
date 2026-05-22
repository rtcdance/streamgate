package tx

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

const defaultSyncInterval = 60 * time.Second

type NonceProvider interface {
	NextNonce(ctx context.Context, address string) (uint64, error)
	Rollback(address string, nonce uint64)
	Reset(address string)
}

type NonceClient interface {
	GetNonce(ctx context.Context, address string) (uint64, error)
}

type nonceState struct {
	next    uint64
	pending map[uint64]struct{}
}

type NonceManager struct {
	client NonceClient
	logger *zap.Logger

	mu           sync.Mutex
	states       map[string]*nonceState
	lastSync     map[string]time.Time
	syncInterval time.Duration
	evictTTL     time.Duration
}

func NewNonceManager(client NonceClient, logger *zap.Logger) *NonceManager {
	return &NonceManager{
		client:       client,
		logger:       logger,
		states:       make(map[string]*nonceState),
		lastSync:     make(map[string]time.Time),
		syncInterval: defaultSyncInterval,
		evictTTL:     30 * time.Minute,
	}
}

func (nm *NonceManager) NextNonce(ctx context.Context, address string) (uint64, error) {
	nm.mu.Lock()
	nm.evictStaleLocked()

	_, ok := nm.states[address]
	needSync := !ok || (nm.lastSync[address].Before(time.Now().Add(-nm.syncInterval)))
	nm.mu.Unlock()

	if needSync {
		netNonce, err := nm.client.GetNonce(ctx, address)
		if err != nil {
			if !ok {
				return 0, fmt.Errorf("nonce manager: failed to get network nonce: %w", err)
			}
			nm.logger.Warn("NonceManager: failed to re-sync nonce from network, using cached",
				zap.String("address", address),
				zap.Error(err))
		} else {
			nm.mu.Lock()
			if st2, ok2 := nm.states[address]; ok2 {
				if netNonce > st2.next {
					st2.next = netNonce
				}
				for n := range st2.pending {
					if n < st2.next {
						delete(st2.pending, n)
					}
				}
			} else {
				nm.states[address] = &nonceState{
					next:    netNonce + 1,
					pending: make(map[uint64]struct{}),
				}
			}
			nm.lastSync[address] = time.Now()
			nm.logger.Debug("NonceManager: synced nonce from network",
				zap.String("address", address),
				zap.Uint64("nonce", netNonce))
			nm.mu.Unlock()

			if !ok {
				return netNonce, nil
			}
		}
	}

	nm.mu.Lock()
	defer nm.mu.Unlock()

	st := nm.states[address]
	if st == nil {
		netNonce, err := nm.client.GetNonce(ctx, address)
		if err != nil {
			return 0, fmt.Errorf("nonce manager: failed to get network nonce: %w", err)
		}
		nm.states[address] = &nonceState{
			next:    netNonce + 1,
			pending: make(map[uint64]struct{}),
		}
		nm.lastSync[address] = time.Now()
		return netNonce, nil
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
			nm.logger.Debug("NonceManager: filled gap nonce",
				zap.String("address", address),
				zap.Uint64("nonce", smallest))
			return smallest, nil
		}
	}

	nonce := st.next
	st.next++
	nm.logger.Debug("NonceManager: returned cached nonce",
		zap.String("address", address),
		zap.Uint64("nonce", nonce))
	return nonce, nil
}

func (nm *NonceManager) Rollback(address string, nonce uint64) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	st, ok := nm.states[address]
	if !ok {
		return
	}

	if nonce == st.next-1 {
		st.next = nonce
		delete(st.pending, nonce)
		for n := range st.pending {
			if n == st.next-1 {
				st.next = n
				delete(st.pending, n)
			}
		}
	} else {
		st.pending[nonce] = struct{}{}
	}

	nm.logger.Debug("NonceManager: rolled back nonce",
		zap.String("address", address),
		zap.Uint64("nonce", nonce))
}

func (nm *NonceManager) evictStaleLocked() {
	if nm.evictTTL <= 0 {
		return
	}
	now := time.Now()
	for addr, last := range nm.lastSync {
		if now.Sub(last) > nm.evictTTL {
			delete(nm.states, addr)
			delete(nm.lastSync, addr)
		}
	}
}

func (nm *NonceManager) Reset(address string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	delete(nm.states, address)
	delete(nm.lastSync, address)
	nm.logger.Debug("NonceManager: reset cached nonce",
		zap.String("address", address))
}

func (nm *NonceManager) ResetAddresses(addresses []string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	for _, addr := range addresses {
		delete(nm.states, addr)
		delete(nm.lastSync, addr)
	}
	nm.logger.Info("NonceManager: reset nonce state after reorg",
		zap.Int("address_count", len(addresses)))
}

func (nm *NonceManager) ResetAll() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.states = make(map[string]*nonceState)
	nm.lastSync = make(map[string]time.Time)
	nm.logger.Debug("NonceManager: reset all cached nonces")
}

package web3

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// defaultSyncInterval is how often the cached nonce is re-synced from the
// network even if no error occurred. This prevents permanent nonce drift
// when an external signer uses the same address.
const defaultSyncInterval = 60 * time.Second

// NonceProvider abstracts nonce management for testability.
// Consumers should depend on this interface rather than *NonceManager directly.
type NonceProvider interface {
	NextNonce(ctx context.Context, address string) (uint64, error)
	Rollback(address string, nonce uint64)
	Reset(address string)
}

// nonceClient is the narrow ChainClient dependency needed by NonceManager.
type nonceClient interface {
	GetNonce(ctx context.Context, address string) (uint64, error)
}

// NonceManager manages local nonce tracking for concurrent transaction sending.
// It avoids querying the RPC for every send by caching the last-used nonce and
// incrementing atomically. On restart or gap detection, it re-syncs from the
// network via PendingNonceAt.
type NonceManager struct {
	client nonceClient
	logger *zap.Logger

	mu           sync.Mutex
	cached       map[string]uint64    // address → next nonce to use
	lastSync     map[string]time.Time // address → last network sync time
	syncInterval time.Duration
	evictTTL     time.Duration // addresses idle longer than this are evicted
}

// NewNonceManager creates a new NonceManager backed by the given nonceClient.
func NewNonceManager(client nonceClient, logger *zap.Logger) *NonceManager {
	return &NonceManager{
		client:       client,
		logger:       logger,
		cached:       make(map[string]uint64),
		lastSync:     make(map[string]time.Time),
		syncInterval: defaultSyncInterval,
		evictTTL:     30 * time.Minute, // evict addresses idle for 30 minutes
	}
}

// NextNonce returns the next nonce to use for the given address.
// On the first call for an address it fetches the pending nonce from the
// network. Subsequent calls increment the local counter atomically.
// If more than syncInterval has passed since the last network sync, it
// re-syncs to detect external transactions that may have consumed a nonce.
func (nm *NonceManager) NextNonce(ctx context.Context, address string) (uint64, error) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// Periodically evict stale entries to prevent unbounded memory growth.
	nm.evictStaleLocked()

	if next, ok := nm.cached[address]; ok {
		// Periodic re-sync: if it's been too long since last network sync,
		// refresh from chain to detect external nonce consumption.
		if last, ok := nm.lastSync[address]; ok && time.Since(last) > nm.syncInterval {
			netNonce, err := nm.client.GetNonce(ctx, address)
			if err != nil {
				// Network error — return cached nonce as best-effort
				nm.logger.Warn("NonceManager: failed to re-sync nonce from network, using cached",
					zap.String("address", address),
					zap.Error(err))
			} else {
				// Use the higher of cached vs network to avoid "nonce too low"
				if netNonce > next {
					next = netNonce
				}
				nm.cached[address] = next + 1
				nm.lastSync[address] = time.Now()
				nm.logger.Debug("NonceManager: re-synced nonce from network",
					zap.String("address", address),
					zap.Uint64("nonce", next))
				return next, nil
			}
		}

		nm.cached[address] = next + 1
		nm.logger.Debug("NonceManager: returned cached nonce",
			zap.String("address", address),
			zap.Uint64("nonce", next))
		return next, nil
	}

	// First call — sync from network
	netNonce, err := nm.client.GetNonce(ctx, address)
	if err != nil {
		return 0, fmt.Errorf("nonce manager: failed to get network nonce: %w", err)
	}
	nm.cached[address] = netNonce + 1
	nm.lastSync[address] = time.Now()
	nm.logger.Debug("NonceManager: synced nonce from network",
		zap.String("address", address),
		zap.Uint64("nonce", netNonce))
	return netNonce, nil
}

// Rollback decrements the cached nonce for the given address after a
// failed transaction send. This prevents nonce gaps that would block all
// subsequent transactions. The nonce parameter is the nonce that was
// consumed but never actually used on-chain.
func (nm *NonceManager) Rollback(address string, nonce uint64) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if next, ok := nm.cached[address]; ok && next == nonce+1 {
		nm.cached[address] = nonce
		nm.logger.Debug("NonceManager: rolled back nonce",
			zap.String("address", address),
			zap.Uint64("nonce", nonce))
	}
}

// evictStaleLocked removes entries not accessed within the eviction TTL.
// Must be called with nm.mu held.
func (nm *NonceManager) evictStaleLocked() {
	if nm.evictTTL <= 0 {
		return
	}
	now := time.Now()
	for addr, last := range nm.lastSync {
		if now.Sub(last) > nm.evictTTL {
			delete(nm.cached, addr)
			delete(nm.lastSync, addr)
		}
	}
}

// Reset clears the cached nonce for the given address, forcing a network
// re-sync on the next call. Call this after a transaction fails with a
// nonce-related error (e.g. "nonce too low" or "nonce too high").
func (nm *NonceManager) Reset(address string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	delete(nm.cached, address)
	delete(nm.lastSync, address)
	nm.logger.Debug("NonceManager: reset cached nonce",
		zap.String("address", address))
}

// ResetAll clears all cached nonces.
func (nm *NonceManager) ResetAll() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.cached = make(map[string]uint64)
	nm.lastSync = make(map[string]time.Time)
	nm.logger.Debug("NonceManager: reset all cached nonces")
}

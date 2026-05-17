package storage

import (
	"context"
	"sync"
	"time"
)

// MemoryTokenBlacklist is an in-memory token blacklist with lazy expiry eviction.
type MemoryTokenBlacklist struct {
	mu      sync.RWMutex
	entries map[string]time.Time // jti → expiresAt
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewMemoryTokenBlacklist creates a new in-memory token blacklist with periodic cleanup.
func NewMemoryTokenBlacklist() *MemoryTokenBlacklist {
	b := &MemoryTokenBlacklist{
		entries: make(map[string]time.Time),
		stopCh:  make(chan struct{}),
	}
	b.wg.Add(1)
	go b.cleanupLoop()
	return b
}

// Close stops the background cleanup goroutine.
func (b *MemoryTokenBlacklist) Close() error {
	close(b.stopCh)
	b.wg.Wait()
	return nil
}

// cleanupLoop periodically removes expired entries.
func (b *MemoryTokenBlacklist) cleanupLoop() {
	defer b.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.evictExpired()
		}
	}
}

// evictExpired removes all expired entries.
func (b *MemoryTokenBlacklist) evictExpired() {
	now := time.Now()
	b.mu.Lock()
	for jti, expiresAt := range b.entries {
		if now.After(expiresAt) {
			delete(b.entries, jti)
		}
	}
	b.mu.Unlock()
}

// Revoke adds a JTI to the blacklist.
func (b *MemoryTokenBlacklist) Revoke(ctx context.Context, jti string, expiresAt time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[jti] = expiresAt
	return nil
}

// IsRevoked checks if a JTI is blacklisted.
func (b *MemoryTokenBlacklist) IsRevoked(ctx context.Context, jti string) bool {
	b.mu.RLock()
	expiresAt, ok := b.entries[jti]
	b.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expiresAt) {
		b.mu.Lock()
		if stored, stillExists := b.entries[jti]; stillExists && time.Now().After(stored) {
			delete(b.entries, jti)
		}
		b.mu.Unlock()
		return false
	}
	return true
}

package middleware

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"streamgate/pkg/cachetypes"
)

type NFTAccessEntry struct {
	HasNFT      bool
	Balance     *big.Int
	BlockNumber uint64
	BlockHash   string
	Expires     time.Time
}

type NFTAccessCache interface {
	Get(key string) (NFTAccessEntry, bool)
	Set(key string, entry NFTAccessEntry)
}

type memoryNFTCache struct {
	mu      sync.RWMutex
	entries map[string]NFTAccessEntry
	maxSize int
	access  map[string]time.Time
}

func newMemoryNFTCache(maxSize int) *memoryNFTCache {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &memoryNFTCache{
		entries: make(map[string]NFTAccessEntry),
		maxSize: maxSize,
		access:  make(map[string]time.Time),
	}
}

func (m *memoryNFTCache) Get(key string) (NFTAccessEntry, bool) {
	m.mu.RLock()
	v, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok {
		return NFTAccessEntry{}, false
	}
	if time.Now().After(v.Expires) {
		m.mu.Lock()
		delete(m.entries, key)
		delete(m.access, key)
		m.mu.Unlock()
		return NFTAccessEntry{}, false
	}
	m.mu.Lock()
	m.access[key] = time.Now()
	m.mu.Unlock()
	return v, true
}

func (m *memoryNFTCache) Set(key string, value NFTAccessEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) >= m.maxSize {
		m.evictLRULocked()
	}
	m.entries[key] = value
	m.access[key] = time.Now()
}

func (m *memoryNFTCache) evictLRULocked() {
	var oldestKey string
	var oldestTime time.Time
	for k, t := range m.access {
		if oldestKey == "" || t.Before(oldestTime) {
			oldestKey = k
			oldestTime = t
		}
	}
	if oldestKey != "" {
		delete(m.entries, oldestKey)
		delete(m.access, oldestKey)
	}
}

type tieredNFTCache struct {
	l1      *memoryNFTCache
	l2      NFTAccessCache
	metrics NFTAccessCacheMetrics
}

type NFTAccessCacheMetrics struct {
	L1Hit   int64
	L2Hit   int64
	L3Miss  int64
	Reorged int64
}

func newTieredNFTCache(l1Size int, l2 NFTAccessCache) *tieredNFTCache {
	if l1Size <= 0 {
		l1Size = 10000
	}
	return &tieredNFTCache{
		l1: newMemoryNFTCache(l1Size),
		l2: l2,
	}
}

func (t *tieredNFTCache) Get(key string) (NFTAccessEntry, bool) {
	v, ok := t.l1.Get(key)
	if ok {
		t.metrics.L1Hit++
		return v, true
	}
	if t.l2 != nil {
		v, ok = t.l2.Get(key)
		if ok {
			t.metrics.L2Hit++
			t.l1.Set(key, v)
			return v, true
		}
	}
	t.metrics.L3Miss++
	return NFTAccessEntry{}, false
}

func (t *tieredNFTCache) Set(key string, value NFTAccessEntry) {
	t.l1.Set(key, value)
	if t.l2 != nil {
		t.l2.Set(key, value)
	}
}

func (t *tieredNFTCache) GetMetrics() NFTAccessCacheMetrics {
	return t.metrics
}

type redisNFTCache struct {
	store cachetypes.CacheBackend
}

func newRedisNFTCache(store cachetypes.CacheBackend) *redisNFTCache {
	return &redisNFTCache{store: store}
}

func (r *redisNFTCache) Get(key string) (NFTAccessEntry, bool) {
	v, err := r.store.Get(fmt.Sprintf("nft:verify:%s", key))
	if err != nil {
		return NFTAccessEntry{}, false
	}
	entry, ok := v.(NFTAccessEntry)
	if !ok {
		return NFTAccessEntry{}, false
	}
	if time.Now().After(entry.Expires) {
		_ = r.store.Delete(fmt.Sprintf("nft:verify:%s", key))
		return NFTAccessEntry{}, false
	}
	return entry, true
}

func (r *redisNFTCache) Set(key string, entry NFTAccessEntry) {
	_ = r.store.Set(fmt.Sprintf("nft:verify:%s", key), entry)
}

package optimization

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CacheLevel represents cache level
type CacheLevel int

const (
	L1 CacheLevel = iota // In-memory cache
	L2                   // Redis cache
	L3                   // CDN cache
)

// MultiLevelCacheEntry represents a cache entry in multi-level cache
type MultiLevelCacheEntry struct {
	ID        string
	Key       string
	Value     interface{}
	TTL       time.Duration
	CreatedAt time.Time
	ExpiresAt time.Time
	HitCount  int64
	Level     CacheLevel
}

// CacheStats represents cache statistics
type CacheStats struct {
	TotalRequests int64
	CacheHits     int64
	CacheMisses   int64
	HitRate       float64
	EvictionCount int64
	Size          int64
}

// MultiLevelCache implements multi-level caching
type MultiLevelCache struct {
	mu        sync.RWMutex
	l1Cache   map[string]*MultiLevelCacheEntry
	l2Cache   map[string]*MultiLevelCacheEntry
	l3Cache   map[string]*MultiLevelCacheEntry
	stats     *CacheStats
	maxL1Size int
	maxL2Size int
	maxL3Size int
	l1Entries int
	l2Entries int
	l3Entries int
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewMultiLevelCache creates a new multi-level cache
func NewMultiLevelCache(maxL1, maxL2, maxL3 int) *MultiLevelCache {
	ctx, cancel := context.WithCancel(context.Background())

	cache := &MultiLevelCache{
		l1Cache:   make(map[string]*MultiLevelCacheEntry),
		l2Cache:   make(map[string]*MultiLevelCacheEntry),
		l3Cache:   make(map[string]*MultiLevelCacheEntry),
		stats:     &CacheStats{},
		maxL1Size: maxL1,
		maxL2Size: maxL2,
		maxL3Size: maxL3,
		ctx:       ctx,
		cancel:    cancel,
	}

	cache.start()
	return cache
}

// start begins the cache cleanup process
func (c *MultiLevelCache) start() {
	c.wg.Add(1)
	go c.cleanupLoop()
}

// cleanupLoop periodically cleans up expired entries
func (c *MultiLevelCache) cleanupLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

// Set sets a value in the cache
func (c *MultiLevelCache) Set(key string, value interface{}, ttl time.Duration, level CacheLevel) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := &MultiLevelCacheEntry{
		ID:        uuid.New().String(),
		Key:       key,
		Value:     value,
		TTL:       ttl,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(ttl),
		Level:     level,
	}

	switch level {
	case L1:
		if c.l1Entries >= c.maxL1Size {
			c.evictL1()
		}
		c.l1Cache[key] = entry
		c.l1Entries++

	case L2:
		if c.l2Entries >= c.maxL2Size {
			c.evictL2()
		}
		c.l2Cache[key] = entry
		c.l2Entries++

	case L3:
		if c.l3Entries >= c.maxL3Size {
			c.evictL3()
		}
		c.l3Cache[key] = entry
		c.l3Entries++
	}

	return nil
}

// Get retrieves a value from the cache
func (c *MultiLevelCache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.TotalRequests++

	// Try L1 cache first
	if entry, ok := c.l1Cache[key]; ok {
		if time.Now().Before(entry.ExpiresAt) {
			entry.HitCount++
			c.stats.CacheHits++
			c.updateHitRate()
			return entry.Value, true
		}
		delete(c.l1Cache, key)
		c.l1Entries--
	}

	// Try L2 cache
	if entry, ok := c.l2Cache[key]; ok {
		if time.Now().Before(entry.ExpiresAt) {
			entry.HitCount++
			c.stats.CacheHits++
			c.updateHitRate()
			// Promote to L1
			c.promoteToL1(entry)
			return entry.Value, true
		}
		delete(c.l2Cache, key)
		c.l2Entries--
	}

	// Try L3 cache
	if entry, ok := c.l3Cache[key]; ok {
		if time.Now().Before(entry.ExpiresAt) {
			entry.HitCount++
			c.stats.CacheHits++
			c.updateHitRate()
			// Promote to L1
			c.promoteToL1(entry)
			return entry.Value, true
		}
		delete(c.l3Cache, key)
		c.l3Entries--
	}

	c.stats.CacheMisses++
	c.updateHitRate()
	return nil, false
}

// Delete deletes a value from the cache
func (c *MultiLevelCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.l1Cache[key]; ok {
		delete(c.l1Cache, key)
		c.l1Entries--
	}

	if _, ok := c.l2Cache[key]; ok {
		delete(c.l2Cache, key)
		c.l2Entries--
	}

	if _, ok := c.l3Cache[key]; ok {
		delete(c.l3Cache, key)
		c.l3Entries--
	}
}

// Clear clears all caches
func (c *MultiLevelCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.l1Cache = make(map[string]*MultiLevelCacheEntry)
	c.l2Cache = make(map[string]*MultiLevelCacheEntry)
	c.l3Cache = make(map[string]*MultiLevelCacheEntry)
	c.l1Entries = 0
	c.l2Entries = 0
	c.l3Entries = 0
}

// evictL1 evicts an entry from L1 cache
func (c *MultiLevelCache) evictL1() {
	// Find least recently used entry
	var lruKey string
	var lruEntry *MultiLevelCacheEntry

	for key, entry := range c.l1Cache {
		if lruEntry == nil || entry.HitCount < lruEntry.HitCount {
			lruKey = key
			lruEntry = entry
		}
	}

	if lruKey != "" {
		delete(c.l1Cache, lruKey)
		c.l1Entries--
		c.stats.EvictionCount++

		// Move to L2
		if c.l2Entries < c.maxL2Size {
			c.l2Cache[lruKey] = lruEntry
			c.l2Entries++
		}
	}
}

// evictL2 evicts an entry from L2 cache
func (c *MultiLevelCache) evictL2() {
	// Find least recently used entry
	var lruKey string
	var lruEntry *MultiLevelCacheEntry

	for key, entry := range c.l2Cache {
		if lruEntry == nil || entry.HitCount < lruEntry.HitCount {
			lruKey = key
			lruEntry = entry
		}
	}

	if lruKey != "" {
		delete(c.l2Cache, lruKey)
		c.l2Entries--
		c.stats.EvictionCount++

		// Move to L3
		if c.l3Entries < c.maxL3Size {
			c.l3Cache[lruKey] = lruEntry
			c.l3Entries++
		}
	}
}

// evictL3 evicts an entry from L3 cache
func (c *MultiLevelCache) evictL3() {
	// Find least recently used entry
	var lruKey string
	var lruEntry *MultiLevelCacheEntry

	for key, entry := range c.l3Cache {
		if lruEntry == nil || entry.HitCount < lruEntry.HitCount {
			lruKey = key
			lruEntry = entry
		}
	}

	if lruKey != "" {
		delete(c.l3Cache, lruKey)
		c.l3Entries--
		c.stats.EvictionCount++
	}
}

// promoteToL1 promotes an entry to L1 cache
func (c *MultiLevelCache) promoteToL1(entry *MultiLevelCacheEntry) {
	if c.l1Entries >= c.maxL1Size {
		c.evictL1()
	}

	c.l1Cache[entry.Key] = entry
	c.l1Entries++
}

// cleanup removes expired entries
func (c *MultiLevelCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Clean L1
	for key, entry := range c.l1Cache {
		if now.After(entry.ExpiresAt) {
			delete(c.l1Cache, key)
			c.l1Entries--
		}
	}

	// Clean L2
	for key, entry := range c.l2Cache {
		if now.After(entry.ExpiresAt) {
			delete(c.l2Cache, key)
			c.l2Entries--
		}
	}

	// Clean L3
	for key, entry := range c.l3Cache {
		if now.After(entry.ExpiresAt) {
			delete(c.l3Cache, key)
			c.l3Entries--
		}
	}
}

// updateHitRate updates the cache hit rate
func (c *MultiLevelCache) updateHitRate() {
	if c.stats.TotalRequests > 0 {
		c.stats.HitRate = float64(c.stats.CacheHits) / float64(c.stats.TotalRequests)
	}
}

// GetStats returns cache statistics
func (c *MultiLevelCache) GetStats() *CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := &CacheStats{
		TotalRequests: c.stats.TotalRequests,
		CacheHits:     c.stats.CacheHits,
		CacheMisses:   c.stats.CacheMisses,
		HitRate:       c.stats.HitRate,
		EvictionCount: c.stats.EvictionCount,
		Size:          int64(c.l1Entries + c.l2Entries + c.l3Entries),
	}

	return stats
}

// GetSize returns the total cache size
func (c *MultiLevelCache) GetSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.l1Entries + c.l2Entries + c.l3Entries
}

// Close closes the cache
func (c *MultiLevelCache) Close() error {
	c.cancel()
	c.wg.Wait()
	return nil
}

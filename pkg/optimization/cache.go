package optimization

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// CacheEntry represents a cache entry
type CacheEntry struct {
	Key          string
	Value        interface{}
	ExpiresAt    time.Time
	CreatedAt    time.Time
	AccessCount  int64
	LastAccessed time.Time
}

// LocalCache is an in-memory cache with TTL support
type LocalCache struct {
	logger          *zap.Logger
	mu              sync.RWMutex
	entries         map[string]*CacheEntry
	maxSize         int
	ttl             time.Duration
	cleanupInterval time.Duration
	stopChan        chan struct{}
}

// NewLocalCache creates a new local cache
func NewLocalCache(maxSize int, ttl time.Duration, logger *zap.Logger) *LocalCache {
	cache := &LocalCache{
		logger:          logger,
		entries:         make(map[string]*CacheEntry),
		maxSize:         maxSize,
		ttl:             ttl,
		cleanupInterval: 1 * time.Minute,
		stopChan:        make(chan struct{}),
	}

	// Start cleanup goroutine
	go cache.cleanupLoop()

	return cache
}

// Set sets a cache entry
func (lc *LocalCache) Set(key string, value interface{}) error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Check size limit
	if len(lc.entries) >= lc.maxSize {
		// Evict oldest entry
		lc.evictOldest()
	}

	entry := &CacheEntry{
		Key:          key,
		Value:        value,
		ExpiresAt:    time.Now().Add(lc.ttl),
		CreatedAt:    time.Now(),
		AccessCount:  0,
		LastAccessed: time.Now(),
	}

	lc.entries[key] = entry
	lc.logger.Debug("Cache entry set", zap.String("key", key))

	return nil
}

// Get gets a cache entry
func (lc *LocalCache) Get(key string) (interface{}, bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	entry, exists := lc.entries[key]
	if !exists {
		return nil, false
	}

	// Check expiration
	if time.Now().After(entry.ExpiresAt) {
		delete(lc.entries, key)
		return nil, false
	}

	// Update access info
	entry.AccessCount++
	entry.LastAccessed = time.Now()

	return entry.Value, true
}

// Delete deletes a cache entry
func (lc *LocalCache) Delete(key string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	delete(lc.entries, key)
	lc.logger.Debug("Cache entry deleted", zap.String("key", key))
}

// Clear clears all cache entries
func (lc *LocalCache) Clear() {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.entries = make(map[string]*CacheEntry)
	lc.logger.Info("Cache cleared")
}

// GetSize returns the cache size
func (lc *LocalCache) GetSize() int {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	return len(lc.entries)
}

// evictOldest evicts the oldest entry
func (lc *LocalCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range lc.entries {
		if oldestTime.IsZero() || entry.CreatedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.CreatedAt
		}
	}

	if oldestKey != "" {
		delete(lc.entries, oldestKey)
		lc.logger.Debug("Cache entry evicted", zap.String("key", oldestKey))
	}
}

// cleanupLoop periodically cleans up expired entries
func (lc *LocalCache) cleanupLoop() {
	ticker := time.NewTicker(lc.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lc.cleanup()
		case <-lc.stopChan:
			return
		}
	}
}

// cleanup removes expired entries
func (lc *LocalCache) cleanup() {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	now := time.Now()
	expiredCount := 0

	for key, entry := range lc.entries {
		if now.After(entry.ExpiresAt) {
			delete(lc.entries, key)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		lc.logger.Debug("Cache cleanup completed", zap.Int("expired_count", expiredCount))
	}
}

// Stop stops the cache cleanup
func (lc *LocalCache) Stop() {
	close(lc.stopChan)
	lc.logger.Info("Local cache stopped")
}

// GetStats returns cache statistics
func (lc *LocalCache) GetStats() map[string]interface{} {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	stats := map[string]interface{}{
		"size":     len(lc.entries),
		"max_size": lc.maxSize,
		"ttl":      lc.ttl.String(),
	}

	totalAccess := int64(0)
	for _, entry := range lc.entries {
		totalAccess += entry.AccessCount
	}

	stats["total_accesses"] = totalAccess

	return stats
}

// BatchCache handles batch operations
type BatchCache struct {
	cache *LocalCache
	batch map[string]interface{}
	mu    sync.Mutex
}

// NewBatchCache creates a new batch cache
func NewBatchCache(cache *LocalCache) *BatchCache {
	return &BatchCache{
		cache: cache,
		batch: make(map[string]interface{}),
	}
}

// Add adds an entry to the batch
func (bc *BatchCache) Add(key string, value interface{}) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.batch[key] = value
}

// Flush flushes the batch to cache
func (bc *BatchCache) Flush() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	for key, value := range bc.batch {
		if err := bc.cache.Set(key, value); err != nil {
			return fmt.Errorf("failed to set cache entry: %w", err)
		}
	}

	bc.batch = make(map[string]interface{})
	return nil
}

// GetBatchSize returns the batch size
func (bc *BatchCache) GetBatchSize() int {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	return len(bc.batch)
}

// CacheWarmer pre-loads frequently accessed data
type CacheWarmer struct {
	cache   *LocalCache
	logger  *zap.Logger
	loaders map[string]CacheLoader
	mu      sync.RWMutex
}

// CacheLoader is a function that loads data for cache warming
type CacheLoader func() (map[string]interface{}, error)

// NewCacheWarmer creates a new cache warmer
func NewCacheWarmer(cache *LocalCache, logger *zap.Logger) *CacheWarmer {
	return &CacheWarmer{
		cache:   cache,
		logger:  logger,
		loaders: make(map[string]CacheLoader),
	}
}

// RegisterLoader registers a cache loader
func (cw *CacheWarmer) RegisterLoader(name string, loader CacheLoader) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	cw.loaders[name] = loader
	cw.logger.Debug("Cache loader registered", zap.String("name", name))
}

// Warm warms the cache
func (cw *CacheWarmer) Warm() error {
	cw.mu.RLock()
	loaders := make(map[string]CacheLoader)
	for name, loader := range cw.loaders {
		loaders[name] = loader
	}
	cw.mu.RUnlock()

	for name, loader := range loaders {
		data, err := loader()
		if err != nil {
			cw.logger.Error("Failed to load cache data", zap.String("loader", name), zap.Error(err))
			continue
		}

		for key, value := range data {
			if err := cw.cache.Set(key, value); err != nil {
				cw.logger.Error("Failed to set cache entry", zap.String("key", key), zap.Error(err))
			}
		}

		cw.logger.Info("Cache warmed", zap.String("loader", name), zap.Int("entries", len(data)))
	}

	return nil
}

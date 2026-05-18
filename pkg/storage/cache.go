package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Cache abstracts key-value cache operations.
// Both *RedisCache and *CacheStorage satisfy this interface.
//
//go:generate mockgen -destination=mocks/mock_cache.go -package=mocks streamgate/pkg/storage Cache
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	SetWithExpiration(ctx context.Context, key, value string, expiration time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Close() error
}

// CacheStorage handles cache storage with in-memory LRU cache
type CacheStorage struct {
	items   map[string]*cacheItem
	mu      sync.RWMutex
	wg      sync.WaitGroup
	maxSize int
	stopCh  chan struct{}
	closed  bool
	closeOnce sync.Once
}

type cacheItem struct {
	value      interface{}
	expiration time.Time
	lastAccess atomic.Int64
}

// NewCacheStorage creates a new cache storage instance
func NewCacheStorage(maxSize int) *CacheStorage {
	cs := &CacheStorage{
		items:   make(map[string]*cacheItem),
		maxSize: maxSize,
		stopCh:  make(chan struct{}),
	}

	// Start cleanup goroutine
	cs.wg.Add(1)
	go cs.cleanupExpired()

	return cs
}

// Close stops the cleanup goroutine, waits for it to exit, and clears the cache.
func (cs *CacheStorage) Close() {
	cs.closeOnce.Do(func() {
		close(cs.stopCh)
		cs.wg.Wait()
		cs.mu.Lock()
		cs.items = make(map[string]*cacheItem)
		cs.closed = true
		cs.mu.Unlock()
	})
}

// Get gets value from cache
func (cs *CacheStorage) Get(key string) (interface{}, error) {
	cs.mu.RLock()
	item, exists := cs.items[key]
	cs.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	if !item.expiration.IsZero() && time.Now().After(item.expiration) {
		return nil, fmt.Errorf("key expired: %s", key)
	}

	item.lastAccess.Store(time.Now().UnixNano())

	return item.value, nil
}

// GetString gets string value from cache
func (cs *CacheStorage) GetString(key string) (string, error) {
	val, err := cs.Get(key)
	if err != nil {
		return "", err
	}

	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("value is not a string")
	}

	return str, nil
}

// GetBytes gets byte slice from cache
func (cs *CacheStorage) GetBytes(key string) ([]byte, error) {
	val, err := cs.Get(key)
	if err != nil {
		return nil, err
	}

	bytes, ok := val.([]byte)
	if !ok {
		return nil, fmt.Errorf("value is not a byte slice")
	}

	return bytes, nil
}

// Set sets value in cache without expiration
func (cs *CacheStorage) Set(key string, value interface{}) error {
	return cs.SetWithExpiration(key, value, 0)
}

// SetWithExpiration sets value in cache with expiration
func (cs *CacheStorage) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Check if we need to evict items
	if len(cs.items) >= cs.maxSize {
		cs.evictLRU()
	}

	var expiration time.Time
	if ttl > 0 {
		expiration = time.Now().Add(ttl)
	}

	cs.items[key] = &cacheItem{
		value:      value,
		expiration: expiration,
	}
	cs.items[key].lastAccess.Store(time.Now().UnixNano())

	return nil
}

// SetJSON sets JSON-encoded value in cache
func (cs *CacheStorage) SetJSON(key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return cs.SetWithExpiration(key, data, ttl)
}

// GetJSON gets and decodes JSON value from cache
func (cs *CacheStorage) GetJSON(key string, dest interface{}) error {
	data, err := cs.GetBytes(key)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

// Delete deletes value from cache
func (cs *CacheStorage) Delete(key string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	delete(cs.items, key)
	return nil
}

// Exists checks if key exists in cache
func (cs *CacheStorage) Exists(key string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	item, exists := cs.items[key]
	if !exists {
		return false
	}

	// Check if expired
	if !item.expiration.IsZero() && time.Now().After(item.expiration) {
		return false
	}

	return true
}

// CacheAdapter wraps a CacheStorage to satisfy the Cache interface.
// This allows CacheStorage to be used where a Cache is expected.
type CacheAdapter struct {
	inner *CacheStorage
}

// NewCacheAdapter creates a Cache adapter from a CacheStorage.
func NewCacheAdapter(cs *CacheStorage) *CacheAdapter {
	return &CacheAdapter{inner: cs}
}

// Get gets a string value from cache.
func (a *CacheAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.inner.GetString(key)
}

// Set sets a string value in cache.
func (a *CacheAdapter) Set(ctx context.Context, key, value string) error {
	return a.inner.Set(key, value)
}

// SetWithExpiration sets a string value with expiration.
func (a *CacheAdapter) SetWithExpiration(ctx context.Context, key, value string, expiration time.Duration) error {
	return a.inner.SetWithExpiration(key, value, expiration)
}

// Delete deletes a key from cache.
func (a *CacheAdapter) Delete(ctx context.Context, key string) error {
	return a.inner.Delete(key)
}

// Exists checks if a key exists in cache.
func (a *CacheAdapter) Exists(ctx context.Context, key string) (bool, error) {
	return a.inner.Exists(key), nil
}

// Close stops the cleanup goroutine and clears the cache.
func (a *CacheAdapter) Close() error {
	a.inner.Close()
	return nil
}

// Clear clears all items from cache
func (cs *CacheStorage) Clear() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.items = make(map[string]*cacheItem)
	return nil
}

// Size returns the number of items in cache
func (cs *CacheStorage) Size() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	return len(cs.items)
}

// evictLRU evicts the least recently used item
func (cs *CacheStorage) evictLRU() {
	evictCount := cs.maxSize / 10
	if evictCount < 1 {
		evictCount = 1
	}

	type kv struct {
		key        string
		lastAccess int64
	}
	candidates := make([]kv, 0, len(cs.items))
	for key, item := range cs.items {
		candidates = append(candidates, kv{key: key, lastAccess: item.lastAccess.Load()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].lastAccess < candidates[j].lastAccess
	})

	for i := 0; i < evictCount && i < len(candidates); i++ {
		delete(cs.items, candidates[i].key)
	}
}

// cleanupExpired periodically removes expired items
func (cs *CacheStorage) cleanupExpired() {
	defer cs.wg.Done()
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-cs.stopCh:
			return
		case <-ticker.C:
			cs.mu.Lock()
			now := time.Now()
			for key, item := range cs.items {
				if !item.expiration.IsZero() && now.After(item.expiration) {
					delete(cs.items, key)
				}
			}
			cs.mu.Unlock()
		}
	}
}

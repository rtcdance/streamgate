package storage

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CacheStorage handles cache storage with in-memory LRU cache
type CacheStorage struct {
	items   map[string]*cacheItem
	mu      sync.RWMutex
	maxSize int
}

type cacheItem struct {
	value      interface{}
	expiration time.Time
	lastAccess time.Time
}

// NewCacheStorage creates a new cache storage instance
func NewCacheStorage(maxSize int) *CacheStorage {
	cs := &CacheStorage{
		items:   make(map[string]*cacheItem),
		maxSize: maxSize,
	}

	// Start cleanup goroutine
	go cs.cleanupExpired()

	return cs
}

// Get gets value from cache
func (cs *CacheStorage) Get(key string) (interface{}, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	item, exists := cs.items[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	// Check if expired
	if !item.expiration.IsZero() && time.Now().After(item.expiration) {
		return nil, fmt.Errorf("key expired: %s", key)
	}

	// Update last access time
	item.lastAccess = time.Now()

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
		lastAccess: time.Now(),
	}

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
	var oldestKey string
	var oldestTime time.Time

	for key, item := range cs.items {
		if oldestKey == "" || item.lastAccess.Before(oldestTime) {
			oldestKey = key
			oldestTime = item.lastAccess
		}
	}

	if oldestKey != "" {
		delete(cs.items, oldestKey)
	}
}

// cleanupExpired periodically removes expired items
func (cs *CacheStorage) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
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

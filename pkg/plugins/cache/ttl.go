package cache

import "time"

// TTLCache represents a cache with TTL
type TTLCache struct {
	cache map[string]*ttlEntry
}

type ttlEntry struct {
	value  interface{}
	expiry time.Time
}

// NewTTLCache creates a new TTL cache
func NewTTLCache() *TTLCache {
	return &TTLCache{
		cache: make(map[string]*ttlEntry),
	}
}

// Get gets a value from cache
func (t *TTLCache) Get(key string) (interface{}, bool) {
	entry, ok := t.cache[key]
	if !ok {
		return nil, false
	}

	if time.Now().After(entry.expiry) {
		delete(t.cache, key)
		return nil, false
	}

	return entry.value, true
}

// Set sets a value in cache with TTL
func (t *TTLCache) Set(key string, value interface{}, ttl time.Duration) {
	t.cache[key] = &ttlEntry{
		value:  value,
		expiry: time.Now().Add(ttl),
	}
}

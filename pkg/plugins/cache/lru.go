package cache

import (
	"container/list"
	"sync"
	"time"
)

// CacheItem represents an item in the cache
type CacheItem struct {
	Key        string
	Value      interface{}
	ExpiresAt  time.Time
	AccessTime time.Time
	HitCount   int
	Size       int
}

// LRU implements LRU (Least Recently Used) cache with TTL support
type LRU struct {
	maxSize     int
	currentSize int
	cache       map[string]*list.Element
	lruList     *list.List
	mu          sync.RWMutex
	onEvict     func(key string, value interface{})
	stats       *CacheStats
}

// CacheStats tracks cache statistics
type CacheStats struct {
	Hits      int64
	Misses    int64
	Evictions int64
	Sets      int64
	Gets      int64
	Deletes   int64
	mu        sync.RWMutex
}

// NewLRU creates a new LRU cache
func NewLRU(maxSize int) *LRU {
	return &LRU{
		maxSize: maxSize,
		cache:   make(map[string]*list.Element),
		lruList: list.New(),
		stats:   &CacheStats{},
	}
}

// Get gets a value from cache
func (l *LRU) Get(key string) (interface{}, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.stats.Gets++

	elem, ok := l.cache[key]
	if !ok {
		l.stats.Misses++
		return nil, false
	}

	item := elem.Value.(*CacheItem)

	// Check if expired
	if !item.ExpiresAt.IsZero() && time.Now().After(item.ExpiresAt) {
		l.removeElement(elem)
		l.stats.Misses++
		return nil, false
	}

	// Move to front (most recently used)
	l.lruList.MoveToFront(elem)
	item.AccessTime = time.Now()
	item.HitCount++

	l.stats.Hits++
	return item.Value, true
}

// Set sets a value in cache
func (l *LRU) Set(key string, value interface{}) {
	l.SetWithTTL(key, value, 0)
}

// SetWithTTL sets a value in cache with TTL
func (l *LRU) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.stats.Sets++

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	// Calculate size (simple estimation)
	size := estimateSize(value)

	// Check if key already exists
	if elem, ok := l.cache[key]; ok {
		// Update existing item
		item := elem.Value.(*CacheItem)
		l.currentSize -= item.Size

		item.Value = value
		item.ExpiresAt = expiresAt
		item.AccessTime = time.Now()
		item.Size = size

		l.currentSize += size
		l.lruList.MoveToFront(elem)
		return
	}

	// Check if we need to evict
	for l.maxSize > 0 && len(l.cache) >= l.maxSize {
		l.evictOldest()
	}

	// Create new item
	item := &CacheItem{
		Key:        key,
		Value:      value,
		ExpiresAt:  expiresAt,
		AccessTime: time.Now(),
		HitCount:   0,
		Size:       size,
	}

	elem := l.lruList.PushFront(item)
	l.cache[key] = elem
	l.currentSize += size
}

// Delete deletes a value from cache
func (l *LRU) Delete(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.stats.Deletes++

	if elem, ok := l.cache[key]; ok {
		l.removeElement(elem)
	}
}

// Clear clears all items from cache
func (l *LRU) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cache = make(map[string]*list.Element)
	l.lruList.Init()
	l.currentSize = 0
}

// Len returns the number of items in cache
func (l *LRU) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.cache)
}

// Size returns the current size of cache in bytes
func (l *LRU) Size() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.currentSize
}

// Keys returns all keys in cache
func (l *LRU) Keys() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	keys := make([]string, 0, len(l.cache))
	for key := range l.cache {
		keys = append(keys, key)
	}
	return keys
}

// GetStats returns cache statistics
func (l *LRU) GetStats() *CacheStats {
	l.stats.mu.RLock()
	defer l.stats.mu.RUnlock()

	return &CacheStats{
		Hits:      l.stats.Hits,
		Misses:    l.stats.Misses,
		Evictions: l.stats.Evictions,
		Sets:      l.stats.Sets,
		Gets:      l.stats.Gets,
		Deletes:   l.stats.Deletes,
	}
}

// GetHitRate returns the cache hit rate
func (l *LRU) GetHitRate() float64 {
	stats := l.GetStats()
	total := stats.Hits + stats.Misses
	if total == 0 {
		return 0
	}
	return float64(stats.Hits) / float64(total)
}

// SetOnEvict sets a callback function to be called when an item is evicted
func (l *LRU) SetOnEvict(onEvict func(key string, value interface{})) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onEvict = onEvict
}

// GetOldest returns the oldest item in cache
func (l *LRU) GetOldest() (string, interface{}, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.lruList.Len() == 0 {
		return "", nil, false
	}

	elem := l.lruList.Back()
	item := elem.Value.(*CacheItem)
	return item.Key, item.Value, true
}

// GetNewest returns the newest item in cache
func (l *LRU) GetNewest() (string, interface{}, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.lruList.Len() == 0 {
		return "", nil, false
	}

	elem := l.lruList.Front()
	item := elem.Value.(*CacheItem)
	return item.Key, item.Value, true
}

// Peek gets a value without updating its position in LRU
func (l *LRU) Peek(key string) (interface{}, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	elem, ok := l.cache[key]
	if !ok {
		return nil, false
	}

	item := elem.Value.(*CacheItem)

	// Check if expired
	if !item.ExpiresAt.IsZero() && time.Now().After(item.ExpiresAt) {
		return nil, false
	}

	return item.Value, true
}

// Contains checks if a key exists in cache
func (l *LRU) Contains(key string) bool {
	_, ok := l.Peek(key)
	return ok
}

// Resize changes the maximum size of the cache
func (l *LRU) Resize(newSize int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.maxSize = newSize

	// Evict items if necessary
	for l.maxSize > 0 && len(l.cache) > l.maxSize {
		l.evictOldest()
	}
}

// GetItem returns the full cache item for a key
func (l *LRU) GetItem(key string) (*CacheItem, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	elem, ok := l.cache[key]
	if !ok {
		return nil, false
	}

	item := elem.Value.(*CacheItem)

	// Check if expired
	if !item.ExpiresAt.IsZero() && time.Now().After(item.ExpiresAt) {
		return nil, false
	}

	return item, true
}

// evictOldest removes the oldest item from cache
func (l *LRU) evictOldest() {
	elem := l.lruList.Back()
	if elem != nil {
		l.removeElement(elem)
	}
}

// removeElement removes an element from cache
func (l *LRU) removeElement(elem *list.Element) {
	item := elem.Value.(*CacheItem)

	l.currentSize -= item.Size
	delete(l.cache, item.Key)
	l.lruList.Remove(elem)
	l.stats.Evictions++

	if l.onEvict != nil {
		l.onEvict(item.Key, item.Value)
	}
}

// estimateSize estimates the size of a value in bytes
func estimateSize(value interface{}) int {
	switch v := value.(type) {
	case string:
		return len(v)
	case []byte:
		return len(v)
	case int, int8, int16, int32, int64:
		return 8
	case uint, uint8, uint16, uint32, uint64:
		return 8
	case float32, float64:
		return 8
	case bool:
		return 1
	default:
		// Default estimation for complex types
		return 100
	}
}

// CleanupExpired removes all expired items from cache
func (l *LRU) CleanupExpired() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	expired := 0

	for _, elem := range l.cache {
		item := elem.Value.(*CacheItem)
		if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
			l.removeElement(elem)
			expired++
		}
	}

	return expired
}

// GetTopKeys returns the most frequently accessed keys
func (l *LRU) GetTopKeys(n int) []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	type keyHitCount struct {
		key  string
		hits int
	}

	items := make([]keyHitCount, 0, len(l.cache))

	for key, elem := range l.cache {
		item := elem.Value.(*CacheItem)
		items = append(items, keyHitCount{key: key, hits: item.HitCount})
	}

	// Simple bubble sort (for small n)
	for i := 0; i < len(items)-1 && i < n; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].hits > items[i].hits {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	result := make([]string, 0, n)
	for i := 0; i < len(items) && i < n; i++ {
		result = append(result, items[i].key)
	}

	return result
}

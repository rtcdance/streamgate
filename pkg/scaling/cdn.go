package scaling

import (
	"fmt"
	"sync"
	"time"
)

// CDNProvider represents a CDN provider
type CDNProvider string

const (
	CloudFlare CDNProvider = "cloudflare"
	Akamai     CDNProvider = "akamai"
	Fastly     CDNProvider = "fastly"
)

// CDNConfig holds CDN configuration
type CDNConfig struct {
	Provider  CDNProvider
	APIKey    string
	APISecret string
	ZoneID    string
	Endpoint  string
}

// CDNCache represents a cached item
type CDNCache struct {
	Key        string
	URL        string
	TTL        int64 // seconds
	Size       int64 // bytes
	HitCount   int64
	MissCount  int64
	LastAccess time.Time
	CreatedAt  time.Time
}

// CDNMetrics holds CDN metrics
type CDNMetrics struct {
	TotalRequests  int64
	CacheHits      int64
	CacheMisses    int64
	HitRate        float64
	TotalBandwidth int64
	CachedSize     int64
	LastUpdated    time.Time
}

// CDNManager manages CDN integration
type CDNManager struct {
	config       CDNConfig
	cache        map[string]*CDNCache
	metrics      *CDNMetrics
	mu           sync.RWMutex
	maxCacheSize int64 // bytes
}

// NewCDNManager creates a new CDN manager
func NewCDNManager(config CDNConfig, maxCacheSize int64) *CDNManager {
	if maxCacheSize == 0 {
		maxCacheSize = 1024 * 1024 * 1024 // 1GB default
	}

	return &CDNManager{
		config:       config,
		cache:        make(map[string]*CDNCache),
		maxCacheSize: maxCacheSize,
		metrics: &CDNMetrics{
			LastUpdated: time.Now(),
		},
	}
}

// CacheContent caches content in CDN
func (cm *CDNManager) CacheContent(key, url string, ttl int64, size int64) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if key == "" || url == "" {
		return fmt.Errorf("key and url are required")
	}

	// Check if cache is full
	if cm.getCachedSize()+size > cm.maxCacheSize {
		// Evict oldest item
		cm.evictOldest()
	}

	cache := &CDNCache{
		Key:       key,
		URL:       url,
		TTL:       ttl,
		Size:      size,
		CreatedAt: time.Now(),
	}

	cm.cache[key] = cache
	return nil
}

// GetCachedContent retrieves cached content
func (cm *CDNManager) GetCachedContent(key string) (*CDNCache, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cache, exists := cm.cache[key]
	if !exists {
		cm.metrics.CacheMisses++
		cm.metrics.TotalRequests++
		if cm.metrics.TotalRequests > 0 {
			cm.metrics.HitRate = float64(cm.metrics.CacheHits) / float64(cm.metrics.TotalRequests) * 100
		}
		return nil, fmt.Errorf("cache not found: %s", key)
	}

	// Check if expired
	if time.Since(cache.CreatedAt) > time.Duration(cache.TTL)*time.Second {
		delete(cm.cache, key)
		cm.metrics.CacheMisses++
		cm.metrics.TotalRequests++
		if cm.metrics.TotalRequests > 0 {
			cm.metrics.HitRate = float64(cm.metrics.CacheHits) / float64(cm.metrics.TotalRequests) * 100
		}
		return nil, fmt.Errorf("cache expired: %s", key)
	}

	cache.HitCount++
	cache.LastAccess = time.Now()
	cm.metrics.CacheHits++
	cm.metrics.TotalRequests++

	// Update hit rate
	if cm.metrics.TotalRequests > 0 {
		cm.metrics.HitRate = float64(cm.metrics.CacheHits) / float64(cm.metrics.TotalRequests) * 100
	}

	return cache, nil
}

// InvalidateCache invalidates cached content
func (cm *CDNManager) InvalidateCache(key string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	_, exists := cm.cache[key]
	if !exists {
		return fmt.Errorf("cache not found: %s", key)
	}

	delete(cm.cache, key)
	return nil
}

// InvalidateAll invalidates all cached content
func (cm *CDNManager) InvalidateAll() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.cache = make(map[string]*CDNCache)
	return nil
}

// ListCachedContent lists all cached content
func (cm *CDNManager) ListCachedContent() []*CDNCache {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var caches []*CDNCache
	for _, cache := range cm.cache {
		caches = append(caches, cache)
	}
	return caches
}

// GetMetrics retrieves CDN metrics
func (cm *CDNManager) GetMetrics() *CDNMetrics {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	metricsCopy := &CDNMetrics{
		TotalRequests:  cm.metrics.TotalRequests,
		CacheHits:      cm.metrics.CacheHits,
		CacheMisses:    cm.metrics.CacheMisses,
		HitRate:        cm.metrics.HitRate,
		TotalBandwidth: cm.metrics.TotalBandwidth,
		CachedSize:     cm.getCachedSize(),
		LastUpdated:    time.Now(),
	}

	return metricsCopy
}

// UpdateBandwidth updates bandwidth metrics
func (cm *CDNManager) UpdateBandwidth(bytes int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.metrics.TotalBandwidth += bytes
}

// GetCacheSize returns the current cache size
func (cm *CDNManager) GetCacheSize() int64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.getCachedSize()
}

// GetCacheCount returns the number of cached items
func (cm *CDNManager) GetCacheCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return len(cm.cache)
}

// GetMaxCacheSize returns the maximum cache size
func (cm *CDNManager) GetMaxCacheSize() int64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.maxCacheSize
}

// GetCacheUtilization returns cache utilization percentage
func (cm *CDNManager) GetCacheUtilization() float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.maxCacheSize == 0 {
		return 0
	}

	return float64(cm.getCachedSize()) / float64(cm.maxCacheSize) * 100
}

// getCachedSize returns the total cached size (internal, must hold lock)
func (cm *CDNManager) getCachedSize() int64 {
	size := int64(0)
	for _, cache := range cm.cache {
		size += cache.Size
	}
	return size
}

// evictOldest evicts the oldest cached item (internal, must hold lock)
func (cm *CDNManager) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, cache := range cm.cache {
		if oldestTime.IsZero() || cache.CreatedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = cache.CreatedAt
		}
	}

	if oldestKey != "" {
		delete(cm.cache, oldestKey)
	}
}

// PrefetchContent prefetches content to CDN
func (cm *CDNManager) PrefetchContent(urls []string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, url := range urls {
		// Simulate prefetch
		key := fmt.Sprintf("prefetch-%s", url)
		cache := &CDNCache{
			Key:       key,
			URL:       url,
			TTL:       3600, // 1 hour
			Size:      1024, // 1KB default
			CreatedAt: time.Now(),
		}
		cm.cache[key] = cache
	}

	return nil
}

// GetCacheHitRate returns the cache hit rate
func (cm *CDNManager) GetCacheHitRate() float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.metrics.HitRate
}

// GetCacheStats returns cache statistics
func (cm *CDNManager) GetCacheStats() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return map[string]interface{}{
		"total_requests":  cm.metrics.TotalRequests,
		"cache_hits":      cm.metrics.CacheHits,
		"cache_misses":    cm.metrics.CacheMisses,
		"hit_rate":        cm.metrics.HitRate,
		"total_bandwidth": cm.metrics.TotalBandwidth,
		"cached_size":     cm.getCachedSize(),
		"cache_count":     len(cm.cache),
		"utilization":     float64(cm.getCachedSize()) / float64(cm.maxCacheSize) * 100,
	}
}

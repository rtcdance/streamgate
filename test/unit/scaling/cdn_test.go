package scaling_test

import (
	"testing"

	"streamgate/pkg/scaling"
)

func TestCDNManager_CacheContent(t *testing.T) {
	config := scaling.CDNConfig{
		Provider: scaling.CloudFlare,
		APIKey:   "test-key",
	}

	cm := scaling.NewCDNManager(config, 1024*1024)

	err := cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 1024)
	if err != nil {
		t.Fatalf("CacheContent failed: %v", err)
	}

	if cm.GetCacheCount() != 1 {
		t.Errorf("Expected 1 cached item, got %d", cm.GetCacheCount())
	}
}

func TestCDNManager_GetCachedContent(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 1024)

	cached, err := cm.GetCachedContent("key-1")
	if err != nil {
		t.Fatalf("GetCachedContent failed: %v", err)
	}

	if cached.Key != "key-1" {
		t.Errorf("Cache key doesn't match")
	}

	if cached.HitCount != 1 {
		t.Errorf("Expected 1 hit, got %d", cached.HitCount)
	}
}

func TestCDNManager_InvalidateCache(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 1024)

	err := cm.InvalidateCache("key-1")
	if err != nil {
		t.Fatalf("InvalidateCache failed: %v", err)
	}

	if cm.GetCacheCount() != 0 {
		t.Errorf("Expected 0 cached items, got %d", cm.GetCacheCount())
	}
}

func TestCDNManager_InvalidateAll(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	for i := 1; i <= 3; i++ {
		cm.CacheContent("key-"+string(rune(i)), "https://example.com/file"+string(rune(i))+".mp4", 3600, 1024)
	}

	err := cm.InvalidateAll()
	if err != nil {
		t.Fatalf("InvalidateAll failed: %v", err)
	}

	if cm.GetCacheCount() != 0 {
		t.Errorf("Expected 0 cached items, got %d", cm.GetCacheCount())
	}
}

func TestCDNManager_ListCachedContent(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	for i := 1; i <= 3; i++ {
		cm.CacheContent("key-"+string(rune(i)), "https://example.com/file"+string(rune(i))+".mp4", 3600, 1024)
	}

	listed := cm.ListCachedContent()
	if len(listed) != 3 {
		t.Errorf("Expected 3 cached items, got %d", len(listed))
	}
}

func TestCDNManager_GetMetrics(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 1024)
	cm.GetCachedContent("key-1")
	cm.GetCachedContent("key-1")

	metrics := cm.GetMetrics()
	if metrics.CacheHits != 2 {
		t.Errorf("Expected 2 cache hits, got %d", metrics.CacheHits)
	}

	if metrics.HitRate != 100 {
		t.Errorf("Expected 100%% hit rate, got %.2f%%", metrics.HitRate)
	}
}

func TestCDNManager_UpdateBandwidth(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	cm.UpdateBandwidth(1024)
	cm.UpdateBandwidth(2048)

	metrics := cm.GetMetrics()
	if metrics.TotalBandwidth != 3072 {
		t.Errorf("Expected 3072 bytes, got %d", metrics.TotalBandwidth)
	}
}

func TestCDNManager_GetCacheSize(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	cm.CacheContent("key-1", "https://example.com/file1.mp4", 3600, 1024)
	cm.CacheContent("key-2", "https://example.com/file2.mp4", 3600, 2048)

	size := cm.GetCacheSize()
	if size != 3072 {
		t.Errorf("Expected 3072 bytes, got %d", size)
	}
}

func TestCDNManager_GetCacheUtilization(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 512*1024)

	utilization := cm.GetCacheUtilization()
	if utilization != 50 {
		t.Errorf("Expected 50%% utilization, got %.2f%%", utilization)
	}
}

func TestCDNManager_PrefetchContent(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	urls := []string{
		"https://example.com/file1.mp4",
		"https://example.com/file2.mp4",
		"https://example.com/file3.mp4",
	}

	err := cm.PrefetchContent(urls)
	if err != nil {
		t.Fatalf("PrefetchContent failed: %v", err)
	}

	if cm.GetCacheCount() != 3 {
		t.Errorf("Expected 3 prefetched items, got %d", cm.GetCacheCount())
	}
}

func TestCDNManager_GetCacheHitRate(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 1024)

	// 2 hits
	cm.GetCachedContent("key-1")
	cm.GetCachedContent("key-1")

	// 1 miss
	cm.GetCachedContent("key-2")

	hitRate := cm.GetCacheHitRate()
	if hitRate != 66.66666666666666 {
		t.Errorf("Expected ~66.67%% hit rate, got %.2f%%", hitRate)
	}
}

func TestCDNManager_GetCacheStats(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 1024)
	cm.GetCachedContent("key-1")

	stats := cm.GetCacheStats()
	if stats["cache_count"] != 1 {
		t.Errorf("Expected 1 cached item in stats")
	}

	if stats["cache_hits"] != int64(1) {
		t.Errorf("Expected 1 cache hit in stats")
	}
}

func TestCDNManager_CacheEviction(t *testing.T) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 2048) // 2KB max

	// Cache 3 items of 1KB each
	cm.CacheContent("key-1", "https://example.com/file1.mp4", 3600, 1024)
	cm.CacheContent("key-2", "https://example.com/file2.mp4", 3600, 1024)
	cm.CacheContent("key-3", "https://example.com/file3.mp4", 3600, 1024)

	// Should have evicted oldest (key-1)
	if cm.GetCacheCount() != 2 {
		t.Errorf("Expected 2 cached items after eviction, got %d", cm.GetCacheCount())
	}
}

func BenchmarkCDNManager_CacheContent(b *testing.B) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024*1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.CacheContent("key-"+string(rune(i)), "https://example.com/file.mp4", 3600, 1024)
	}
}

func BenchmarkCDNManager_GetCachedContent(b *testing.B) {
	config := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(config, 1024*1024)

	cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.GetCachedContent("key-1")
	}
}

package load_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"streamgate/test/helpers"
)

func TestLoad_CacheHitRate(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Setup: Pre-populate cache
	for i := 0; i < 100; i++ {
		cache.Set(context.Background(), "key_"+string(rune(i)), "value", 0)
	}

	// Test cache hit rate
	numGoroutines := 50
	numRequests := 100
	var wg sync.WaitGroup
	var hitCount int64
	var missCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				keyID := (id*numRequests + j) % 100
				_, err := cache.Get(context.Background(), "key_"+string(rune(keyID)))
				if err == nil {
					atomic.AddInt64(&hitCount, 1)
				} else {
					atomic.AddInt64(&missCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalRequests := hitCount + missCount
	hitRate := float64(hitCount) / float64(totalRequests) * 100
	throughput := float64(totalRequests) / elapsed.Seconds()

	t.Logf("Cache Hit Rate Load Test:")
	t.Logf("  Total Requests: %d", totalRequests)
	t.Logf("  Hits: %d", hitCount)
	t.Logf("  Misses: %d", missCount)
	t.Logf("  Hit Rate: %.2f%%", hitRate)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f req/s", throughput)

	helpers.AssertTrue(t, hitRate > 80)
}

func TestLoad_CacheWritePerformance(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Test cache write performance
	numGoroutines := 100
	numRequests := 50
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64
	var totalDuration time.Duration
	var mu sync.Mutex

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				writeStart := time.Now()
				err := cache.Set(context.Background(), "key_"+string(rune(id))+"_"+string(rune(j)), "value", 0)
				writeDuration := time.Since(writeStart)

				mu.Lock()
				totalDuration += writeDuration
				mu.Unlock()

				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalRequests := int64(numGoroutines * numRequests)
	avgDuration := totalDuration / time.Duration(totalRequests)
	throughput := float64(totalRequests) / elapsed.Seconds()

	t.Logf("Cache Write Performance Load Test:")
	t.Logf("  Total Writes: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Average Write Time: %v", avgDuration)
	t.Logf("  Total Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f writes/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

func TestLoad_CacheEviction(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Test cache eviction under load
	maxSize := 1000
	numGoroutines := 50
	numRequests := 100
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				keyID := (id*numRequests + j) % maxSize
				err := cache.Set(context.Background(), "key_"+string(rune(keyID)), "value", 0)
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalRequests := int64(numGoroutines * numRequests)
	throughput := float64(totalRequests) / elapsed.Seconds()

	t.Logf("Cache Eviction Load Test:")
	t.Logf("  Total Operations: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f ops/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

func TestLoad_CacheConsistency(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Test cache consistency under concurrent access
	key := "consistency_test"
	initialValue := "initial"
	cache.Set(context.Background(), key, initialValue, 0)

	numGoroutines := 50
	numRequests := 20
	var wg sync.WaitGroup
	var consistencyErrors int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				value, err := cache.Get(context.Background(), key)
				if err != nil || value != initialValue {
					atomic.AddInt64(&consistencyErrors, 1)
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalRequests := int64(numGoroutines * numRequests)
	consistencyRate := float64(totalRequests-consistencyErrors) / float64(totalRequests) * 100

	t.Logf("Cache Consistency Load Test:")
	t.Logf("  Total Reads: %d", totalRequests)
	t.Logf("  Consistency Errors: %d", consistencyErrors)
	t.Logf("  Consistency Rate: %.2f%%", consistencyRate)
	t.Logf("  Duration: %v", elapsed)

	helpers.AssertTrue(t, consistencyRate > 99)
}

func TestLoad_CacheMemoryUsage(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Test memory usage under load
	numKeys := 10000
	valueSize := 1024 // 1KB per value

	start := time.Now()

	for i := 0; i < numKeys; i++ {
		value := make([]byte, valueSize)
		cache.Set(context.Background(), "key_"+string(rune(i)), string(value), 0)
	}

	elapsed := time.Since(start)

	memoryUsage := cache.GetMemoryUsage(context.Background())
	avgMemoryPerKey := float64(memoryUsage) / float64(numKeys)

	t.Logf("Cache Memory Usage Load Test:")
	t.Logf("  Total Keys: %d", numKeys)
	t.Logf("  Value Size: %d bytes", valueSize)
	t.Logf("  Total Memory: %d bytes", memoryUsage)
	t.Logf("  Average per Key: %.2f bytes", avgMemoryPerKey)
	t.Logf("  Duration: %v", elapsed)
}

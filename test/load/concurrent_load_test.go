package load_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func TestLoad_ConcurrentAuthRequests(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService("test-secret-key", db)

	// Setup: Create a user
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	// Concurrent login requests
	numGoroutines := 10
	numRequests := 3
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				_, err := authService.Authenticate("testuser", "password123")
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalRequests := int64(numGoroutines * numRequests)
	throughput := float64(totalRequests) / elapsed.Seconds()

	t.Logf("Concurrent Auth Load Test Results:")
	t.Logf("  Total Requests: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f req/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

func TestLoad_ConcurrentContentOperations(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	contentService := service.NewContentService(db.GetDB(), storage, nil)

	// Setup: Create initial content
	content := &service.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		Size:        1024000,
		OwnerID:     uuid.New().String(),
	}
	id, err := contentService.CreateContent(content)
	helpers.AssertNoError(t, err)
	content.ID = id

	// Concurrent read operations
	numGoroutines := 10
	numRequests := 3
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				_, err := contentService.GetContent(content.ID)
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalRequests := int64(numGoroutines * numRequests)
	throughput := float64(totalRequests) / elapsed.Seconds()

	t.Logf("Concurrent Content Load Test Results:")
	t.Logf("  Total Requests: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f req/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

func TestLoad_ConcurrentCacheOperations(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Concurrent cache operations
	numGoroutines := 10
	numRequests := 5
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				key := fmt.Sprintf("key_%d_%d", id, j)
				err := cache.Set(key, "value")
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}

				_, err = cache.Get(key)
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

	totalRequests := int64(numGoroutines * numRequests * 2)
	throughput := float64(totalRequests) / elapsed.Seconds()

	t.Logf("Concurrent Cache Load Test Results:")
	t.Logf("  Total Requests: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f req/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

func TestLoad_ConcurrentDatabaseOperations(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService("test-secret-key", db)

	// Concurrent database operations
	numGoroutines := 10
	numRequests := 5
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				username := fmt.Sprintf("user_%d_%d", id, j)
				email := fmt.Sprintf("user%d%d@example.com", id, j)
				err := authService.Register(username, "password", email)
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

	t.Logf("Concurrent Database Load Test Results:")
	t.Logf("  Total Requests: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f req/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

func TestLoad_SustainedLoad(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService("test-secret-key", db)

	// Setup: Create a user
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	// Sustained load for 2 seconds
	duration := 2 * time.Second
	numGoroutines := 5
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()
	deadline := start.Add(duration)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				_, err := authService.Authenticate("testuser", "password123")
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalRequests := successCount + errorCount
	throughput := float64(totalRequests) / elapsed.Seconds()

	t.Logf("Sustained Load Test Results:")
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Total Requests: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Throughput: %.2f req/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

package load_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"streamgate/pkg/models"
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
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}
	authService.Register(context.Background(), user)

	// Concurrent login requests
	numGoroutines := 100
	numRequests := 10
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				_, err := authService.Login(context.Background(), user.Email, "password123")
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

	contentService := service.NewContentService(db)

	// Setup: Create initial content
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}
	contentService.Create(context.Background(), content)

	// Concurrent read operations
	numGoroutines := 50
	numRequests := 20
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				_, err := contentService.GetByID(context.Background(), content.ID)
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
	numGoroutines := 100
	numRequests := 50
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				key := "key_" + string(rune(id)) + "_" + string(rune(j))
				err := cache.Set(context.Background(), key, "value", 0)
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}

				_, err = cache.Get(context.Background(), key)
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

	// Concurrent database operations
	numGoroutines := 50
	numRequests := 20
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				user := &models.User{
					Username: "user_" + string(rune(id)) + "_" + string(rune(j)),
					Email:    "user" + string(rune(id)) + string(rune(j)) + "@example.com",
					Password: "password",
				}
				err := db.SaveUser(context.Background(), user)
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

	authService := service.NewAuthService(db)

	// Setup: Create a user
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}
	authService.Register(context.Background(), user)

	// Sustained load for 10 seconds
	duration := 10 * time.Second
	numGoroutines := 20
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
				_, err := authService.Login(context.Background(), user.Email, "password123")
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

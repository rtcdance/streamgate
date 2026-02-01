package load_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func TestLoad_DatabaseConnectionPool(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService("test-secret-key", db)

	// Test connection pool under load
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

	t.Logf("Database Connection Pool Load Test:")
	t.Logf("  Total Requests: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f req/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

func TestLoad_DatabaseQueryPerformance(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService("test-secret-key", db)

	// Setup: Create test data
	for i := 0; i < 10; i++ {
		username := fmt.Sprintf("user%d", i)
		email := fmt.Sprintf("user%d@example.com", i)
		authService.Register(username, "password", email)
	}

	// Test query performance
	numGoroutines := 5
	numRequests := 3
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64
	var totalDuration time.Duration
	var mu sync.Mutex

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				queryStart := time.Now()
				_, err := db.GetDB().Query("SELECT * FROM users LIMIT 10")
				queryDuration := time.Since(queryStart)

				mu.Lock()
				totalDuration += queryDuration
				mu.Unlock()

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
	avgDuration := totalDuration / time.Duration(totalRequests)
	throughput := float64(totalRequests) / elapsed.Seconds()

	t.Logf("Database Query Performance Load Test:")
	t.Logf("  Total Requests: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Average Query Time: %v", avgDuration)
	t.Logf("  Total Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f req/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

func TestLoad_DatabaseTransactions(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService("test-secret-key", db)

	// Test transaction handling under load
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

	t.Logf("Database Transaction Load Test:")
	t.Logf("  Total Transactions: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f tx/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

func TestLoad_DatabaseBulkOperations(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService("test-secret-key", db)

	// Test bulk operations
	bulkSize := 10
	numBulks := 2
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numBulks; i++ {
		wg.Add(1)
		go func(bulkID int) {
			defer wg.Done()
			for j := 0; j < bulkSize; j++ {
				username := fmt.Sprintf("user_%d_%d", bulkID, j)
				email := fmt.Sprintf("user%d%d@example.com", bulkID, j)
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

	totalRequests := int64(numBulks * bulkSize)
	throughput := float64(totalRequests) / elapsed.Seconds()

	t.Logf("Database Bulk Operations Load Test:")
	t.Logf("  Total Records: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.2f records/s", throughput)

	helpers.AssertTrue(t, successCount > 0)
}

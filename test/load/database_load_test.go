package load_test

import (
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
				username := "user_" + string(rune('0'+id)) + "_" + string(rune('0'+j))
				email := "user" + string(rune('0'+id)) + string(rune('0'+j)) + "@example.com"
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
	for i := 0; i < 100; i++ {
		username := "user" + string(rune('0'+i))
		email := "user" + string(rune('0'+i)) + "@example.com"
		authService.Register(username, "password", email)
	}

	// Test query performance
	numGoroutines := 50
	numRequests := 20
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
	numGoroutines := 30
	numRequests := 15
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				username := "user_" + string(rune('0'+id)) + "_" + string(rune('0'+j))
				email := "user" + string(rune('0'+id)) + string(rune('0'+j)) + "@example.com"

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
	bulkSize := 100
	numBulks := 10
	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	start := time.Now()

	for i := 0; i < numBulks; i++ {
		wg.Add(1)
		go func(bulkID int) {
			defer wg.Done()
			for j := 0; j < bulkSize; j++ {
				username := "user_" + string(rune('0'+bulkID)) + "_" + string(rune('0'+j))
				email := "user" + string(rune('0'+bulkID)) + string(rune('0'+j)) + "@example.com"
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

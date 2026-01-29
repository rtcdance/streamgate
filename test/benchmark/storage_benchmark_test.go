package benchmark_test

import (
	"context"
	"testing"

	"streamgate/pkg/storage"
	"streamgate/test/helpers"
)

func BenchmarkPostgres_Insert(b *testing.B) {
	db := helpers.SetupTestPostgres(&testing.T{})
	if db == nil {
		b.Skip("PostgreSQL not available")
	}
	defer helpers.CleanupTestPostgres(&testing.T{}, db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate insert operation
		_ = db.Insert(context.Background(), "test_table", map[string]interface{}{
			"name": "test" + string(rune(i)),
		})
	}
}

func BenchmarkPostgres_Query(b *testing.B) {
	db := helpers.SetupTestPostgres(&testing.T{})
	if db == nil {
		b.Skip("PostgreSQL not available")
	}
	defer helpers.CleanupTestPostgres(&testing.T{}, db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = db.Query(context.Background(), "SELECT * FROM test_table LIMIT 10")
	}
}

func BenchmarkPostgres_Update(b *testing.B) {
	db := helpers.SetupTestPostgres(&testing.T{})
	if db == nil {
		b.Skip("PostgreSQL not available")
	}
	defer helpers.CleanupTestPostgres(&testing.T{}, db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = db.Update(context.Background(), "test_table", map[string]interface{}{
			"name": "updated" + string(rune(i)),
		})
	}
}

func BenchmarkPostgres_Delete(b *testing.B) {
	db := helpers.SetupTestPostgres(&testing.T{})
	if db == nil {
		b.Skip("PostgreSQL not available")
	}
	defer helpers.CleanupTestPostgres(&testing.T{}, db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = db.Delete(context.Background(), "test_table", map[string]interface{}{
			"id": i,
		})
	}
}

func BenchmarkRedis_Set(b *testing.B) {
	cache := helpers.SetupTestRedis(&testing.T{})
	if cache == nil {
		b.Skip("Redis not available")
	}
	defer helpers.CleanupTestRedis(&testing.T{}, cache)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(context.Background(), "key"+string(rune(i)), "value", 0)
	}
}

func BenchmarkRedis_Get(b *testing.B) {
	cache := helpers.SetupTestRedis(&testing.T{})
	if cache == nil {
		b.Skip("Redis not available")
	}
	defer helpers.CleanupTestRedis(&testing.T{}, cache)

	// Setup: Set a key
	cache.Set(context.Background(), "test_key", "test_value", 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(context.Background(), "test_key")
	}
}

func BenchmarkRedis_Delete(b *testing.B) {
	cache := helpers.SetupTestRedis(&testing.T{})
	if cache == nil {
		b.Skip("Redis not available")
	}
	defer helpers.CleanupTestRedis(&testing.T{}, cache)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Delete(context.Background(), "key"+string(rune(i)))
	}
}

func BenchmarkObjectStorage_Upload(b *testing.B) {
	storage := helpers.SetupTestStorage(&testing.T{})
	if storage == nil {
		b.Skip("Object storage not available")
	}
	defer helpers.CleanupTestStorage(&testing.T{}, storage)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = storage.Upload(context.Background(), "test_file_"+string(rune(i)), []byte("test content"))
	}
}

func BenchmarkObjectStorage_Download(b *testing.B) {
	storage := helpers.SetupTestStorage(&testing.T{})
	if storage == nil {
		b.Skip("Object storage not available")
	}
	defer helpers.CleanupTestStorage(&testing.T{}, storage)

	// Setup: Upload a file
	storage.Upload(context.Background(), "test_file", []byte("test content"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = storage.Download(context.Background(), "test_file")
	}
}

func BenchmarkObjectStorage_Delete(b *testing.B) {
	storage := helpers.SetupTestStorage(&testing.T{})
	if storage == nil {
		b.Skip("Object storage not available")
	}
	defer helpers.CleanupTestStorage(&testing.T{}, storage)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = storage.Delete(context.Background(), "test_file_"+string(rune(i)))
	}
}

func BenchmarkConnectionPool_Acquire(b *testing.B) {
	db := helpers.SetupTestPostgres(&testing.T{})
	if db == nil {
		b.Skip("PostgreSQL not available")
	}
	defer helpers.CleanupTestPostgres(&testing.T{}, db)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = db.AcquireConnection(context.Background())
		}
	})
}

func BenchmarkConnectionPool_Release(b *testing.B) {
	db := helpers.SetupTestPostgres(&testing.T{})
	if db == nil {
		b.Skip("PostgreSQL not available")
	}
	defer helpers.CleanupTestPostgres(&testing.T{}, db)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn := db.AcquireConnection(context.Background())
			if conn != nil {
				db.ReleaseConnection(conn)
			}
		}
	})
}

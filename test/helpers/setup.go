package helpers

import (
	"database/sql"
	"testing"

	"streamgate/pkg/storage"
)

// TestConfig holds test configuration
type TestConfig struct {
	DBType          string
	DBDSN           string
	StorageType     string
	StorageEndpoint string
	StorageKey      string
	StorageSecret   string
	RedisAddr       string
}

// DefaultTestConfig returns default test configuration
func DefaultTestConfig() TestConfig {
	return TestConfig{
		DBType:          "postgres",
		DBDSN:           "postgres://test:test@localhost:5432/streamgate_test?sslmode=disable",
		StorageType:     "minio",
		StorageEndpoint: "localhost:9000",
		StorageKey:      "minioadmin",
		StorageSecret:   "minioadmin",
		RedisAddr:       "localhost:6379",
	}
}

// SetupTestDB creates a test database connection
func SetupTestDB(t *testing.T) *storage.Database {
	t.Helper()

	config := DefaultTestConfig()
	db, err := storage.NewDatabase(storage.DatabaseConfig{
		Type: config.DBType,
		DSN:  config.DBDSN,
	})
	if err != nil {
		t.Skipf("Skipping test: failed to setup test DB: %v", err)
		return nil
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("Skipping test: database not available: %v", err)
		return nil
	}

	return db
}

// SetupTestStorage creates test object storage
func SetupTestStorage(t *testing.T) *storage.ObjectStorage {
	t.Helper()

	config := DefaultTestConfig()
	objStorage, err := storage.NewObjectStorage(storage.ObjectStorageConfig{
		Type:            config.StorageType,
		Endpoint:        config.StorageEndpoint,
		AccessKeyID:     config.StorageKey,
		SecretAccessKey: config.StorageSecret,
		UseSSL:          false,
	})
	if err != nil {
		t.Skipf("Skipping test: failed to setup test storage: %v", err)
		return nil
	}

	return objStorage
}

// SetupTestRedis creates test Redis cache
func SetupTestRedis(t *testing.T) *storage.RedisCache {
	t.Helper()

	config := DefaultTestConfig()
	cache := storage.NewRedisCache()
	if err := cache.Connect(config.RedisAddr); err != nil {
		t.Skipf("Skipping test: Redis not available: %v", err)
		return nil
	}

	return cache
}

// SetupTestPostgres creates test PostgreSQL connection
func SetupTestPostgres(t *testing.T) *storage.PostgresDB {
	t.Helper()

	config := DefaultTestConfig()
	db := storage.NewPostgresDB()
	if err := db.Connect(config.DBDSN); err != nil {
		t.Skipf("Skipping test: PostgreSQL not available: %v", err)
		return nil
	}

	return db
}

// CleanupTestDB cleans up test database
func CleanupTestDB(t *testing.T, db *storage.Database) {
	t.Helper()
	if db != nil {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to cleanup DB: %v", err)
		}
	}
}

// CleanupTestStorage cleans up test storage
func CleanupTestStorage(t *testing.T, storage *storage.ObjectStorage) {
	t.Helper()
	// Object storage doesn't need explicit cleanup
}

// CleanupTestRedis cleans up test Redis
func CleanupTestRedis(t *testing.T, cache *storage.RedisCache) {
	t.Helper()
	if cache != nil {
		if err := cache.Close(); err != nil {
			t.Errorf("Failed to cleanup Redis: %v", err)
		}
	}
}

// CleanupTestPostgres cleans up test PostgreSQL
func CleanupTestPostgres(t *testing.T, db *storage.PostgresDB) {
	t.Helper()
	if db != nil {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to cleanup PostgreSQL: %v", err)
		}
	}
}

// CreateTestTable creates a test table
func CreateTestTable(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()

	query := `
		CREATE TABLE IF NOT EXISTS ` + tableName + ` (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255),
			value TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
}

// DropTestTable drops a test table
func DropTestTable(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()

	query := "DROP TABLE IF EXISTS " + tableName
	if _, err := db.Exec(query); err != nil {
		t.Errorf("Failed to drop test table: %v", err)
	}
}

// TruncateTestTable truncates a test table
func TruncateTestTable(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()

	query := "TRUNCATE TABLE " + tableName
	if _, err := db.Exec(query); err != nil {
		t.Errorf("Failed to truncate test table: %v", err)
	}
}

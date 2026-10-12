package helpers

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rtcdance/streamgate/pkg/storage"
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

// DefaultTestConfig returns default test configuration.
//
// STREAMGATE_TEST_DB_DSN overrides the database DSN, so the integration suite
// can be pointed at a throwaway database (a CI service container, a local
// acceptance stack) instead of whatever happens to listen on 5432.
func DefaultTestConfig() TestConfig {
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	if dsn == "" {
		dsn = "postgres://streamgate:streamgate@localhost:5432/streamgate?sslmode=disable"
	}
	return TestConfig{
		DBType:          "postgres",
		DBDSN:           dsn,
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
	if err := db.Ping(context.Background()); err != nil {
		_ = db.Close()
		t.Skipf("Skipping test: database not available: %v", err)
		return nil
	}

	// Apply migrations
	if err := ApplyMigrations(t, db); err != nil {
		_ = db.Close()
		t.Skipf("Skipping test: failed to apply migrations: %v", err)
		return nil
	}

	// Clean up existing data to avoid conflicts with concurrent tests
	CleanupTestData(t, db)

	return db
}

// CleanupTestData removes all test data from the database
func CleanupTestData(t *testing.T, db *storage.Database) {
	t.Helper()

	sqlDB := db.GetDB()
	if sqlDB == nil {
		return
	}

	// Truncate all tables in correct order (respecting foreign key constraints)
	tables := []string{
		"transactions",
		"nfts",
		"uploads",
		"transcoding_tasks",
		"streams",
		"tasks",
		"content",
		"users",
	}

	for _, table := range tables {
		_, err := sqlDB.Exec("TRUNCATE TABLE " + table + " CASCADE")
		if err != nil {
			// Ignore errors for tables that might not exist
			continue
		}
	}
}

// ApplyMigrations applies all database migrations
// findMigrationsDir walks up from the test's working directory to the module
// root and returns the migrations directory. A hard-coded relative path only
// resolved from one specific test depth, so every other package silently
// skipped with "failed to read migrations directory".
func findMigrationsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory: %w", err)
	}
	for {
		candidate := filepath.Join(dir, "migrations")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("migrations directory not found above %s", dir)
		}
		dir = parent
	}
}

func ApplyMigrations(t *testing.T, db *storage.Database) error {
	t.Helper()

	sqlDB := db.GetDB()
	if sqlDB == nil {
		return fmt.Errorf("database not available")
	}

	// Find all migration files
	migrationsDir, err := findMigrationsDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Filter and sort migration files. Down-migrations are excluded: applying
	// them as if they were up-migrations both breaks the schema and is the
	// opposite of what a fresh test database needs.
	var migrationFiles []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		if strings.HasSuffix(name, ".down.sql") {
			continue
		}
		migrationFiles = append(migrationFiles, name)
	}
	sort.Strings(migrationFiles)

	// Track what has been applied. SetupTestDB runs once per test against the
	// same persistent database, and the migration set is not idempotent as a
	// whole (033 adds an FK on content_id as UUID, 035 later retypes that column
	// to VARCHAR, so replaying 033 fails with "cannot be implemented"). Record
	// each file the first time it succeeds and skip it afterwards.
	if _, err := sqlDB.Exec(
		`CREATE TABLE IF NOT EXISTS test_applied_migrations (filename TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
	); err != nil {
		return fmt.Errorf("failed to create migration tracking table: %w", err)
	}

	alreadyApplied := func(file string) bool {
		var n int
		if err := sqlDB.QueryRow(
			`SELECT COUNT(*) FROM test_applied_migrations WHERE filename = $1`, file).Scan(&n); err == nil && n > 0 {
			return true
		}
		// Also honour the application's own migrator, so the helper is usable
		// against a database the app has already migrated.
		if err := sqlDB.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE filename = $1`, file).Scan(&n); err == nil && n > 0 {
			return true
		}
		return false
	}

	// Apply each migration
	for _, file := range migrationFiles {
		if alreadyApplied(file) {
			continue
		}
		migrationPath := filepath.Join(migrationsDir, file)
		content, err := os.ReadFile(migrationPath)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		// Execute migration
		_, err = sqlDB.Exec(string(content))
		if err != nil {
			// A duplicate object means the migration was already applied by
			// some other path, which is the state we want.
			if isDuplicateObjectErr(err) {
				_, _ = sqlDB.Exec(`INSERT INTO test_applied_migrations (filename) VALUES ($1) ON CONFLICT DO NOTHING`, file)
				continue
			}
			return fmt.Errorf("failed to apply migration %s: %w", file, err)
		}
		_, _ = sqlDB.Exec(`INSERT INTO test_applied_migrations (filename) VALUES ($1) ON CONFLICT DO NOTHING`, file)
	}

	return nil
}

// isDuplicateObjectErr reports whether err is Postgres refusing to create an
// object that already exists (SQLSTATE 42P07 duplicate_table, 42710
// duplicate_object, 42701 duplicate_column, 42P06 duplicate_schema, 42P16
// invalid_table_definition for a conflicting constraint).
func isDuplicateObjectErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "duplicate key value")
}

// SetupTestStorage creates test object storage
func SetupTestStorage(t *testing.T) storage.ObjectStorage {
	t.Helper()

	config := DefaultTestConfig()
	objStorage, err := storage.NewMinIOStorage(storage.MinIOConfig{
		Endpoint:        config.StorageEndpoint,
		AccessKeyID:     config.StorageKey,
		SecretAccessKey: config.StorageSecret,
		UseSSL:          false,
	})
	if err != nil {
		t.Skipf("Skipping test: failed to setup test storage: %v", err)
		return nil
	}

	// Verify storage is accessible by trying to list objects (will fail if MinIO is not running)
	_, err = objStorage.ListObjects(context.Background(), "test-bucket", "")
	if err != nil {
		t.Skipf("Skipping test: storage not accessible: %v", err)
		return nil
	}

	return objStorage
}

// SetupTestRedis creates test Redis cache
func SetupTestRedis(t *testing.T) *storage.RedisCache {
	t.Helper()

	config := DefaultTestConfig()
	cache := storage.NewRedisCache()
	if err := cache.Connect(storage.RedisConfig{Addr: config.RedisAddr}); err != nil {
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
func CleanupTestStorage(t *testing.T, s storage.ObjectStorage) {
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

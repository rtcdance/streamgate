package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// migrationLockID is a deterministic advisory lock ID derived from
// "streamgate-migration". Only one pod can hold this lock at a time,
// preventing concurrent migration execution during rolling deployments.
const migrationLockID int64 = 0x53747265616D6761 // "Streamga" as int64

// RunMigrations executes SQL migration files from the given directory.
// It tracks applied migrations in a schema_migrations table and skips
// any migration that has already been applied. A PostgreSQL advisory
// lock prevents concurrent pods from running migrations simultaneously.
func RunMigrations(db *sql.DB, migrationsDir string) error {
	// Acquire advisory lock to prevent concurrent migration execution.
	if _, err := db.Exec("SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("failed to acquire migration advisory lock: %w", err)
	}
	defer func() { _, _ = db.Exec("SELECT pg_advisory_unlock($1)", migrationLockID) }()

	// Create migrations tracking table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Read migration files
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory %s: %w", migrationsDir, err)
	}

	var sqlFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			sqlFiles = append(sqlFiles, e.Name())
		}
	}
	sort.Strings(sqlFiles)

	for _, name := range sqlFiles {
		// Check if already applied
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", name).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		// Read and execute
		content, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", name, err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", name, err)
		}

		// Record as applied
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", name, err)
		}
	}

	return nil
}

// RunEmbeddedMigrations executes SQL migration files from an embedded filesystem.
// It uses the same schema_migrations table and advisory lock as RunMigrations.
func RunEmbeddedMigrations(db *sql.DB, fs embed.FS, dir string) error {
	// Acquire advisory lock to prevent concurrent migration execution.
	if _, err := db.Exec("SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("failed to acquire migration advisory lock: %w", err)
	}
	defer func() { _, _ = db.Exec("SELECT pg_advisory_unlock($1)", migrationLockID) }()

	// Create migrations tracking table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Read migration files from embedded FS
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations dir %s: %w", dir, err)
	}

	var sqlFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			sqlFiles = append(sqlFiles, e.Name())
		}
	}
	sort.Strings(sqlFiles)

	for _, name := range sqlFiles {
		// Check if already applied
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", name).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		// Read and execute
		content, err := fs.ReadFile(dir + "/" + name)
		if err != nil {
			return fmt.Errorf("failed to read embedded migration %s: %w", name, err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", name, err)
		}

		// Record as applied
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", name, err)
		}
	}

	return nil
}

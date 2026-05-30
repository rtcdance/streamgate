// Package migrate provides a programmatic database migration runner.
//
// Usage:
//
//	runner, err := migrate.New(db, migrations.FS)
//	if err != nil { return err }
//	if err := runner.Up(); err != nil { return err }
package migrate

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// Migration tracks an applied migration.
type Migration struct {
	Version  string
	Name     string
	Applied  bool
	FileName string
}

// Runner applies SQL migrations in order with version tracking.
type Runner struct {
	db          *sql.DB
	migrationFS embed.FS
}

// New creates a migration runner.
// db: database connection.
// migrationFS: embedded filesystem containing .sql files.
func New(db *sql.DB, migrationFS embed.FS) *Runner {
	return &Runner{db: db, migrationFS: migrationFS}
}

// upMigrations returns the list of pending up migration files (sorted).
func (r *Runner) upMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(r.migrationFS, ".")
	if err != nil {
		return nil, fmt.Errorf("read migration dir: %w", err)
	}

	var migrations []Migration
	for _, e := range entries {
		name := e.Name()
		// Match: NNN_name.sql (not .down.sql)
		if !strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".down.sql") {
			continue
		}
		// Extract version prefix
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}
		migrations = append(migrations, Migration{
			Version:  parts[0],
			Name:     strings.TrimSuffix(parts[1], ".sql"),
			FileName: name,
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		vi, erri := strconv.Atoi(migrations[i].Version)
		vj, errj := strconv.Atoi(migrations[j].Version)
		if erri == nil && errj == nil {
			return vi < vj
		}
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// Up applies all pending migrations in order.
// Each migration runs in its own transaction.
func (r *Runner) Up() error {
	if err := r.ensureTrackingTable(); err != nil {
		return fmt.Errorf("ensure tracking table: %w", err)
	}

	migrations, err := r.upMigrations()
	if err != nil {
		return err
	}

	applied, err := r.applied()
	if err != nil {
		return fmt.Errorf("query applied: %w", err)
	}

	appliedSet := make(map[string]bool, len(applied))
	for _, v := range applied {
		appliedSet[v] = true
	}

	for _, m := range migrations {
		if appliedSet[m.Version] {
			continue
		}

		sqlBytes, err := r.migrationFS.ReadFile(m.FileName)
		if err != nil {
			return fmt.Errorf("read %s: %w", m.FileName, err)
		}

		tx, err := r.db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", m.FileName, err)
		}

		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", m.FileName, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, name, filename, applied_at)
			 VALUES ($1, $2, $3, NOW())`,
			m.Version, m.Name, m.FileName,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", m.FileName, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", m.FileName, err)
		}
	}

	return nil
}

// Down rolls back the last N migrations by applying their .down.sql files.
func (r *Runner) Down(steps int) error {
	if steps <= 0 {
		return nil
	}

	if err := r.ensureTrackingTable(); err != nil {
		return fmt.Errorf("ensure tracking table: %w", err)
	}

	applied, err := r.applied()
	if err != nil {
		return fmt.Errorf("query applied: %w", err)
	}

	if len(applied) == 0 {
		return nil
	}

	if steps > len(applied) {
		steps = len(applied)
	}

	// Roll back the last N migrations in reverse order
	for i := len(applied) - 1; i >= len(applied)-steps; i-- {
		version := applied[i]

		// Find the migration file that matches
		migrations, _ := r.upMigrations()
		var fileName string
		for _, m := range migrations {
			if m.Version == version {
				fileName = m.FileName
				break
			}
		}
		if fileName == "" {
			return fmt.Errorf("migration %s not found in filesystem", version)
		}

		downFile := strings.TrimSuffix(fileName, ".sql") + ".down.sql"
		sqlBytes, err := r.migrationFS.ReadFile(downFile)
		if err != nil {
			return fmt.Errorf("read %s (rollback %s): %w", downFile, version, err)
		}

		tx, err := r.db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for rollback %s: %w", version, err)
		}

		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply rollback %s: %w", version, err)
		}

		if _, err := tx.Exec(
			`DELETE FROM schema_migrations WHERE version = $1`, version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("remove tracking %s: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit rollback %s: %w", version, err)
		}
	}

	return nil
}

// Applied returns the list of applied migration versions in order.
func (r *Runner) applied() ([]string, error) {
	err := r.ensureTrackingTable()
	if err != nil {
		return nil, err
	}

	rows, err := r.db.Query(
		`SELECT version FROM schema_migrations ORDER BY applied_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (r *Runner) ensureTrackingTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    VARCHAR(10)   PRIMARY KEY,
			name       VARCHAR(255)  NOT NULL,
			filename   VARCHAR(255)  NOT NULL DEFAULT '',
			applied_at TIMESTAMPTZ   NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return err
	}
	r.db.Exec(`ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS filename VARCHAR(255) NOT NULL DEFAULT ''`)
	return nil
}

package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

func RunEmbeddedMigrations(db *sql.DB, migrationFS embed.FS, dir string) error {
	m := NewMigrator(db, zap.NewNop(), migrationFS, dir)
	return m.Up(context.Background())
}

type Migrator struct {
	db    *sql.DB
	log   *zap.Logger
	fs    embed.FS
	dir   string
	table string
}

func NewMigrator(db *sql.DB, log *zap.Logger, migrationFS embed.FS, dir string) *Migrator {
	return &Migrator{
		db:    db,
		log:   log,
		fs:    migrationFS,
		dir:   dir,
		table: "schema_migrations",
	}
}

func (m *Migrator) Up(ctx context.Context) error {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	if dirty, err := m.hasDirtyMigration(ctx); err != nil {
		return fmt.Errorf("check dirty migrations: %w", err)
	} else if dirty {
		return fmt.Errorf("database has dirty migration — manual intervention required: inspect schema_migrations table, fix the issue, then call Force(version) to mark clean")
	}

	files, err := m.readMigrationFiles()
	if err != nil {
		return fmt.Errorf("read migration files: %w", err)
	}

	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("get applied migrations: %w", err)
	}

	for _, f := range files {
		if applied[f.Version] {
			m.log.Debug("migration already applied", zap.String("version", f.Version), zap.String("name", f.Name))
			continue
		}

		m.log.Info("applying migration", zap.String("version", f.Version), zap.String("name", f.Name))
		start := time.Now()

		if err := m.applyMigration(ctx, f); err != nil {
			return fmt.Errorf("apply migration %s: %w", f.Version, err)
		}

		m.log.Info("migration applied",
			zap.String("version", f.Version),
			zap.String("name", f.Name),
			zap.Duration("duration", time.Since(start)),
		)
	}

	return nil
}

func (m *Migrator) Down(ctx context.Context, targetVersion int) error {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	files, err := m.readMigrationFiles()
	if err != nil {
		return fmt.Errorf("read migration files: %w", err)
	}

	downFiles, err := m.readDownFiles()
	if err != nil {
		return fmt.Errorf("read down migration files: %w", err)
	}

	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("get applied migrations: %w", err)
	}

	if err := m.ensureNoDirty(ctx, applied); err != nil {
		return err
	}

	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		versionNum, _ := strconv.Atoi(f.Version)
		if versionNum <= targetVersion {
			break
		}
		if !applied[f.Version] {
			continue
		}

		m.log.Info("rolling back migration", zap.String("version", f.Version), zap.String("name", f.Name))
		start := time.Now()

		if err := m.rollbackMigration(ctx, f, downFiles); err != nil {
			return fmt.Errorf("rollback migration %s: %w", f.Version, err)
		}

		m.log.Info("migration rolled back",
			zap.String("version", f.Version),
			zap.String("name", f.Name),
			zap.Duration("duration", time.Since(start)),
		)
	}

	return nil
}

func (m *Migrator) Force(version int) error {
	ctx := context.Background()
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}
	verStr := fmt.Sprintf("%03d", version)
	_, err := m.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET dirty = FALSE WHERE version = $1", m.table),
		verStr,
	)
	return err
}

func (m *Migrator) Version(ctx context.Context) (version string, dirty bool, err error) {
	if err = m.ensureMigrationsTable(ctx); err != nil {
		return
	}

	err = m.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT version, dirty FROM %s ORDER BY version DESC LIMIT 1", m.table),
	).Scan(&version, &dirty)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return version, dirty, nil
}

func (m *Migrator) ensureMigrationsTable(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version VARCHAR(255) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			dirty BOOLEAN NOT NULL DEFAULT FALSE,
			applied_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)
	`, m.table))
	return err
}

type migrationFile struct {
	Version string
	Name    string
	Content string
}

func (m *Migrator) readMigrationFiles() ([]migrationFile, error) {
	entries, err := fs.ReadDir(m.fs, m.dir)
	if err != nil {
		return nil, err
	}

	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		if strings.Contains(name, ".down.") {
			continue
		}

		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			continue
		}

		version := parts[0]
		migrationName := strings.TrimSuffix(parts[1], ".sql")

		content, err := fs.ReadFile(m.fs, m.dir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		files = append(files, migrationFile{
			Version: version,
			Name:    migrationName,
			Content: string(content),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		vi, _ := strconv.Atoi(files[i].Version)
		vj, _ := strconv.Atoi(files[j].Version)
		return vi < vj
	})

	return files, nil
}

func (m *Migrator) readDownFiles() (map[string]string, error) {
	entries, err := fs.ReadDir(m.fs, m.dir)
	if err != nil {
		return nil, err
	}

	downFiles := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.Contains(name, ".down.") {
			continue
		}

		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			continue
		}
		version := parts[0]

		content, err := fs.ReadFile(m.fs, m.dir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		downFiles[version] = string(content)
	}

	return downFiles, nil
}

func (m *Migrator) getAppliedMigrations(ctx context.Context) (map[string]bool, error) {
	rows, err := m.db.QueryContext(ctx, fmt.Sprintf("SELECT version FROM %s", m.table))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

func (m *Migrator) hasDirtyMigration(ctx context.Context) (bool, error) {
	var count int
	err := m.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE dirty = TRUE", m.table),
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *Migrator) ensureNoDirty(ctx context.Context, applied map[string]bool) error {
	rows, err := m.db.QueryContext(ctx, fmt.Sprintf("SELECT version FROM %s WHERE dirty = TRUE", m.table))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	var dirtyVersions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return err
		}
		dirtyVersions = append(dirtyVersions, v)
	}
	if rows.Err() != nil {
		return rows.Err()
	}
	if len(dirtyVersions) > 0 {
		return fmt.Errorf("cannot rollback: database has dirty migrations (%s) — fix manually and run Force(version) first",
			strings.Join(dirtyVersions, ", "))
	}
	return nil
}

func (m *Migrator) applyMigration(ctx context.Context, f migrationFile) error {
	_, err := m.db.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (version, name, dirty) VALUES ($1, $2, TRUE) ON CONFLICT (version) DO UPDATE SET dirty = TRUE", m.table),
		f.Version, f.Name,
	)
	if err != nil {
		return fmt.Errorf("record migration start: %w", err)
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, f.Content); err != nil {
		return fmt.Errorf("execute SQL: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET dirty = FALSE, applied_at = NOW() WHERE version = $1", m.table),
		f.Version,
	); err != nil {
		return fmt.Errorf("mark migration clean: %w", err)
	}

	return tx.Commit()
}

func (m *Migrator) rollbackMigration(ctx context.Context, f migrationFile, downFiles map[string]string) error {
	downSQL, hasDown := downFiles[f.Version]

	_, err := m.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET dirty = TRUE WHERE version = $1", m.table),
		f.Version,
	)
	if err != nil {
		return fmt.Errorf("mark migration dirty for rollback: %w", err)
	}

	if hasDown {
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		if _, err := tx.ExecContext(ctx, downSQL); err != nil {
			return fmt.Errorf("execute down SQL: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE version = $1", m.table),
			f.Version,
		); err != nil {
			return fmt.Errorf("remove migration record: %w", err)
		}

		return tx.Commit()
	}

	_, err = m.db.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE version = $1", m.table),
		f.Version,
	)
	return err
}
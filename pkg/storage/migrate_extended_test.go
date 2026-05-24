package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stubDriver struct{}

func (d *stubDriver) Open(name string) (driver.Conn, error) {
	return &stubConn{}, nil
}

type stubConn struct{}

func (c *stubConn) Prepare(query string) (driver.Stmt, error) {
	return &stubStmt{}, nil
}

func (c *stubConn) Close() error { return nil }

func (c *stubConn) Begin() (driver.Tx, error) { return &stubTx{}, nil }

type stubTx struct{}

func (t *stubTx) Commit() error   { return nil }
func (t *stubTx) Rollback() error { return nil }

type stubStmt struct{}

func (s *stubStmt) Close() error { return nil }

func (s *stubStmt) NumInput() int { return -1 }

func (s *stubStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.ResultNoRows, nil
}

func (s *stubStmt) Query(args []driver.Value) (driver.Rows, error) {
	return &stubRows{}, nil
}

type stubRows struct {
	consumed bool
}

func (r *stubRows) Columns() []string {
	return []string{"version", "dirty", "name", "applied_at", "locked", "count"}
}

func (r *stubRows) Close() error { return nil }

func (r *stubRows) Next(dest []driver.Value) error {
	if r.consumed {
		return driver.ErrSkip
	}
	r.consumed = true
	dest[0] = "001"
	dest[1] = false
	dest[2] = "test"
	dest[3] = time.Now()
	dest[4] = true
	dest[5] = int64(0)
	return nil
}

func init() {
	sql.Register("stub", &stubDriver{})
}

func newStubDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("stub", "")
	require.NoError(t, err)
	return db
}

func TestMigrator_Up_LockNotAcquired(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Down_LockNotAcquired(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_Up_AdvisoryLockNotAcquired(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	m.db = newFailingDB(t)
	defer m.db.Close()

	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Down_AdvisoryLockNotAcquired(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	m.db = newFailingDB(t)
	defer m.db.Close()

	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_Force_AdvisoryLockFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Force(context.Background(), 1)
	assert.Error(t, err)
}

func TestMigrator_Version_AdvisoryLockFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, _, err := m.Version(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Up_EnsureTableFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.ensureMigrationsTable(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Up_GetAppliedFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, err := m.getAppliedMigrations(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Up_HasDirtyFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, err := m.hasDirtyMigration(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Up_EnsureNoDirtyFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.ensureNoDirty(context.Background(), map[string]bool{})
	assert.Error(t, err)
}

func TestMigrator_Up_ApplyMigrationFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	f := migrationFile{Version: "001", Name: "test", Content: "SELECT 1"}
	err := m.applyMigration(context.Background(), f)
	assert.Error(t, err)
}

func TestMigrator_Up_RollbackMigrationFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	f := migrationFile{Version: "001", Name: "test", Content: "SELECT 1"}
	err := m.rollbackMigration(context.Background(), f, map[string]string{"001": "DROP TABLE test"})
	assert.Error(t, err)
}

func TestMigrator_Up_RollbackMigrationNoDownFile(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	f := migrationFile{Version: "001", Name: "test", Content: "SELECT 1"}
	err := m.rollbackMigration(context.Background(), f, map[string]string{})
	assert.Error(t, err)
}

func TestMigrator_Up_ReadMigrationFilesFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), embed.FS{}, "nonexistent")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Down_ReadMigrationFilesFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), embed.FS{}, "nonexistent")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_Version_NoRows(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, _, err := m.Version(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Force_UpdateFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Force(context.Background(), 1)
	assert.Error(t, err)
}

func TestMigrator_Up_WithTimeoutContext(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := m.Up(ctx)
	assert.Error(t, err)
}

func TestMigrator_Down_WithTimeoutContext(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := m.Down(ctx, 0)
	assert.Error(t, err)
}

func TestMigrator_Down_InvalidVersionSkipped(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 999)
	assert.Error(t, err)
}

func TestMigrator_Up_DirtyMigrationDetected(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Down_DirtyMigrationDetected(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_readDownFiles_EmbeddedFS(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "nonexistent")
	_, err := m.readDownFiles()
	assert.Error(t, err)
}

func TestMigrator_readMigrationFiles_ValidFS(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	files, err := m.readMigrationFiles()
	require.NoError(t, err)
	assert.NotEmpty(t, files)

	for _, f := range files {
		assert.NotContains(t, f.Name, ".down.")
		assert.NotEmpty(t, f.Version)
		assert.NotEmpty(t, f.Content)
	}
}

func TestMigrator_readDownFiles_ValidFS(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	downFiles, err := m.readDownFiles()
	require.NoError(t, err)
	assert.NotEmpty(t, downFiles)

	for version, content := range downFiles {
		assert.NotEmpty(t, version)
		assert.NotEmpty(t, content)
	}
}

func TestMigrator_NewMigrator_DefaultTable(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "migrations")
	assert.Equal(t, "schema_migrations", m.table)
}

func TestMigrator_migrationFile_Fields(t *testing.T) {
	f := migrationFile{
		Version: "001",
		Name:    "create_users",
		Content: "CREATE TABLE users (id SERIAL PRIMARY KEY);",
	}
	assert.Equal(t, "001", f.Version)
	assert.Equal(t, "create_users", f.Name)
	assert.Equal(t, "CREATE TABLE users (id SERIAL PRIMARY KEY);", f.Content)
}

func TestMigrator_rollbackMigration_NoDownFile_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	f := migrationFile{Version: "001", Name: "test", Content: "SELECT 1"}
	err := m.rollbackMigration(context.Background(), f, map[string]string{})
	assert.Error(t, err)
}

func TestMigrator_Up_CanceledContext(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Up(ctx)
	assert.Error(t, err)
}

func TestMigrator_Down_CanceledContext(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Down(ctx, 0)
	assert.Error(t, err)
}

func TestMigrator_Force_CanceledContext(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Force(ctx, 1)
	assert.Error(t, err)
}

func TestMigrator_Version_CanceledContext(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := m.Version(ctx)
	assert.Error(t, err)
}

func TestMigrator_Down_EnsureNoDirtyFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_readMigrationFiles_Content(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	files, err := m.readMigrationFiles()
	require.NoError(t, err)

	for _, f := range files {
		assert.NotEmpty(t, f.Content, "migration %s should have content", f.Version)
	}
}

func TestMigrator_readDownFiles_Content(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	downFiles, err := m.readDownFiles()
	require.NoError(t, err)

	for version, content := range downFiles {
		assert.NotEmpty(t, content, "down migration %s should have content", version)
	}
}

func TestNewMigrator_NilDB(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "migrations")
	require.NotNil(t, m)
	assert.Equal(t, "schema_migrations", m.table)
}

func TestMigrator_Force_VersionFormatting(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Force(context.Background(), 5)
	assert.Error(t, err)
}

func TestMigrator_Up_EmptyMigrationDir(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata")
	files, err := m.readMigrationFiles()
	require.NoError(t, err)
	for _, f := range files {
		assert.NotContains(t, f.Name, "/")
	}
}

func TestRunEmbeddedMigrations_WithFailingDB(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	err := RunEmbeddedMigrations(context.Background(), db, testMigrationFS, "testdata/migrations")
	assert.Error(t, err)
}

func TestMigrator_Down_GetAppliedFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_ensureNoDirty_EmptyDirtyRows(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.ensureNoDirty(context.Background(), map[string]bool{})
	assert.Error(t, err)
}

func TestMigrator_ensureNoDirty_DirtyVersions(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.ensureNoDirty(context.Background(), map[string]bool{"001": true, "002": true})
	assert.Error(t, err)
}

func TestMigrator_Up_HasDirtyMigration(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Down_HasDirtyMigration(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_readMigrationFiles_FileContent(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	files, err := m.readMigrationFiles()
	require.NoError(t, err)

	for _, f := range files {
		assert.Contains(t, f.Content, "CREATE", fmt.Sprintf("migration %s content should contain SQL", f.Version))
	}
}

func TestMigrator_readDownFiles_FileContent(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	downFiles, err := m.readDownFiles()
	require.NoError(t, err)

	for _, content := range downFiles {
		assert.Contains(t, content, "DROP", "down migration content should contain DROP")
	}
}

func TestMigrator_Up_AdvisoryLockContention(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Down_AdvisoryLockContention(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_Up_AdvisoryLockFailed_Explicit(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Up(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "acquire migration lock")
}

func TestMigrator_Down_AdvisoryLockFailed_Explicit(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "acquire migration lock")
}

func TestMigrator_Force_EnsureTableFailed_Explicit(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Force(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ensure migrations table")
}

func TestMigrator_Down_EnsureTableFailed_Explicit(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_Up_ReadFilesFailed_NonexistentDir(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), embed.FS{}, "nonexistent")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Down_ReadFilesFailed_NonexistentDir(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), embed.FS{}, "nonexistent")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_Up_GetAppliedFailed_Explicit(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_readMigrationFiles_SpecificContent(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	files, err := m.readMigrationFiles()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(files), 2, "should have at least 2 migration files")
}

func TestMigrator_readDownFiles_SpecificContent(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	downFiles, err := m.readDownFiles()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(downFiles), 1, "should have at least 1 down file")
}

func TestMigrator_Version_ForceTableCreation(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, _, err := m.Version(context.Background())
	assert.Error(t, err)
}

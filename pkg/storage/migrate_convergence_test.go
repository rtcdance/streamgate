package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
	"testing"

	migrations "github.com/rtcdance/streamgate/migrations"
	migrate "github.com/rtcdance/streamgate/pkg/storage/migrate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// convergeDriver records every prepared statement so the test can assert the
// migrator converges on the shared schema_migrations shape.
type convergeDriver struct {
	mu    *sync.Mutex
	stmts *[]string
	execs *[]convergeExec
}

func (d *convergeDriver) Open(name string) (driver.Conn, error) {
	return &convergeConn{d: d}, nil
}

type convergeConn struct{ d *convergeDriver }

func (c *convergeConn) Prepare(query string) (driver.Stmt, error) {
	c.d.mu.Lock()
	*c.d.stmts = append(*c.d.stmts, query)
	c.d.mu.Unlock()
	return &convergeStmt{d: c.d, query: query}, nil
}
func (c *convergeConn) Close() error              { return nil }
func (c *convergeConn) Begin() (driver.Tx, error) { return convergeTx{}, nil }

type convergeTx struct{}

func (convergeTx) Commit() error   { return nil }
func (convergeTx) Rollback() error { return nil }

type convergeStmt struct {
	d     *convergeDriver
	query string
}

func (s *convergeStmt) Close() error  { return nil }
func (s *convergeStmt) NumInput() int { return -1 }
func (s *convergeStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.d.mu.Lock()
	*s.d.execs = append(*s.d.execs, convergeExec{query: s.query, args: args})
	s.d.mu.Unlock()
	return driver.RowsAffected(1), nil
}
func (s *convergeStmt) Query(args []driver.Value) (driver.Rows, error) {
	return nil, driver.ErrSkip
}

// convergeExec records an Exec together with its arguments; the arguments are
// what identify the advisory lock, so a test can prove both migration
// implementations take the same one.
type convergeExec struct {
	query string
	args  []driver.Value
}

var convergeMu sync.Mutex
var convergeStmts []string
var convergeExecs []convergeExec

func init() {
	sql.Register("converge", &convergeDriver{mu: &convergeMu, stmts: &convergeStmts, execs: &convergeExecs})
}

// The gateway migrator and pkg/storage/migrate's runner share the table name
// but historically used different columns (dirty vs filename). Whichever binary
// starts first wins the CREATE TABLE, and the other runner then failed its
// checks forever — silently skipping its migrations. The migrator must ALTER a
// pre-existing runner-created table to add the dirty column.
func TestMigrator_EnsureTableConvergesOnRunnerSchema(t *testing.T) {
	convergeMu.Lock()
	convergeStmts = nil
	convergeMu.Unlock()

	db, err := sql.Open("converge", "")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	require.NoError(t, m.ensureMigrationsTable(context.Background()))

	convergeMu.Lock()
	defer convergeMu.Unlock()
	var sawCreate, sawAlter bool
	for _, q := range convergeStmts {
		u := strings.ToUpper(q)
		if strings.Contains(u, "CREATE TABLE IF NOT EXISTS SCHEMA_MIGRATIONS") {
			sawCreate = true
		}
		if strings.Contains(u, "ALTER TABLE") && strings.Contains(u, "DIRTY") && strings.Contains(u, "IF NOT EXISTS") {
			sawAlter = true
		}
	}
	assert.True(t, sawCreate, "must create the table when absent")
	assert.True(t, sawAlter,
		"must converge a runner-created table by adding the dirty column; statements: %v", convergeStmts)
}

// The migrator must take the advisory lock the runner takes, and take it
// before any DDL. The two implementations used different keys (and the
// try-lock variant skipped the run entirely when the lock was held, reporting
// success over a half-built schema) — two processes then raced CREATE TABLE
// on schema_migrations and one died on the pg_type unique index.
func TestMigrator_Up_TakesTheSharedMigrationLockFirst(t *testing.T) {
	convergeMu.Lock()
	convergeStmts = nil
	convergeExecs = nil
	convergeMu.Unlock()

	db, err := sql.Open("converge", "")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	// The fake serves no result set, so the run ends at the dirty check —
	// after the lock, which is all this test needs.
	require.Error(t, m.Up(context.Background()))

	convergeMu.Lock()
	defer convergeMu.Unlock()
	require.NotEmpty(t, convergeExecs)
	assert.Equal(t, "SELECT pg_advisory_lock($1)", convergeExecs[0].query,
		"the lock must precede every DDL statement: %v", convergeExecs)
	require.Len(t, convergeExecs[0].args, 1)
	assert.Equal(t, migrate.MigrationLockID(), convergeExecs[0].args[0].(int64),
		"the migrator must lock the same id the runner locks; a private id leaves the race open")
}

// The gateway and the monolith now apply ONE migration set, embedded at the
// repository root rather than in a subdirectory, so the migrator is driven with
// dir "." — a combination that only works with a path.Join-style lookup. Older
// code sized the set from a dir-aware join, and "./001_x.sql" is not a valid
// fs path, so every file would be unreadable and the whole set skipped.
func TestMigrator_RootEmbeddedSetIsReadableAtTopLevelDir(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), migrations.FS, ".")

	files, err := m.readMigrationFiles()
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, f := range files {
		assert.NotEmpty(t, f.Content, "%s must be readable, not just listed", f.Version)
	}

	seen := make(map[string]bool, len(files))
	for _, f := range files {
		assert.False(t, seen[f.Version], "version %s appears twice in the shared set", f.Version)
		seen[f.Version] = true
	}

	downFiles, err := m.readDownFiles()
	require.NoError(t, err)
	assert.NotEmpty(t, downFiles)
}

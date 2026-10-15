package migrate

import (
	"database/sql"
	"database/sql/driver"
	"embed"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockRecordingDriver captures the statements (and arguments) a run issues, so
// a test can assert which advisory lock the runner takes without a real
// PostgreSQL.
type lockRecordingDriver struct {
	execs *[]lockExec
}

type lockExec struct {
	query string
	args  []driver.Value
}

func (d *lockRecordingDriver) Open(name string) (driver.Conn, error) {
	return &lockRecordingConn{d: d}, nil
}

type lockRecordingConn struct{ d *lockRecordingDriver }

func (c *lockRecordingConn) Prepare(query string) (driver.Stmt, error) {
	return &lockRecordingStmt{d: c.d, query: query}, nil
}
func (c *lockRecordingConn) Close() error              { return nil }
func (c *lockRecordingConn) Begin() (driver.Tx, error) { return lockRecordingTx{}, nil }

type lockRecordingTx struct{}

func (lockRecordingTx) Commit() error   { return nil }
func (lockRecordingTx) Rollback() error { return nil }

type lockRecordingStmt struct {
	d     *lockRecordingDriver
	query string
}

func (s *lockRecordingStmt) Close() error  { return nil }
func (s *lockRecordingStmt) NumInput() int { return -1 }
func (s *lockRecordingStmt) Exec(args []driver.Value) (driver.Result, error) {
	*s.d.execs = append(*s.d.execs, lockExec{query: s.query, args: args})
	return driver.RowsAffected(0), nil
}
func (s *lockRecordingStmt) Query(args []driver.Value) (driver.Rows, error) {
	// The fake cannot serve a result set, so Up stops right after the lock.
	return nil, driver.ErrSkip
}

//go:embed 900_*.sql 901_*.sql
var testMigrateFS embed.FS

func TestNew(t *testing.T) {
	r := New(nil, testMigrateFS)
	require.NotNil(t, r)
}

func TestRunner_upMigrations_ValidFS(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	assert.NotEmpty(t, migrations)
}

func TestRunner_upMigrations_EmptyFS(t *testing.T) {
	r := New(nil, embed.FS{})
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	assert.Empty(t, migrations)
}

func TestRunner_upMigrations_SkipsDownFiles(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)

	for _, m := range migrations {
		assert.NotContains(t, m.FileName, ".down.",
			"upMigrations should skip .down.sql files")
	}
}

func TestRunner_upMigrations_SortedByVersion(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)

	for i := 1; i < len(migrations); i++ {
		assert.LessOrEqual(t, migrations[i-1].Version, migrations[i].Version,
			"migrations should be sorted by version")
	}
}

func TestRunner_upMigrations_VersionAndName(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	require.NotEmpty(t, migrations)

	for _, m := range migrations {
		assert.NotEmpty(t, m.Version, "version should not be empty")
		assert.NotEmpty(t, m.Name, "name should not be empty")
		assert.NotEmpty(t, m.FileName, "filename should not be empty")
		assert.Contains(t, m.FileName, ".sql")
	}
}

func TestRunner_Up_NilDB(t *testing.T) {
	r := New(nil, testMigrateFS)
	assert.Panics(t, func() {
		_ = r.Up()
	})
}

func TestRunner_Down_ZeroSteps(t *testing.T) {
	r := New(nil, testMigrateFS)
	err := r.Down(0)
	require.NoError(t, err)
}

func TestRunner_Down_NegativeSteps(t *testing.T) {
	r := New(nil, testMigrateFS)
	err := r.Down(-1)
	require.NoError(t, err)
}

func TestRunner_Down_NilDB(t *testing.T) {
	r := New(nil, testMigrateFS)
	assert.Panics(t, func() {
		_ = r.Down(1)
	})
}

func TestRunner_Up_ConnectionError(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	r := New(db, testMigrateFS)
	err = r.Up()
	require.Error(t, err)
}

func TestRunner_Down_ConnectionError(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	r := New(db, testMigrateFS)
	err = r.Down(1)
	require.Error(t, err)
}

func TestMigration_Fields(t *testing.T) {
	m := Migration{
		Version:  "001",
		Name:     "create_users",
		Applied:  false,
		FileName: "001_create_users.sql",
	}
	assert.Equal(t, "001", m.Version)
	assert.Equal(t, "create_users", m.Name)
	assert.False(t, m.Applied)
	assert.Equal(t, "001_create_users.sql", m.FileName)
}

func TestRunner_upMigrations_Count(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	assert.Len(t, migrations, 2)
}

func TestRunner_upMigrations_VersionPrefix(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	require.Len(t, migrations, 2)

	assert.Equal(t, "900", migrations[0].Version)
	assert.Equal(t, "901", migrations[1].Version)
}

// The runner (monolith) and pkg/storage.Migrator (api-gateway) migrate the same
// database at startup. They must take the SAME advisory lock: with a
// per-implementation key both processes interleaved CREATE TABLE on
// schema_migrations and the loser died on the pg_type unique index.
func TestRunner_Up_TakesTheSharedMigrationLock(t *testing.T) {
	var execs []lockExec
	sql.Register("lockrecording", &lockRecordingDriver{execs: &execs})
	db, err := sql.Open("lockrecording", "")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	r := New(db, testMigrateFS)
	// The fake driver serves no result set, so the run stops right after the
	// lock — which is all this test needs to observe.
	require.Error(t, r.Up())

	require.NotEmpty(t, execs)
	assert.Equal(t, "SELECT pg_advisory_lock($1)", execs[0].query,
		"the shared lock must be the first statement of the run, before any DDL")
	require.Len(t, execs[0].args, 1)
	assert.Equal(t, MigrationLockID(), execs[0].args[0].(int64))

	var unlockArgs []driver.Value
	for _, e := range execs {
		if e.query == "SELECT pg_advisory_unlock($1)" {
			unlockArgs = e.args
		}
	}
	require.NotNil(t, unlockArgs, "the lock must be released: %v", execs)
	assert.Equal(t, MigrationLockID(), unlockArgs[0].(int64))
}

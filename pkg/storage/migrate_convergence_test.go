package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// convergeDriver records every prepared statement so the test can assert the
// migrator converges on the shared schema_migrations shape.
type convergeDriver struct {
	mu    *sync.Mutex
	stmts *[]string
}

func (d *convergeDriver) Open(name string) (driver.Conn, error) {
	return &convergeConn{d: d}, nil
}

type convergeConn struct{ d *convergeDriver }

func (c *convergeConn) Prepare(query string) (driver.Stmt, error) {
	c.d.mu.Lock()
	*c.d.stmts = append(*c.d.stmts, query)
	c.d.mu.Unlock()
	return &convergeStmt{}, nil
}
func (c *convergeConn) Close() error              { return nil }
func (c *convergeConn) Begin() (driver.Tx, error) { return convergeTx{}, nil }

type convergeTx struct{}

func (convergeTx) Commit() error   { return nil }
func (convergeTx) Rollback() error { return nil }

type convergeStmt struct{}

func (s *convergeStmt) Close() error  { return nil }
func (s *convergeStmt) NumInput() int { return -1 }
func (s *convergeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (s *convergeStmt) Query(args []driver.Value) (driver.Rows, error) {
	return nil, driver.ErrSkip
}

var convergeMu sync.Mutex
var convergeStmts []string

func init() { sql.Register("converge", &convergeDriver{mu: &convergeMu, stmts: &convergeStmts}) }

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

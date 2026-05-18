package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

const (
	defaultMaxRetries   = 3
	defaultRetryBackoff = time.Second

	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 5 * time.Minute
	defaultConnMaxIdleTime = 1 * time.Minute
)

var errNotConnectedDB *sql.DB

func init() {
	errNotConnectedDB, _ = sql.Open("postgres", "host=invalid")
	errNotConnectedDB.Close()
}

// PostgresDB handles PostgreSQL database
type PostgresDB struct {
	db  *sql.DB
	dsn string
}

// NewPostgresDB creates a new PostgreSQL database instance
func NewPostgresDB() *PostgresDB {
	return &PostgresDB{}
}

// NewPostgresDBFromDB creates a PostgresDB wrapping an existing *sql.DB.
// The caller is responsible for having already verified connectivity.
func NewPostgresDBFromDB(db *sql.DB) *PostgresDB {
	return &PostgresDB{db: db}
}

// SetMaxOpenConns sets the maximum number of open connections.
func (pdb *PostgresDB) SetMaxOpenConns(n int) {
	if pdb.db != nil {
		pdb.db.SetMaxOpenConns(n)
	}
}

// SetMaxIdleConns sets the maximum number of idle connections.
func (pdb *PostgresDB) SetMaxIdleConns(n int) {
	if pdb.db != nil {
		pdb.db.SetMaxIdleConns(n)
	}
}

// SetConnMaxLifetime sets the maximum lifetime of a connection.
func (pdb *PostgresDB) SetConnMaxLifetime(d time.Duration) {
	if pdb.db != nil {
		pdb.db.SetConnMaxLifetime(d)
	}
}

func (pdb *PostgresDB) SetConnMaxIdleTime(d time.Duration) {
	if pdb.db != nil {
		pdb.db.SetConnMaxIdleTime(d)
	}
}

type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func (c PoolConfig) apply(db *sql.DB) {
	if c.MaxOpenConns > 0 {
		db.SetMaxOpenConns(c.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(defaultMaxOpenConns)
	}
	if c.MaxIdleConns > 0 {
		db.SetMaxIdleConns(c.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(defaultMaxIdleConns)
	}
	if c.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(c.ConnMaxLifetime)
	} else {
		db.SetConnMaxLifetime(defaultConnMaxLifetime)
	}
	if c.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(c.ConnMaxIdleTime)
	} else {
		db.SetConnMaxIdleTime(defaultConnMaxIdleTime)
	}
}

func PoolConfigFromValues(maxOpenConns, maxIdleConns int, connMaxLifetime, connMaxIdleTime time.Duration) PoolConfig {
	return PoolConfig{
		MaxOpenConns:    maxOpenConns,
		MaxIdleConns:    maxIdleConns,
		ConnMaxLifetime: connMaxLifetime,
		ConnMaxIdleTime: connMaxIdleTime,
	}
}

// Connect connects to PostgreSQL. Uses a background context for the initial
// connection test since the caller has not yet provided a request context.
func (pdb *PostgresDB) Connect(dsn string) error {
	return pdb.ConnectWithConfig(dsn, PoolConfig{})
}

func (pdb *PostgresDB) ConnectWithConfig(dsn string, poolCfg PoolConfig) error {
	pdb.dsn = dsn

	var lastErr error
	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(defaultRetryBackoff * time.Duration(1<<(attempt-1)))
		}

		db, err := sql.Open("postgres", dsn)
		if err != nil {
			lastErr = fmt.Errorf("failed to open database: %w", err)
			continue
		}

		poolCfg.apply(db)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err != nil {
			_ = db.Close()
			lastErr = fmt.Errorf("failed to ping database: %w", err)
			continue
		}

		pdb.db = db
		return nil
	}

	return fmt.Errorf("database connect failed after %d attempts: %w", defaultMaxRetries+1, lastErr)
}

// Query queries PostgreSQL and returns rows.
// Callers should pass a context with an appropriate timeout; the returned
// *sql.Rows is lazy and requires the context to remain valid during iteration.
func (pdb *PostgresDB) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if pdb.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := pdb.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return rows, nil
}

// QueryRow queries PostgreSQL and returns a single row.
// Callers should pass a context with an appropriate timeout; the returned
// *sql.Row is lazy — the query is not executed until .Scan() is called.
func (pdb *PostgresDB) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if pdb.db == nil {
		return errNotConnectedDB.QueryRowContext(ctx, "SELECT 1")
	}

	return pdb.db.QueryRowContext(ctx, query, args...)
}

// Exec executes a query without returning rows. Derives a 10s timeout from ctx.
func (pdb *PostgresDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if pdb.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := pdb.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("exec failed: %w", err)
	}

	return result, nil
}

// Begin starts a transaction.
func (pdb *PostgresDB) Begin(ctx context.Context) (*sql.Tx, error) {
	if pdb.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	tx, err := pdb.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return tx, nil
}

// InTransaction executes fn inside a database transaction.
// It begins a transaction, calls fn, and commits if fn returns nil.
// The deferred function recovers panics to rollback before re-panicking.
func (pdb *PostgresDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	if pdb.db == nil {
		return fmt.Errorf("database not connected")
	}

	tx, err := pdb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		} else if err != nil {
			_ = tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	err = fn(tx)
	return
}

// Close closes PostgreSQL connection
func (pdb *PostgresDB) Close() error {
	if pdb.db == nil {
		return nil
	}
	return pdb.db.Close()
}

func (pdb *PostgresDB) Reconnect(ctx context.Context) error {
	if pdb.db != nil {
		_ = pdb.db.Close()
		pdb.db = nil
	}
	if pdb.dsn == "" {
		return fmt.Errorf("no DSN configured for reconnect")
	}
	return pdb.Connect(pdb.dsn)
}

// Ping checks if the database is alive
func (pdb *PostgresDB) Ping(ctx context.Context) error {
	if pdb.db == nil {
		return fmt.Errorf("database not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return pdb.db.PingContext(ctx)
}

// Stats returns database statistics
func (pdb *PostgresDB) Stats() sql.DBStats {
	if pdb.db == nil {
		return sql.DBStats{}
	}
	return pdb.db.Stats()
}

// DB returns the underlying *sql.DB for advanced operations. Use with caution.
func (pdb *PostgresDB) DB() *sql.DB {
	return pdb.db
}

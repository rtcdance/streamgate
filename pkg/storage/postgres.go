package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

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

// Connect connects to PostgreSQL. Uses a background context for the initial
// connection test since the caller has not yet provided a request context.
func (pdb *PostgresDB) Connect(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	pdb.db = db
	pdb.dsn = dsn
	return nil
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
		return &sql.Row{}
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

// Begin starts a transaction. Derives a 5s timeout from ctx.
func (pdb *PostgresDB) Begin(ctx context.Context) (*sql.Tx, error) {
	if pdb.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := pdb.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return tx, nil
}

// Close closes PostgreSQL connection
func (pdb *PostgresDB) Close() error {
	if pdb.db == nil {
		return nil
	}
	return pdb.db.Close()
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

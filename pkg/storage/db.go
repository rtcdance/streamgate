package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Database is a generic database interface wrapper
type Database struct {
	postgres *PostgresDB
	dbType   string
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Type string // postgres, mysql, etc.
	DSN  string
}

// NewDatabase creates a new database instance
func NewDatabase(config DatabaseConfig) (*Database, error) {
	db := &Database{
		dbType: config.Type,
	}

	switch config.Type {
	case "postgres", "postgresql":
		postgres := NewPostgresDB()
		if err := postgres.Connect(config.DSN); err != nil {
			return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
		}
		db.postgres = postgres
	default:
		return nil, fmt.Errorf("unsupported database type: %s", config.Type)
	}

	return db, nil
}

// Query executes a query that returns rows
func (db *Database) Query(query string, args ...interface{}) (*sql.Rows, error) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil {
			return nil, fmt.Errorf("database not connected")
		}
		return db.postgres.Query(query, args...)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.dbType)
	}
}

// QueryRow executes a query that returns a single row
func (db *Database) QueryRow(query string, args ...interface{}) *sql.Row {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil {
			return nil
		}
		return db.postgres.QueryRow(query, args...)
	default:
		return nil
	}
}

// Exec executes a query without returning rows
func (db *Database) Exec(query string, args ...interface{}) (sql.Result, error) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil {
			return nil, fmt.Errorf("database not connected")
		}
		return db.postgres.Exec(query, args...)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.dbType)
	}
}

// Begin starts a transaction
func (db *Database) Begin() (*sql.Tx, error) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil {
			return nil, fmt.Errorf("database not connected")
		}
		return db.postgres.Begin()
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.dbType)
	}
}

// BeginTx starts a transaction with options
func (db *Database) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil || db.postgres.db == nil {
			return nil, fmt.Errorf("database not connected")
		}
		return db.postgres.db.BeginTx(ctx, opts)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.dbType)
	}
}

// Close closes the database connection
func (db *Database) Close() error {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil {
			return nil
		}
		return db.postgres.Close()
	default:
		return fmt.Errorf("unsupported database type: %s", db.dbType)
	}
}

// Ping checks if the database is alive
func (db *Database) Ping() error {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil {
			return fmt.Errorf("database not connected")
		}
		return db.postgres.Ping()
	default:
		return fmt.Errorf("unsupported database type: %s", db.dbType)
	}
}

// Stats returns database statistics
func (db *Database) Stats() sql.DBStats {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil {
			return sql.DBStats{}
		}
		return db.postgres.Stats()
	default:
		return sql.DBStats{}
	}
}

// GetType returns the database type
func (db *Database) GetType() string {
	return db.dbType
}

// QueryContext executes a query with context
func (db *Database) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil || db.postgres.db == nil {
			return nil, fmt.Errorf("database not connected")
		}
		return db.postgres.db.QueryContext(ctx, query, args...)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.dbType)
	}
}

// QueryRowContext executes a query that returns a single row with context
func (db *Database) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil || db.postgres.db == nil {
			return nil
		}
		return db.postgres.db.QueryRowContext(ctx, query, args...)
	default:
		return nil
	}
}

// ExecContext executes a query without returning rows with context
func (db *Database) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil || db.postgres.db == nil {
			return nil, fmt.Errorf("database not connected")
		}
		return db.postgres.db.ExecContext(ctx, query, args...)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.dbType)
	}
}

// PrepareContext prepares a statement with context
func (db *Database) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres == nil || db.postgres.db == nil {
			return nil, fmt.Errorf("database not connected")
		}
		return db.postgres.db.PrepareContext(ctx, query)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.dbType)
	}
}

// SetMaxOpenConns sets the maximum number of open connections
func (db *Database) SetMaxOpenConns(n int) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres != nil && db.postgres.db != nil {
			db.postgres.db.SetMaxOpenConns(n)
		}
	}
}

// SetMaxIdleConns sets the maximum number of idle connections
func (db *Database) SetMaxIdleConns(n int) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres != nil && db.postgres.db != nil {
			db.postgres.db.SetMaxIdleConns(n)
		}
	}
}

// SetConnMaxLifetime sets the maximum lifetime of connections
func (db *Database) SetConnMaxLifetime(d time.Duration) {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres != nil && db.postgres.db != nil {
			db.postgres.db.SetConnMaxLifetime(d)
		}
	}
}

// GetDB returns the underlying *sql.DB (use with caution)
func (db *Database) GetDB() *sql.DB {
	switch db.dbType {
	case "postgres", "postgresql":
		if db.postgres != nil {
			return db.postgres.db
		}
	}
	return nil
}

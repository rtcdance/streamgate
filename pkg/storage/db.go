package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rtcdance/streamgate/pkg/models"
)

// DB abstracts SQL database operations.
// Both *PostgresDB and *Database satisfy this interface.
//
//go:generate mockgen -destination=mocks/mock_db.go -package=mocks streamgate/pkg/storage DB
type DB interface {
	Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(ctx context.Context, query string, args ...interface{}) *CancelRow
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Begin(ctx context.Context) (*sql.Tx, error)
	InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error
	Ping(ctx context.Context) error
	Close() error
}

// Database is a generic database wrapper that delegates to a DB implementation.
type Database struct {
	impl   DB
	dbType string
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Type    string
	DSN     string
	PoolCfg PoolConfig
}

// NewDatabase creates a new database instance
func NewDatabase(config DatabaseConfig) (*Database, error) {
	switch config.Type {
	case "postgres", "postgresql":
		postgres := NewPostgresDB()
		if err := postgres.ConnectWithConfig(config.DSN, config.PoolCfg); err != nil {
			return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
		}
		return &Database{impl: postgres, dbType: config.Type}, nil
	default:
		return nil, fmt.Errorf("unsupported database type: %s", config.Type)
	}
}

// NewDatabaseWithImpl creates a Database with a pre-configured DB implementation.
func NewDatabaseWithImpl(impl DB, dbType string) *Database {
	return &Database{impl: impl, dbType: dbType}
}

// Query executes a query that returns rows
func (db *Database) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return db.impl.Query(ctx, query, args...)
}

func (db *Database) QueryRow(ctx context.Context, query string, args ...interface{}) *CancelRow {
	return db.impl.QueryRow(ctx, query, args...)
}

// Exec executes a query without returning rows
func (db *Database) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return db.impl.Exec(ctx, query, args...)
}

// Begin starts a transaction
func (db *Database) Begin(ctx context.Context) (*sql.Tx, error) {
	return db.impl.Begin(ctx)
}

// InTransaction executes fn inside a database transaction.
// It begins a transaction, calls fn, and commits if fn returns nil.
// On error or panic, the transaction is rolled back.
func (db *Database) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return db.impl.InTransaction(ctx, fn)
}

// Close closes the database connection
func (db *Database) Close() error {
	return db.impl.Close()
}

// Ping checks if the database is alive
func (db *Database) Ping(ctx context.Context) error {
	return db.impl.Ping(ctx)
}

// GetType returns the database type
func (db *Database) GetType() string {
	return db.dbType
}

// DBImpl returns the underlying DB implementation for advanced operations.
func (db *Database) DBImpl() DB {
	return db.impl
}

// GetDB returns the underlying *sql.DB if the implementation is PostgresDB.
// Returns nil for other implementations. Use with caution.
func (db *Database) GetDB() *sql.DB {
	if pg, ok := db.impl.(*PostgresDB); ok {
		return pg.DB()
	}
	return nil
}

// GetUser retrieves a user by username
func (db *Database) GetUser(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	var password, email, walletAddr sql.NullString
	err := db.impl.QueryRow(ctx, "SELECT id, username, password, email, wallet_address, created_at, updated_at FROM users WHERE username = $1", username).Scan(
		&user.ID, &user.Username, &password, &email, &walletAddr, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	user.Password = password.String
	user.Email = email.String
	user.WalletAddress = walletAddr.String
	return &user, nil
}

// CreateUser creates a new user
func (db *Database) CreateUser(ctx context.Context, user *models.User) error {
	_, err := db.impl.Exec(ctx,
		"INSERT INTO users (id, username, password, email, wallet_address, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)",
		user.ID, user.Username, user.Password, user.Email, user.WalletAddress, user.CreatedAt, user.UpdatedAt,
	)
	return err
}

// UpdateUser updates an existing user
func (db *Database) UpdateUser(ctx context.Context, user *models.User) error {
	_, err := db.impl.Exec(ctx,
		"UPDATE users SET username = $1, password = $2, email = $3, wallet_address = $4, updated_at = $5 WHERE id = $6",
		user.Username, user.Password, user.Email, user.WalletAddress, user.UpdatedAt, user.ID,
	)
	return err
}

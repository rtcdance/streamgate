package unit_test

import (
	"context"
	"testing"

	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/require"
)

func TestPostgresDB_Connect(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return // Test skipped
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Test that connection is established
	err := db.Ping(context.Background())
	require.NoError(t, err)
}

func TestPostgresDB_Query(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Test query
	rows, err := db.Query(context.Background(), "SELECT 1 as num")
	require.NoError(t, err)
	require.NotNil(t, rows)
	defer func() { _ = rows.Close() }()

	// Verify result
	require.True(t, rows.Next())
	var num int
	err = rows.Scan(&num)
	require.NoError(t, err)
	require.Equal(t, 1, num)
}

func TestPostgresDB_QueryRow(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Test query row
	row := db.QueryRow(context.Background(), "SELECT 42 as answer")
	require.NotNil(t, row)

	var answer int
	err := row.Scan(&answer)
	require.NoError(t, err)
	require.Equal(t, 42, answer)
}

func TestPostgresDB_Exec(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Create test table
	_, err := db.Exec(context.Background(), `
		CREATE TEMP TABLE test_exec (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255)
		)
	`)
	require.NoError(t, err)

	// Insert data
	result, err := db.Exec(context.Background(), "INSERT INTO test_exec (name) VALUES ($1)", "test")
	require.NoError(t, err)

	// Check rows affected
	rowsAffected, err := result.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), rowsAffected)
}

func TestPostgresDB_Transaction(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Begin transaction
	tx, err := db.Begin(context.Background())
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Rollback
	err = tx.Rollback()
	require.NoError(t, err)
}

func TestPostgresDB_Stats(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Get stats
	stats := db.Stats()
	require.True(t, stats.OpenConnections >= 0)
}

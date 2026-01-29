package storage_test

import (
	"testing"

	"streamgate/test/helpers"
)

func TestPostgresDB_Connect(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return // Test skipped
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Test that connection is established
	err := db.Ping()
	helpers.AssertNoError(t, err)
}

func TestPostgresDB_Query(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Test query
	rows, err := db.Query("SELECT 1 as num")
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, rows)
	defer rows.Close()

	// Verify result
	helpers.AssertTrue(t, rows.Next())
	var num int
	err = rows.Scan(&num)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, 1, num)
}

func TestPostgresDB_QueryRow(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Test query row
	row := db.QueryRow("SELECT 42 as answer")
	helpers.AssertNotNil(t, row)

	var answer int
	err := row.Scan(&answer)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, 42, answer)
}

func TestPostgresDB_Exec(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Create test table
	_, err := db.Exec(`
		CREATE TEMP TABLE test_exec (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255)
		)
	`)
	helpers.AssertNoError(t, err)

	// Insert data
	result, err := db.Exec("INSERT INTO test_exec (name) VALUES ($1)", "test")
	helpers.AssertNoError(t, err)

	// Check rows affected
	rowsAffected, err := result.RowsAffected()
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, int64(1), rowsAffected)
}

func TestPostgresDB_Transaction(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Begin transaction
	tx, err := db.Begin()
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, tx)

	// Rollback
	err = tx.Rollback()
	helpers.AssertNoError(t, err)
}

func TestPostgresDB_Stats(t *testing.T) {
	db := helpers.SetupTestPostgres(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestPostgres(t, db)

	// Get stats
	stats := db.Stats()
	helpers.AssertTrue(t, stats.OpenConnections >= 0)
}

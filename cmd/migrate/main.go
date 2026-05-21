// CLI tool for running database migrations.
//
// Usage:
//
//	go run ./cmd/migrate up              # Apply all pending migrations
//	go run ./cmd/migrate down            # Rollback last migration
//	go run ./cmd/migrate down 3          # Rollback last 3 migrations
//	go run ./cmd/migrate status          # Show migration status
package main

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"

	_ "github.com/lib/pq"

	"github.com/rtcdance/streamgate/migrations"
	"github.com/rtcdance/streamgate/pkg/storage/migrate"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: migrate <up|down|status> [steps]")
		os.Exit(1)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			getEnv("DATABASE_HOST", "localhost"),
			getEnv("DATABASE_PORT", "5432"),
			getEnv("DATABASE_USER", "postgres"),
			getEnv("DATABASE_PASSWORD", "postgres"),
			getEnv("DATABASE_NAME", "streamgate"),
			getEnv("DATABASE_SSLMODE", "disable"),
		)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "Database ping failed: %v\n", err)
		os.Exit(1)
	}

	runner := migrate.New(db, migrations.FS)

	switch os.Args[1] {
	case "up":
		if err := runner.Up(); err != nil {
			fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("All pending migrations applied.")

	case "down":
		steps := 1
		if len(os.Args) > 2 {
			steps, _ = strconv.Atoi(os.Args[2])
		}
		if err := runner.Down(steps); err != nil {
			fmt.Fprintf(os.Stderr, "Rollback failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Rolled back %d migration(s).\n", steps)

	case "status":
		fmt.Println("Not implemented yet.")
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

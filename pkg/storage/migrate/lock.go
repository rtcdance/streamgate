package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"hash/crc32"
)

// migrationLockKey is the advisory-lock identity every migration runner in this
// repository shares: pkg/storage/migrate.Runner (monolith, cmd/migrate) and
// pkg/storage.Migrator (api-gateway). Both start against the same database, and
// a per-implementation key leaves them free to interleave CREATE TABLE /
// ALTER TABLE on one schema — the losing process then dies on the pg_type
// unique index and continues with a half-built schema.
const migrationLockKey = "streamgate_migrations"

// MigrationLockID returns the advisory-lock id shared by every migration
// runner. It is exported so an independent implementation can lock the same id.
func MigrationLockID() int64 {
	return int64(crc32.ChecksumIEEE([]byte(migrationLockKey)))
}

// migrationLock is a dedicated connection holding the migration advisory lock.
type migrationLock struct {
	conn *sql.Conn
	id   int64
}

// LockMigrationRun pins one connection and BLOCKS on the shared advisory lock,
// returning the function that releases it. Every migration implementation must
// call this — it is the only thing that keeps two processes starting at the
// same moment from interleaving DDL on one schema.
//
// The dedicated connection is required because PostgreSQL advisory locks are
// session-scoped: a lock taken through the connection pool stays held by
// whichever pooled session served the query, stalling every other runner long
// after this caller finished — a hang that only shows up with two processes.
func LockMigrationRun(ctx context.Context, db *sql.DB) (func(), error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	id := MigrationLockID()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", id); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	l := &migrationLock{conn: conn, id: id}
	return l.release, nil
}

// release unlocks and returns the pinned connection. Unlock runs on the SAME
// session that acquired the lock; that is the only session PostgreSQL will
// release it from.
func (l *migrationLock) release() {
	if l == nil || l.conn == nil {
		return
	}
	_, _ = l.conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", l.id)
	_ = l.conn.Close()
}

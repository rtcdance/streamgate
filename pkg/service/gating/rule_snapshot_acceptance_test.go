package gating

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAcceptance_GatingRuleSnapshotRepeatableRead(t *testing.T) {
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("STREAMGATE_TEST_DB_DSN not set")
	}
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err, "a configured database must be usable")
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, db.Ping(ctx))
	require.NoError(t, helpers.ApplyMigrations(t, db))

	contentID, ruleID := uuid.NewString(), uuid.NewString()
	sqlDB := db.GetDB()
	_, err = sqlDB.ExecContext(ctx, `
		INSERT INTO content_gating_rules
		(id, content_id, contract_address, token_id, chain_id, standard, min_balance, is_active)
		VALUES ($1, $2, '0x0000000000000000000000000000000000000001', '', 31337, 'erc721', 1, TRUE)`,
		ruleID, contentID)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, `INSERT INTO content_rule_versions (content_id, version) VALUES ($1, 1)`, contentID)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, cleanupErr := sqlDB.ExecContext(cleanupCtx, `DELETE FROM content_gating_rules WHERE id = $1`, ruleID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = sqlDB.ExecContext(cleanupCtx, `DELETE FROM content_rule_versions WHERE content_id = $1`, contentID)
		require.NoError(t, cleanupErr)
	})

	writer, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	writerOpen := true
	t.Cleanup(func() {
		if writerOpen {
			_ = writer.Rollback()
		}
	})
	_, err = writer.ExecContext(ctx, `LOCK TABLE content_gating_rules IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)

	type result struct {
		minBalance int
		version    int64
		err        error
	}
	got := make(chan result, 1)
	service := NewGatingRuleService(db, zap.NewNop())
	go func() {
		rules, version, _, snapshotErr := service.GetActiveRulesSnapshot(ctx, contentID)
		value := result{version: version, err: snapshotErr}
		if len(rules) == 1 {
			value.minBalance = rules[0].MinBalance
		}
		got <- value
	}()

	// Wait for the snapshot transaction's second SELECT to block on the rule
	// table. Its first SELECT has already fixed the REPEATABLE READ snapshot and
	// read version 1, so a later commit must not mix version 1 with rule 2.
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		var waiting bool
		err = sqlDB.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE pid <> pg_backend_pid() AND datname = current_database()
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%SELECT id, content_id, contract_address%'
			)`).Scan(&waiting)
		require.NoError(t, err)
		if waiting {
			blocked = true
			break
		}
		select {
		case result := <-got:
			require.NoError(t, result.err, "snapshot unexpectedly completed before the barrier")
			t.Fatalf("snapshot did not wait on the locked policy table: %+v", result)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, blocked, "snapshot query never reached the deterministic database barrier")

	_, err = writer.ExecContext(ctx, `UPDATE content_gating_rules SET min_balance = 2 WHERE id = $1`, ruleID)
	require.NoError(t, err)
	_, err = writer.ExecContext(ctx, `UPDATE content_rule_versions SET version = 2 WHERE content_id = $1`, contentID)
	require.NoError(t, err)
	require.NoError(t, writer.Commit())
	writerOpen = false

	select {
	case snapshot := <-got:
		require.NoError(t, snapshot.err)
		require.Equal(t, int64(1), snapshot.version)
		require.Equal(t, 1, snapshot.minBalance)
	case <-ctx.Done():
		t.Fatal("snapshot did not complete after the policy writer committed")
	}

	var committedBalance int
	var committedVersion int64
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT min_balance FROM content_gating_rules WHERE id = $1`, ruleID).Scan(&committedBalance))
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT version FROM content_rule_versions WHERE content_id = $1`, contentID).Scan(&committedVersion))
	require.Equal(t, 2, committedBalance)
	require.Equal(t, int64(2), committedVersion)
}

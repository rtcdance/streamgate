package transcoding

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestIntegration_DLQDeliveredUpdateFailureAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("STREAMGATE_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("STREAMGATE_TEST_DB_DSN not set")
	}
	db, err := storage.NewDatabase(storage.DatabaseConfig{Type: "postgres", DSN: dsn})
	require.NoError(t, err, "a configured database must be usable")
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, db.Ping(ctx))
	require.NoError(t, helpers.ApplyMigrations(t, db))

	queue := newRecordingQueue()
	svc := NewTranscodingService(db, queue, WithLogger(zap.NewNop()))
	contentID := uuid.NewString()
	_, err = db.Exec(ctx, "INSERT INTO contents (id, title, type) VALUES ($1, 'outbox SQL failure', 'video')", contentID)
	require.NoError(t, err)
	taskID, err := svc.Transcode(ctx, contentID, "720p", "/tmp/input.mp4", 5, "0xOwner")
	require.NoError(t, err)
	task := &models.TranscodingTask{ID: taskID, ContentID: contentID, Profile: "720p", InputURL: "/tmp/input.mp4"}
	payload, err := json.Marshal(task)
	require.NoError(t, err)
	require.NoError(t, svc.terminalFailTx(ctx, taskID, nil, "terminal fixture", payload))

	// Identifiers and the SQL literal below come only from generated UUIDs;
	// no request-controlled value is interpolated into this test-only trigger.
	name := "reject_dlq_mark_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = db.Exec(ctx, "CREATE FUNCTION "+name+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id::text = '"+taskID+"' AND NEW.kind = 'dlq' AND NEW.status = 'delivered' THEN RAISE EXCEPTION 'injected delivered update failure'; END IF; RETURN NEW; END $$")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, dropErr := db.Exec(cleanupCtx, "DROP TRIGGER IF EXISTS "+name+" ON outbox")
		require.NoError(t, dropErr)
		_, dropErr = db.Exec(cleanupCtx, "DROP FUNCTION IF EXISTS "+name+"()")
		require.NoError(t, dropErr)
	})
	_, err = db.Exec(ctx, "CREATE TRIGGER "+name+" BEFORE UPDATE ON outbox FOR EACH ROW EXECUTE FUNCTION "+name+"()")
	require.NoError(t, err)

	deliverTo(svc, queue, task)
	var status, lastError string
	require.NoError(t, db.QueryRow(ctx, "SELECT status, last_error FROM outbox WHERE id = $1 AND kind = 'dlq'", taskID).Scan(&status, &lastError))
	require.Equal(t, "pending", status)
	require.Equal(t, 0, queue.ackCount(taskID), "the real SQL failure must retain the original delivery")
	require.Contains(t, lastError, "injected delivered update failure")
	require.Equal(t, 1, queue.publishCount(models.OutboxKindDLQ, taskID))
	require.Len(t, queue.nakDelays(taskID), 1)

	_, err = db.Exec(ctx, "DROP TRIGGER "+name+" ON outbox")
	require.NoError(t, err)
	deliverTo(svc, queue, task)
	require.NoError(t, db.QueryRow(ctx, "SELECT status FROM outbox WHERE id = $1 AND kind = 'dlq'", taskID).Scan(&status))
	require.Equal(t, "delivered", status)
	require.Equal(t, 1, queue.ackCount(taskID))
	require.Equal(t, 2, queue.publishCount(models.OutboxKindDLQ, taskID))
	deliverTo(svc, queue, task)
	require.Equal(t, 2, queue.ackCount(taskID))
	require.Equal(t, 2, queue.publishCount(models.OutboxKindDLQ, taskID))
}

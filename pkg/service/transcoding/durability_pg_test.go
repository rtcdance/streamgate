package transcoding

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/test/helpers"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestIntegration_TranscodingLeaseAgainstPostgres drives the real claim /
// renew / release / recover / terminal SQL against Postgres so the NOW()-based
// lease semantics are verified by the database engine itself, not only by the
// in-memory fake. It skips ONLY when STREAMGATE_TEST_DB_DSN is absent; a
// configured-but-broken DSN fails the test rather than hiding behind a skip.
func TestIntegration_TranscodingLeaseAgainstPostgres(t *testing.T) {
	if os.Getenv("STREAMGATE_TEST_DB_DSN") == "" {
		t.Skip("STREAMGATE_TEST_DB_DSN not set")
	}
	db := helpers.SetupTestDB(t)
	require.NotNil(t, db)

	queue := newRecordingQueue()
	svc := NewTranscodingService(db, queue,
		WithLogger(zap.NewNop()),
		WithLeaseOwner("it-worker-A"),
	)
	ctx := context.Background()

	contentID := uuid.New().String()
	_, err := db.Exec(ctx,
		"INSERT INTO contents (id, title, type) VALUES ($1, 'durability', 'video')", contentID)
	require.NoError(t, err)

	taskID, err := svc.Transcode(ctx, contentID, "720p", "/tmp/input.mp4", 5, "0xOwner")
	require.NoError(t, err)

	// Task row + enqueue intent committed together; intent delivered only
	// after the publish returned.
	var status, intentStatus string
	require.NoError(t, db.QueryRow(ctx,
		"SELECT status FROM transcoding_tasks WHERE id = $1", taskID).Scan(&status))
	assert.Equal(t, "pending", status)
	require.NoError(t, db.QueryRow(ctx,
		"SELECT status FROM outbox WHERE id = $1 AND kind = $2", taskID, models.OutboxKindTask).Scan(&intentStatus))
	assert.Equal(t, "delivered", intentStatus)

	// Claim takes a DB-time lease and bumps attempt only on success.
	lease, ok, err := svc.claim(ctx, taskID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 1, lease.generation)
	assert.Equal(t, 1, lease.attempt)

	// A second claim while the lease is live must fail and must NOT burn an
	// attempt.
	_, ok, err = svc.claim(ctx, taskID)
	require.NoError(t, err)
	assert.False(t, ok)
	var attempt int
	require.NoError(t, db.QueryRow(ctx,
		"SELECT attempt FROM transcoding_tasks WHERE id = $1", taskID).Scan(&attempt))
	assert.Equal(t, 1, attempt)

	// Renewal succeeds for the holder and fails for anyone else.
	held, err := svc.renewLease(ctx, lease)
	require.NoError(t, err)
	assert.True(t, held)
	held, err = svc.renewLease(ctx, &taskLease{taskID: taskID, owner: "it-worker-B", generation: lease.generation})
	require.NoError(t, err)
	assert.False(t, held)

	// Release preserves last_error for the retry schedule.
	require.NoError(t, svc.releaseForRetry(ctx, lease, "boom", 2*time.Second))
	var lastErr string
	var leaseOwner string
	require.NoError(t, db.QueryRow(ctx,
		"SELECT last_error, lease_owner FROM transcoding_tasks WHERE id = $1", taskID).Scan(&lastErr, &leaseOwner))
	assert.Equal(t, "boom", lastErr)
	assert.Empty(t, leaseOwner)

	// A dead worker's expired lease is recovered on the DB clock with
	// generation and attempt preserved.
	_, err = db.Exec(ctx, `
		UPDATE transcoding_tasks
		SET status = 'processing', lease_owner = 'it-worker-A',
		    lease_expires_at = NOW() - INTERVAL '1 second',
		    generation = 7, attempt = 2
		WHERE id = $1`, taskID)
	require.NoError(t, err)
	svc.recoverStuckTasks(zap.NewNop())
	var gen int
	var recoveredStatus string
	require.NoError(t, db.QueryRow(ctx,
		"SELECT generation, attempt, status, lease_owner FROM transcoding_tasks WHERE id = $1", taskID).
		Scan(&gen, &attempt, &recoveredStatus, &leaseOwner))
	assert.Equal(t, "pending", recoveredStatus)
	assert.Equal(t, 7, gen, "recovery preserves generation")
	assert.Equal(t, 2, attempt, "recovery preserves attempt")
	assert.Empty(t, leaseOwner)

	// Expired-lease reclaim works directly too: process it again, then
	// exhaust the attempt cap so the claim is refused.
	lease, ok, err = svc.claim(ctx, taskID)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, svc.releaseForRetry(ctx, lease, "again", time.Second))
	_, err = db.Exec(ctx, `UPDATE transcoding_tasks SET attempt = 3 WHERE id = $1`, taskID)
	require.NoError(t, err)
	_, ok, err = svc.claim(ctx, taskID)
	require.NoError(t, err)
	assert.False(t, ok, "attempt cap must refuse the claim")

	// Terminal failure writes status=failed and the DLQ intent atomically.
	payload, err := json.Marshal(&models.TranscodingTask{ID: taskID, ContentID: contentID, Profile: "720p"})
	require.NoError(t, err)
	require.NoError(t, svc.terminalFailTx(ctx, taskID, nil, "terminal boom", payload))
	require.NoError(t, db.QueryRow(ctx,
		"SELECT status FROM transcoding_tasks WHERE id = $1", taskID).Scan(&status))
	assert.Equal(t, "failed", status)
	require.NoError(t, db.QueryRow(ctx,
		"SELECT status FROM outbox WHERE id = $1 AND kind = $2", taskID, models.OutboxKindDLQ).Scan(&intentStatus))
	assert.Equal(t, "pending", intentStatus)

	// Settling an already-terminal task fails the lease-guarded write rather
	// than silently overwriting.
	err = svc.terminalFailTx(ctx, taskID, nil, "second terminal", payload)
	require.Error(t, err, "a failed task must not be terminally failed again")

	// Completed task redelivery: Ack only, no re-claim, no side effects.
	_, err = db.Exec(ctx, `UPDATE transcoding_tasks SET status = 'completed' WHERE id = $1`, taskID)
	require.NoError(t, err)
	var attemptNow int
	require.NoError(t, db.QueryRow(ctx, "SELECT attempt FROM transcoding_tasks WHERE id = $1", taskID).Scan(&attemptNow))
	acksBefore := len(queue.acks)
	svc.handleDelivery(ctx, &models.TranscodingTask{ID: taskID, ContentID: contentID, Profile: "720p"}, &testDelivery{q: queue, taskID: taskID}, zap.NewNop())
	assert.Equal(t, acksBefore+1, len(queue.acks), "redelivered completed task must be Acked")
	require.NoError(t, db.QueryRow(ctx, "SELECT attempt FROM transcoding_tasks WHERE id = $1", taskID).Scan(&attempt))
	assert.Equal(t, attemptNow, attempt, "no re-claim for a completed task")

	// pumpOutbox publishes the pending DLQ intent and marks it delivered.
	n, err := svc.pumpOutbox(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1)
	require.NoError(t, db.QueryRow(ctx,
		"SELECT status FROM outbox WHERE id = $1 AND kind = $2", taskID, models.OutboxKindDLQ).Scan(&intentStatus))
	assert.Equal(t, "delivered", intentStatus)
}

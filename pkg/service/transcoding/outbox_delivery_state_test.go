package transcoding

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/stretchr/testify/require"
)

type outboxMarkFailureDB struct {
	storage.DB
	fail atomic.Bool
}

func (db *outboxMarkFailureDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if strings.HasPrefix(query, "UPDATE outbox SET status = 'delivered'") && db.fail.CompareAndSwap(true, false) {
		return nil, errors.New("injected delivered update failure")
	}
	return db.DB.Exec(ctx, query, args...)
}

func TestOutbox_DLQDeliveryMarkFailureRetainsOriginalDelivery(t *testing.T) {
	for _, redelivery := range []bool{false, true} {
		phase := "terminal"
		if redelivery {
			phase = "redelivery"
		}
		t.Run(phase, func(t *testing.T) {
			queue := newRecordingQueue()
			svc, db := newDurabilityFixture(t, queue, &mockVideoTranscoder{transcodeErr: errors.New("transcode failure")})
			taskID, err := svc.Transcode(context.Background(), "content-dlq-mark", "720p", "/tmp/input.mp4", 5, "0xOwner")
			require.NoError(t, err)
			task := &models.TranscodingTask{ID: taskID, ContentID: "content-dlq-mark", Profile: "720p", InputURL: "/tmp/input.mp4"}
			deliverTo(svc, queue, task)
			db.advance(2 * time.Second)
			deliverTo(svc, queue, task)
			db.advance(4 * time.Second)
			if redelivery {
				queue.mu.Lock()
				queue.publishErr[models.OutboxKindDLQ] = errors.New("broker unavailable")
				queue.mu.Unlock()
				deliverTo(svc, queue, task)
				queue.mu.Lock()
				delete(queue.publishErr, models.OutboxKindDLQ)
				queue.mu.Unlock()
			}
			failingDB := &outboxMarkFailureDB{DB: svc.db}
			failingDB.fail.Store(true)
			svc.db = failingDB
			naksBefore := len(queue.nakDelays(taskID))
			deliverTo(svc, queue, task)
			require.False(t, failingDB.fail.Load(), "the delivered write must actually be attempted")
			require.Equal(t, "failed", db.task(taskID).status)
			require.Equal(t, "pending", db.outboxRow(models.OutboxKindDLQ, taskID).status)
			require.Equal(t, 1, queue.publishCount(models.OutboxKindDLQ, taskID), "the broker already accepted the DLQ message")
			require.Equal(t, 0, queue.ackCount(taskID), "a failed durable delivery mark must not Ack the original message")
			require.Len(t, queue.nakDelays(taskID), naksBefore+1)

			deliverTo(svc, queue, task)
			require.Equal(t, "delivered", db.outboxRow(models.OutboxKindDLQ, taskID).status)
			require.Equal(t, 1, queue.ackCount(taskID))
			require.Equal(t, 2, queue.publishCount(models.OutboxKindDLQ, taskID), "recovery reuses the same task ID")
			deliverTo(svc, queue, task)
			require.Equal(t, 2, queue.ackCount(taskID))
			require.Equal(t, 2, queue.publishCount(models.OutboxKindDLQ, taskID), "delivered intent must stay settled")
		})
	}
}

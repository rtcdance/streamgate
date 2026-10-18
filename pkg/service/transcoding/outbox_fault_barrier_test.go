package transcoding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestFaultBarrier_ImmediateOutboxPublish(t *testing.T) {
	for _, point := range []string{"outbox.before_publish", "outbox.after_publish"} {
		t.Run(point, func(t *testing.T) {
			b := durableBarrier(t)
			queue := newRecordingQueue()
			svc, db := newDurabilityFixture(t, queue, &mockVideoTranscoder{})
			svc.SetFaultBarrier(b)
			require.NoError(t, b.Arm(point))
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			var taskID string
			var submitErr error
			go func() {
				defer close(done)
				taskID, submitErr = svc.Transcode(ctx, "content-outbox", "720p", "/tmp/input.mp4", 5, "0xOwner")
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("submit goroutine did not stop")
				}
			})

			require.Eventually(t, b.Point(point).Reached, 2*time.Second, 5*time.Millisecond,
				"the immediate publish must stop at the same point as the background pump")
			select {
			case <-done:
				t.Fatal("submit returned before the armed point was released")
			default:
			}

			// The committed intent must remain pending until PubAck and the
			// post-publish barrier have both completed.
			db.mu.Lock()
			var intentID string
			for _, row := range db.outbox {
				if row.kind == models.OutboxKindTask {
					intentID = row.id
				}
			}
			db.mu.Unlock()
			require.NotEmpty(t, intentID)
			require.NotNil(t, db.task(intentID))
			require.Equal(t, "pending", db.outboxRow(models.OutboxKindTask, intentID).status)
			wantPublishes := 0
			if point == "outbox.after_publish" {
				wantPublishes = 1
			}
			require.Equal(t, wantPublishes, queue.publishCount(models.OutboxKindTask, intentID))
			require.NoError(t, b.Point(point).Release())
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("submit did not resume after the release")
			}
			require.NoError(t, submitErr)
			require.Equal(t, intentID, taskID)
			require.Equal(t, "delivered", db.outboxRow(models.OutboxKindTask, taskID).status)
			require.Equal(t, 1, queue.publishCount(models.OutboxKindTask, taskID))
		})
	}
}

func TestFaultBarrier_DLQOutboxPublish(t *testing.T) {
	for _, redelivery := range []bool{false, true} {
		phase := "terminal"
		if redelivery {
			phase = "redelivery"
		}
		for _, point := range []string{"outbox.before_publish", "outbox.after_publish"} {
			t.Run(phase+"/"+point, func(t *testing.T) {
				b := durableBarrier(t)
				queue := newRecordingQueue()
				svc, db := newDurabilityFixture(t, queue, &mockVideoTranscoder{transcodeErr: errors.New("transcode failure")})
				svc.SetFaultBarrier(b)
				taskID, err := svc.Transcode(context.Background(), "content-dlq-outbox", "720p", "/tmp/input.mp4", 5, "0xOwner")
				require.NoError(t, err)
				task := &models.TranscodingTask{ID: taskID, ContentID: "content-dlq-outbox", Profile: "720p", InputURL: "/tmp/input.mp4"}
				deliverTo(svc, queue, task)
				db.advance(2 * time.Second)
				deliverTo(svc, queue, task)
				db.advance(4 * time.Second)
				if redelivery {
					queue.mu.Lock()
					queue.publishErr[models.OutboxKindDLQ] = errors.New("broker unavailable")
					queue.mu.Unlock()
					deliverTo(svc, queue, task)
					require.Equal(t, "failed", db.task(taskID).status)
					require.Equal(t, "pending", db.outboxRow(models.OutboxKindDLQ, taskID).status)
					queue.mu.Lock()
					delete(queue.publishErr, models.OutboxKindDLQ)
					queue.mu.Unlock()
				}
				require.NoError(t, b.Arm(point))
				done := make(chan struct{})
				go func() {
					defer close(done)
					deliverTo(svc, queue, task)
				}()
				t.Cleanup(func() {
					_ = b.Point(point).Release()
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("DLQ delivery did not stop")
					}
				})
				require.Eventually(t, b.Point(point).Reached, 2*time.Second, 5*time.Millisecond,
					"durable DLQ publication must use the same outbox crash windows")
				require.Equal(t, "failed", db.task(taskID).status)
				require.Equal(t, "pending", db.outboxRow(models.OutboxKindDLQ, taskID).status)
				require.Equal(t, 0, queue.ackCount(taskID), "original delivery cannot be Acked while the DLQ intent is pending")
				wantPublishes := 0
				if point == "outbox.after_publish" {
					wantPublishes = 1
				}
				require.Equal(t, wantPublishes, queue.publishCount(models.OutboxKindDLQ, taskID))
				require.NoError(t, b.Point(point).Release())
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("DLQ delivery did not resume")
				}
				require.Equal(t, "delivered", db.outboxRow(models.OutboxKindDLQ, taskID).status)
				require.Equal(t, 1, queue.ackCount(taskID))
				require.Equal(t, 1, queue.publishCount(models.OutboxKindDLQ, taskID))
				deliverTo(svc, queue, task)
				require.Equal(t, 2, queue.ackCount(taskID))
				require.Equal(t, 1, queue.publishCount(models.OutboxKindDLQ, taskID), "settled redelivery must not republish")
			})
		}
	}
}

func TestFaultBarrier_CanceledOutboxPublishRemainsRecoverable(t *testing.T) {
	for _, point := range []string{"outbox.before_publish", "outbox.after_publish"} {
		t.Run(point, func(t *testing.T) {
			b := durableBarrier(t)
			queue := newRecordingQueue()
			svc, db := newDurabilityFixture(t, queue, &mockVideoTranscoder{})
			svc.SetFaultBarrier(b)
			require.NoError(t, b.Arm(point))
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			var taskID string
			var submitErr error
			go func() {
				defer close(done)
				taskID, submitErr = svc.Transcode(ctx, "content-canceled-outbox", "720p", "/tmp/input.mp4", 5, "0xOwner")
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("submit goroutine did not stop")
				}
			})
			require.Eventually(t, b.Point(point).Reached, 2*time.Second, 5*time.Millisecond)
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation did not interrupt the barrier")
			}
			// Submission was already committed. Cancellation must leave that
			// intent retryable without reporting a fresh, uncommitted task.
			require.NoError(t, submitErr)
			require.NotEmpty(t, taskID)
			require.Equal(t, "pending", db.outboxRow(models.OutboxKindTask, taskID).status)
			wantPublishes := 0
			if point == "outbox.after_publish" {
				wantPublishes = 1
			}
			require.Equal(t, wantPublishes, queue.publishCount(models.OutboxKindTask, taskID))
			require.NoError(t, b.Disarm(point))
			count, err := svc.pumpOutbox(context.Background())
			require.NoError(t, err)
			require.Equal(t, 1, count)
			require.Equal(t, "delivered", db.outboxRow(models.OutboxKindTask, taskID).status)
			require.Equal(t, wantPublishes+1, queue.publishCount(models.OutboxKindTask, taskID),
				"the recovery pump must reuse the committed task's message ID")
		})
	}
}

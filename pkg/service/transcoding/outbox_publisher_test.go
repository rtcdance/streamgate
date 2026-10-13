package transcoding

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestStartOutboxPublisher_SubmitterOnlyProfile pins the lifecycle of the
// standalone outbox pump: the durable profile's app processes submit tasks
// but run no workers, so their own intents must still be published to the
// broker (SG-12 step 5). The pump must never report a running worker, must
// stop with StopWorker, and must not survive a later StartWorker.
func TestStartOutboxPublisher_SubmitterOnlyProfile(t *testing.T) {
	t.Run("no db or queue is a no-op", func(t *testing.T) {
		svc := NewTranscodingService(nil, nil)
		svc.StartOutboxPublisher(zap.NewNop())
		assert.False(t, svc.WorkerRunning())

		done := make(chan struct{})
		go func() { svc.StopWorker(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("StopWorker hung after a no-op publisher start")
		}
	})

	t.Run("pump runs without workers and stops with StopWorker", func(t *testing.T) {
		var calls int64
		db := &mockDB{queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			atomic.AddInt64(&calls, 1)
			return nil, errors.New("outbox unavailable")
		}}
		svc := NewTranscodingService(db, NewMemoryTranscodingQueue())
		svc.outboxInterval = 10 * time.Millisecond

		svc.StartOutboxPublisher(zap.NewNop())
		require.Eventually(t, func() bool { return atomic.LoadInt64(&calls) > 0 },
			time.Second, 10*time.Millisecond, "the standalone outbox pump must run")
		assert.False(t, svc.WorkerRunning(), "publisher-only profile must not report a running worker")

		done := make(chan struct{})
		go func() { svc.StopWorker(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("StopWorker did not stop the standalone outbox pump")
		}
	})

	t.Run("StartWorker supersedes a standalone publisher", func(t *testing.T) {
		db := &mockDB{queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return nil, errors.New("outbox unavailable")
		}}
		svc := NewTranscodingService(db, NewMemoryTranscodingQueue(), WithTranscoder(&mockVideoTranscoder{}))
		svc.outboxInterval = 10 * time.Millisecond

		svc.StartOutboxPublisher(zap.NewNop())
		svc.StartWorker(zap.NewNop())
		assert.True(t, svc.WorkerRunning())

		done := make(chan struct{})
		go func() { svc.StopWorker(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("StopWorker hung after a publisher was superseded by StartWorker")
		}
		assert.False(t, svc.WorkerRunning())
	})
}

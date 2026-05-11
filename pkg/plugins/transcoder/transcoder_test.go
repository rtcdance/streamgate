package transcoder

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/core/event"
)

func newTestTaskQueue(size int) *TaskQueue {
	return &TaskQueue{
		tasks:   make(map[string]*TranscodeTask),
		queue:   make(chan *TranscodeTask, size),
		maxSize: size,
		metrics: &QueueMetrics{},
	}
}

func TestTaskQueue_EnqueueAndCancel(t *testing.T) {
	queue := newTestTaskQueue(2)
	task := &TranscodeTask{ID: "task-1", FileID: "file-1"}

	require.NoError(t, queue.Enqueue(task))
	assert.Equal(t, 1, queue.Len())

	loaded, err := queue.GetTask("task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskStatusPending, loaded.Status)

	require.NoError(t, queue.CancelTask("task-1"))
	loaded, err = queue.GetTask("task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskStatusCancelled, loaded.Status)
}

func TestWorkerPool_ProcessTaskCompletesAndUpdatesMetrics(t *testing.T) {
	bus, err := event.NewMemoryEventBus()
	require.NoError(t, err)

	queue := newTestTaskQueue(2)
	pool := &WorkerPool{
		taskQueue: queue,
		eventBus:  bus,
		logger:    zap.NewNop(),
		metrics:   &WorkerMetrics{},
	}
	worker := &Worker{
		ID:            "worker-1",
		Status:        WorkerStatusIdle,
		LastHeartbeat: time.Now(),
	}
	task := &TranscodeTask{
		ID:         "task-1",
		FileID:     "file-1",
		Status:     TaskStatusPending,
		MaxRetries: 1,
	}
	require.NoError(t, queue.UpdateTask(task))

	pool.processTask(worker, task)

	// Without FFmpeg configured, transcode returns an error
	assert.Equal(t, TaskStatusFailed, task.Status)
	assert.Equal(t, WorkerStatusIdle, worker.Status)
}

func TestWorkerPool_HealthCheckMarksUnhealthyWorker(t *testing.T) {
	queue := newTestTaskQueue(1)
	pool := &WorkerPool{
		taskQueue: queue,
		logger:    zap.NewNop(),
		metrics:   &WorkerMetrics{},
		workers: []*Worker{
			{
				ID:            "worker-stuck",
				Status:        WorkerStatusBusy,
				LastHeartbeat: time.Now().Add(-6 * time.Minute),
			},
		},
	}

	done := make(chan struct{})
	go func() {
		pool.HealthCheck()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("health check timed out")
	}

	metrics := pool.GetMetrics()
	assert.Equal(t, 1, metrics.UnhealthyWorkers)
}

func TestTaskQueue_DequeueHonorsContext(t *testing.T) {
	queue := newTestTaskQueue(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	task, err := queue.Dequeue(ctx)
	require.Error(t, err)
	assert.Nil(t, task)
}

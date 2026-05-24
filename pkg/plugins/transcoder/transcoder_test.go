package transcoder

import (
	"context"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

func TestTaskQueue_Enqueue_Full(t *testing.T) {
	queue := newTestTaskQueue(1)
	require.NoError(t, queue.Enqueue(&TranscodeTask{ID: "task-1"}))

	err := queue.Enqueue(&TranscodeTask{ID: "task-2"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "full")
}

func TestTaskQueue_GetTask_NotFound(t *testing.T) {
	queue := newTestTaskQueue(2)
	_, err := queue.GetTask("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTaskQueue_CancelTask_ProcessingTask(t *testing.T) {
	queue := newTestTaskQueue(2)
	task := &TranscodeTask{ID: "task-1", Status: TaskStatusProcessing}
	queue.tasks["task-1"] = task

	err := queue.CancelTask("task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot cancel processing")
}

func TestTaskQueue_CancelTask_NotFound(t *testing.T) {
	queue := newTestTaskQueue(2)
	err := queue.CancelTask("nonexistent")
	require.Error(t, err)
}

func TestTaskQueue_TransitionStatus(t *testing.T) {
	queue := newTestTaskQueue(2)
	task := &TranscodeTask{ID: "task-1", Status: TaskStatusPending}
	queue.tasks["task-1"] = task

	err := queue.TransitionStatus("task-1", func(t *TranscodeTask) {
		t.Status = TaskStatusProcessing
	})
	require.NoError(t, err)
	assert.Equal(t, TaskStatusProcessing, task.Status)
}

func TestTaskQueue_TransitionStatus_NotFound(t *testing.T) {
	queue := newTestTaskQueue(2)
	err := queue.TransitionStatus("nonexistent", func(t *TranscodeTask) {})
	require.Error(t, err)
}

func TestTaskQueue_Dequeue_Success(t *testing.T) {
	queue := newTestTaskQueue(2)
	require.NoError(t, queue.Enqueue(&TranscodeTask{ID: "task-1"}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	task, err := queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "task-1", task.ID)
}

func TestNewTranscoderPlugin(t *testing.T) {
	config := &TranscoderConfig{
		WorkerPoolSize:      2,
		MaxConcurrentTasks:  4,
		MaxQueueSize:        10,
		TaskTimeout:         30 * time.Minute,
		HealthCheckInterval: 30 * time.Second,
		ScalingPolicy: &ScalingPolicy{
			MinWorkers:         1,
			MaxWorkers:         5,
			ScaleUpThreshold:   2.0,
			ScaleDownThreshold: 0.5,
			CheckInterval:      10 * time.Second,
		},
	}

	plugin := NewTranscoderPlugin(config)
	assert.Equal(t, "transcoder", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
	assert.Equal(t, []string{"storage", "event-bus"}, plugin.Dependencies())
}

func TestTranscoderPlugin_Destroy(t *testing.T) {
	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize: 1,
		MaxQueueSize:   5,
		ScalingPolicy:  &ScalingPolicy{MinWorkers: 1, MaxWorkers: 1},
	})
	assert.NoError(t, plugin.Destroy())
}

func TestTranscoderPlugin_HealthCheck_NoWorkerPool(t *testing.T) {
	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize: 1,
		MaxQueueSize:   5,
		ScalingPolicy:  &ScalingPolicy{MinWorkers: 1, MaxWorkers: 1},
	})
	err := plugin.HealthCheck()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker pool not initialized")
}

func TestWorkerPool_Scale_Up(t *testing.T) {
	bus, err := event.NewMemoryEventBus()
	require.NoError(t, err)

	queue := newTestTaskQueue(10)
	pool := &WorkerPool{
		taskQueue: queue,
		eventBus:  bus,
		logger:    zap.NewNop(),
		metrics:   &WorkerMetrics{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, pool.Start(ctx, 2))
	defer func() { _ = pool.Stop(ctx) }()

	require.NoError(t, pool.Scale(4))
	assert.Equal(t, 4, pool.metrics.TotalWorkers)
}

func TestWorkerPool_Scale_Down(t *testing.T) {
	bus, err := event.NewMemoryEventBus()
	require.NoError(t, err)

	queue := newTestTaskQueue(10)
	pool := &WorkerPool{
		taskQueue: queue,
		eventBus:  bus,
		logger:    zap.NewNop(),
		metrics:   &WorkerMetrics{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, pool.Start(ctx, 4))
	defer func() { _ = pool.Stop(ctx) }()

	require.NoError(t, pool.Scale(2))
	assert.Equal(t, 2, pool.metrics.TotalWorkers)
}

func TestWorkerPool_GetMetrics(t *testing.T) {
	queue := newTestTaskQueue(10)
	pool := &WorkerPool{
		taskQueue: queue,
		logger:    zap.NewNop(),
		metrics:   &WorkerMetrics{},
		workers: []*Worker{
			{ID: "w1", Status: WorkerStatusIdle},
			{ID: "w2", Status: WorkerStatusBusy},
		},
	}

	metrics := pool.GetMetrics()
	assert.Equal(t, 2, metrics.TotalWorkers)
	assert.Equal(t, 1, metrics.IdleWorkers)
	assert.Equal(t, 1, metrics.ActiveWorkers)
}

func TestNewFFmpegTranscoder_Defaults(t *testing.T) {
	config := &FFmpegConfig{}
	ft := NewFFmpegTranscoder(config, zap.NewNop())

	assert.Equal(t, "ffmpeg", ft.config.FFmpegPath)
	assert.Equal(t, "ffprobe", ft.config.FFprobePath)
	assert.Equal(t, "/tmp/streamgate", ft.config.TempDir)
}

func TestParseBitrate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"k suffix", "5000k", 5000},
		{"kbps suffix", "5000kbps", 5000},
		{"m suffix", "5m", 5000},
		{"mbps suffix", "5mbps", 5000},
		{"plain number", "5000", 5000},
		{"invalid", "invalid", 0},
		{"empty", "", 0},
		{"with spaces", " 2500k ", 2500},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseBitrate(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{"standard", "01:02:03.500", 1*time.Hour + 2*time.Minute + 3*time.Second + 500*time.Millisecond},
		{"zero", "00:00:00.000", 0},
		{"invalid parts", "01:02", 0},
		{"invalid hours", "ab:02:03.500", 0},
		{"invalid minutes", "01:ab:03.500", 0},
		{"invalid seconds", "01:02:ab", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseTime(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFFmpegTranscoder_CleanupTempFiles(t *testing.T) {
	config := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(config, zap.NewNop())

	err := ft.CleanupTempFiles()
	assert.NoError(t, err)
}

func TestFFmpegTranscoder_CleanupTempFiles_NonExistent(t *testing.T) {
	config := &FFmpegConfig{TempDir: "/tmp/nonexistent-dir-for-test-12345"}
	ft := NewFFmpegTranscoder(config, zap.NewNop())

	err := ft.CleanupTempFiles()
	assert.NoError(t, err)
}

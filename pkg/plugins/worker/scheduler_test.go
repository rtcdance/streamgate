package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPriorityQueue_HighPriorityFirst(t *testing.T) {
	queue := NewPriorityQueue(4)
	now := time.Now()

	low := &Job{ID: "low", Priority: JobPriorityLow, CreatedAt: now}
	high := &Job{ID: "high", Priority: JobPriorityHigh, CreatedAt: now.Add(time.Millisecond)}
	medium := &Job{ID: "medium", Priority: JobPriorityMedium, CreatedAt: now.Add(2 * time.Millisecond)}

	require.NoError(t, queue.Enqueue(low))
	require.NoError(t, queue.Enqueue(high))
	require.NoError(t, queue.Enqueue(medium))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	first, err := queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "high", first.ID)

	second, err := queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "medium", second.ID)

	third, err := queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "low", third.ID)
}

func TestScheduler_SubmitJobCompletes(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:    1,
		QueueSize:     8,
		JobTimeout:    time.Second,
		MaxRetries:    1,
		EnableMetrics: true,
	}, zap.NewNop())
	t.Cleanup(func() {
		_ = scheduler.Stop()
	})

	scheduler.RegisterExecutor("transcode", NewFuncExecutor("transcode", func(ctx context.Context, job *Job) (interface{}, error) {
		return map[string]string{"status": "ok"}, nil
	}))

	require.NoError(t, scheduler.Start())

	job := NewJob("transcode", map[string]interface{}{"file_id": "file-1"})
	job.Priority = JobPriorityHigh
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusCompleted
	}, 2*time.Second, 20*time.Millisecond)

	loaded, err := scheduler.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, JobStatusCompleted, loaded.Status)
	assert.Equal(t, float64(100), loaded.Progress)
}

func TestScheduler_RetryThenComplete(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:   1,
		QueueSize:    8,
		JobTimeout:   time.Second,
		MaxRetries:   2,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() {
		_ = scheduler.Stop()
	})

	attempts := 0
	scheduler.RegisterExecutor("transcode", NewFuncExecutor("transcode", func(ctx context.Context, job *Job) (interface{}, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("temporary ffmpeg failure")
		}
		return "ok", nil
	}))

	require.NoError(t, scheduler.Start())

	job := NewJob("transcode", map[string]interface{}{"file_id": "file-2"})
	job.MaxRetries = 2
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusCompleted
	}, 3*time.Second, 20*time.Millisecond)

	loaded, err := scheduler.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, JobStatusCompleted, loaded.Status)
	assert.Equal(t, 1, loaded.RetryCount)
	assert.Equal(t, 2, attempts)
}

func TestScheduler_CancelQueuedJob(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:   1,
		QueueSize:    8,
		JobTimeout:   time.Second,
		MaxRetries:   1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() {
		_ = scheduler.Stop()
	})

	blocker := make(chan struct{})
	scheduler.RegisterExecutor("blocking", NewFuncExecutor("blocking", func(ctx context.Context, job *Job) (interface{}, error) {
		select {
		case <-blocker:
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))

	require.NoError(t, scheduler.Start())

	first := NewJob("blocking", map[string]interface{}{"file_id": "file-a"})
	first.Priority = JobPriorityHigh
	require.NoError(t, scheduler.SubmitJob(first))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(first.ID)
		return err == nil && loaded.Status == JobStatusRunning
	}, 2*time.Second, 20*time.Millisecond)

	second := NewJob("blocking", map[string]interface{}{"file_id": "file-b"})
	second.Priority = JobPriorityLow
	require.NoError(t, scheduler.SubmitJob(second))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(second.ID)
		return err == nil && loaded.Status == JobStatusQueued
	}, 2*time.Second, 20*time.Millisecond)

	require.NoError(t, scheduler.CancelJob(second.ID))

	loaded, err := scheduler.GetJob(second.ID)
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, loaded.Status)

	close(blocker)
}

func TestScheduler_ListJobsFilterAndPagination(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() {
		_ = scheduler.Stop()
	})

	blocker := make(chan struct{})
	scheduler.RegisterExecutor("blocking", NewFuncExecutor("blocking", func(ctx context.Context, job *Job) (interface{}, error) {
		select {
		case <-blocker:
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))

	require.NoError(t, scheduler.Start())

	jobA := NewJob("blocking", map[string]interface{}{"file_id": "a"})
	jobA.Priority = JobPriorityHigh
	require.NoError(t, scheduler.SubmitJob(jobA))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(jobA.ID)
		return err == nil && loaded.Status == JobStatusRunning
	}, 2*time.Second, 20*time.Millisecond)

	jobB := NewJob("blocking", map[string]interface{}{"file_id": "b"})
	jobB.Priority = JobPriorityMedium
	require.NoError(t, scheduler.SubmitJob(jobB))

	jobC := NewJob("blocking", map[string]interface{}{"file_id": "c"})
	jobC.Priority = JobPriorityLow
	require.NoError(t, scheduler.SubmitJob(jobC))

	queued, err := scheduler.ListJobs(JobStatusQueued, 10, 0)
	require.NoError(t, err)
	require.Len(t, queued, 2)

	page, err := scheduler.ListJobs("", 1, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)

	close(blocker)
}

func TestScheduler_FailWithoutRetryMarksFailed(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() {
		_ = scheduler.Stop()
	})

	scheduler.RegisterExecutor("always_fail", NewFuncExecutor("always_fail", func(ctx context.Context, job *Job) (interface{}, error) {
		return nil, errors.New("permanent failure")
	}))

	require.NoError(t, scheduler.Start())

	job := NewJob("always_fail", map[string]interface{}{"file_id": "failed-job"})
	job.MaxRetries = 1
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusFailed
	}, 2*time.Second, 20*time.Millisecond)

	loaded, err := scheduler.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, JobStatusFailed, loaded.Status)
	assert.Equal(t, 1, loaded.RetryCount)
	assert.Contains(t, loaded.Error, "permanent failure")
}

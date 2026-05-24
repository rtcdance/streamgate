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

func TestNewScheduler_DefaultConfig(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	assert.Equal(t, 10, scheduler.config.MaxWorkers)
	assert.Equal(t, 1000, scheduler.config.QueueSize)
	assert.Equal(t, 30*time.Minute, scheduler.config.JobTimeout)
}

func TestScheduler_SubmitJob_DefaultValues(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("test", NewFuncExecutor("test", func(ctx context.Context, job *Job) (interface{}, error) {
		return "ok", nil
	}))
	require.NoError(t, scheduler.Start())

	job := &Job{Type: "test"}
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusCompleted
	}, 2*time.Second, 20*time.Millisecond)

	loaded, err := scheduler.GetJob(job.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, loaded.ID)
	assert.Equal(t, JobPriorityMedium, loaded.Priority)
	assert.False(t, loaded.CreatedAt.IsZero())
}

func TestScheduler_SubmitJob_DuplicateID(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("test", NewFuncExecutor("test", func(ctx context.Context, job *Job) (interface{}, error) {
		return "ok", nil
	}))
	require.NoError(t, scheduler.Start())

	job1 := &Job{ID: "dup-id", Type: "test"}
	require.NoError(t, scheduler.SubmitJob(job1))

	job2 := &Job{ID: "dup-id", Type: "test"}
	err := scheduler.SubmitJob(job2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestScheduler_GetJob_NotFound(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	_, err := scheduler.GetJob("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestScheduler_CancelJob_NotFound(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	err := scheduler.CancelJob("nonexistent")
	require.Error(t, err)
}

func TestScheduler_CancelJob_AlreadyCompleted(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("test", NewFuncExecutor("test", func(ctx context.Context, job *Job) (interface{}, error) {
		return "ok", nil
	}))
	require.NoError(t, scheduler.Start())

	job := NewJob("test", nil)
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusCompleted
	}, 2*time.Second, 20*time.Millisecond)

	err := scheduler.CancelJob(job.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot cancel")
}

func TestScheduler_RetryJob_NotFailed(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("test", NewFuncExecutor("test", func(ctx context.Context, job *Job) (interface{}, error) {
		return "ok", nil
	}))
	require.NoError(t, scheduler.Start())

	job := NewJob("test", nil)
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusCompleted
	}, 2*time.Second, 20*time.Millisecond)

	err := scheduler.RetryJob(job.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can only retry failed")
}

func TestScheduler_RetryJob_MaxRetriesExceeded(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("always_fail", NewFuncExecutor("always_fail", func(ctx context.Context, job *Job) (interface{}, error) {
		return nil, errors.New("permanent failure")
	}))
	require.NoError(t, scheduler.Start())

	job := NewJob("always_fail", nil)
	job.MaxRetries = 1
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusFailed
	}, 2*time.Second, 20*time.Millisecond)

	err := scheduler.RetryJob(job.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum retry")
}

func TestScheduler_GetStats(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("test", NewFuncExecutor("test", func(ctx context.Context, job *Job) (interface{}, error) {
		return "ok", nil
	}))
	require.NoError(t, scheduler.Start())

	job := NewJob("test", nil)
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusCompleted
	}, 2*time.Second, 20*time.Millisecond)

	stats := scheduler.GetStats()
	assert.Equal(t, int64(1), stats.TotalJobs)
	assert.Equal(t, int64(1), stats.CompletedJobs)
}

func TestScheduler_ListJobs_EmptyStatus(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("test", NewFuncExecutor("test", func(ctx context.Context, job *Job) (interface{}, error) {
		return "ok", nil
	}))
	require.NoError(t, scheduler.Start())

	job := NewJob("test", nil)
	require.NoError(t, scheduler.SubmitJob(job))

	all, err := scheduler.ListJobs("", 10, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 1)
}

func TestScheduler_ListJobs_OffsetBeyondRange(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	jobs, err := scheduler.ListJobs("", 10, 100)
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestScheduler_RegisterExecutor(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())

	executor := NewFuncExecutor("transcode", func(ctx context.Context, job *Job) (interface{}, error) {
		return "ok", nil
	})
	scheduler.RegisterExecutor("transcode", executor)

	assert.Contains(t, scheduler.executors, "transcode")
}

func TestScheduler_NoExecutor(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	require.NoError(t, scheduler.Start())

	job := NewJob("unknown_type", nil)
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusFailed
	}, 2*time.Second, 20*time.Millisecond)

	loaded, err := scheduler.GetJob(job.ID)
	require.NoError(t, err)
	assert.Contains(t, loaded.Error, "no executor found")
}

func TestNewJob(t *testing.T) {
	job := NewJob("transcode", map[string]interface{}{"file_id": "f1"})
	assert.Equal(t, "transcode", job.Type)
	assert.Equal(t, JobStatusPending, job.Status)
	assert.NotNil(t, job.Payload)
}

func TestNewWorker(t *testing.T) {
	worker := NewWorker("worker-1", zap.NewNop())
	assert.Equal(t, "worker-1", worker.ID)
	assert.Equal(t, WorkerStatusIdle, worker.Status)
	assert.False(t, worker.LastHeartbeat.IsZero())
}

func TestWorker_RecordJob(t *testing.T) {
	worker := NewWorker("worker-1", zap.NewNop())

	worker.RecordJob(100*time.Millisecond, true)
	worker.RecordJob(200*time.Millisecond, false)

	assert.Equal(t, int64(1), worker.CompletedJobs)
	assert.Equal(t, int64(1), worker.FailedJobs)
	assert.Equal(t, 300*time.Millisecond, worker.TotalProcessing)
}

func TestWorker_GetStats(t *testing.T) {
	worker := NewWorker("worker-1", zap.NewNop())
	worker.RecordJob(50*time.Millisecond, true)

	stats := worker.GetStats()
	assert.Equal(t, "worker-1", stats["id"])
	assert.Equal(t, int64(1), stats["completed_jobs"])
}

func TestFuncExecutor_Execute(t *testing.T) {
	executor := NewFuncExecutor("test", func(ctx context.Context, job *Job) (interface{}, error) {
		return "result", nil
	})

	result, err := executor.Execute(context.Background(), &Job{Type: "test"})
	require.NoError(t, err)
	assert.Equal(t, "result", result)
}

func TestFuncExecutor_CanExecute(t *testing.T) {
	executor := NewFuncExecutor("transcode", func(ctx context.Context, job *Job) (interface{}, error) {
		return nil, nil
	})

	assert.True(t, executor.CanExecute("transcode"))
	assert.False(t, executor.CanExecute("upload"))
}

func TestFuncExecutor_CanExecute_Wildcard(t *testing.T) {
	executor := NewFuncExecutor("*", func(ctx context.Context, job *Job) (interface{}, error) {
		return nil, nil
	})

	assert.True(t, executor.CanExecute("anything"))
}

func TestMultiExecutor(t *testing.T) {
	me := NewMultiExecutor()
	me.Register("type_a", NewFuncExecutor("type_a", func(ctx context.Context, job *Job) (interface{}, error) {
		return "a_result", nil
	}))
	me.Register("type_b", NewFuncExecutor("type_b", func(ctx context.Context, job *Job) (interface{}, error) {
		return "b_result", nil
	}))

	assert.True(t, me.CanExecute("type_a"))
	assert.True(t, me.CanExecute("type_b"))
	assert.False(t, me.CanExecute("type_c"))

	result, err := me.Execute(context.Background(), &Job{Type: "type_a"})
	require.NoError(t, err)
	assert.Equal(t, "a_result", result)
}

func TestMultiExecutor_NoExecutor(t *testing.T) {
	me := NewMultiExecutor()
	_, err := me.Execute(context.Background(), &Job{Type: "missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no executor found")
}

func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()
	assert.Equal(t, 3, policy.MaxRetries)
	assert.Equal(t, 1*time.Second, policy.InitialDelay)
	assert.Equal(t, 30*time.Second, policy.MaxDelay)
	assert.Equal(t, 2.0, policy.BackoffFactor)
}

func TestRetryPolicy_ShouldRetry(t *testing.T) {
	policy := DefaultRetryPolicy()

	assert.True(t, policy.ShouldRetry(errors.New("timeout"), 0))
	assert.True(t, policy.ShouldRetry(errors.New("connection error"), 0))
	assert.False(t, policy.ShouldRetry(nil, 0))
	assert.False(t, policy.ShouldRetry(errors.New("permanent error"), 2))
}

func TestRetryPolicy_GetDelay(t *testing.T) {
	policy := DefaultRetryPolicy()

	delay0 := policy.GetDelay(0)
	assert.Equal(t, 1*time.Second, delay0)

	delay1 := policy.GetDelay(1)
	assert.Equal(t, 2*time.Second, delay1)

	delay2 := policy.GetDelay(2)
	assert.Equal(t, 4*time.Second, delay2)
}

func TestRetryPolicy_GetDelay_MaxDelay(t *testing.T) {
	policy := &RetryPolicy{
		InitialDelay:  1 * time.Second,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 10.0,
	}

	delay := policy.GetDelay(10)
	assert.Equal(t, 5*time.Second, delay)
}

func TestPriorityQueue_Peek(t *testing.T) {
	queue := NewPriorityQueue(4)

	_, ok := queue.Peek()
	assert.False(t, ok)

	require.NoError(t, queue.Enqueue(&Job{ID: "job1", Priority: JobPriorityHigh, CreatedAt: time.Now()}))

	job, ok := queue.Peek()
	assert.True(t, ok)
	assert.Equal(t, "job1", job.ID)
	assert.Equal(t, 1, queue.Len())
}

func TestPriorityQueue_Remove(t *testing.T) {
	queue := NewPriorityQueue(4)

	require.NoError(t, queue.Enqueue(&Job{ID: "job1", Priority: JobPriorityHigh, CreatedAt: time.Now()}))
	require.NoError(t, queue.Enqueue(&Job{ID: "job2", Priority: JobPriorityLow, CreatedAt: time.Now()}))

	removed := queue.Remove("job1")
	assert.True(t, removed)
	assert.Equal(t, 1, queue.Len())

	removed = queue.Remove("nonexistent")
	assert.False(t, removed)
}

func TestPriorityQueue_Clear(t *testing.T) {
	queue := NewPriorityQueue(4)

	require.NoError(t, queue.Enqueue(&Job{ID: "job1", Priority: JobPriorityHigh, CreatedAt: time.Now()}))
	require.NoError(t, queue.Enqueue(&Job{ID: "job2", Priority: JobPriorityLow, CreatedAt: time.Now()}))

	queue.Clear()
	assert.Equal(t, 0, queue.Len())
}

func TestNewJobContext(t *testing.T) {
	job := NewJob("test", nil)
	jc := NewJobContext(job, zap.NewNop())

	assert.Equal(t, job, jc.Job)
	assert.NotNil(t, jc.Metadata)
}

func TestJobContext_WithMetadata(t *testing.T) {
	jc := NewJobContext(NewJob("test", nil), zap.NewNop())
	result := jc.WithMetadata("key1", "value1")

	assert.Equal(t, jc, result)
	assert.Equal(t, "value1", jc.Metadata["key1"])
}

func TestJobContext_GetMetadata(t *testing.T) {
	jc := NewJobContext(NewJob("test", nil), zap.NewNop())
	jc.WithMetadata("key1", "value1")

	val, ok := jc.GetMetadata("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)

	_, ok = jc.GetMetadata("nonexistent")
	assert.False(t, ok)
}

func TestContains(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		substr  string
		want    bool
	}{
		{"exact match", "timeout", "timeout", true},
		{"prefix", "timeout error", "timeout", true},
		{"suffix", "connection timeout", "timeout", true},
		{"middle", "a timeout occurred", "timeout", true},
		{"not found", "permanent error", "timeout", false},
		{"empty", "", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := contains(tc.s, tc.substr)
			assert.Equal(t, tc.want, result)
		})
	}
}

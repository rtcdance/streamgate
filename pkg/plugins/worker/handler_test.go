package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestWorkerHandler(t *testing.T) *WorkerHandler {
	t.Helper()
	kernel, err := core.NewMicrokernel(&config.Config{Mode: "monolith"}, zap.NewNop())
	require.NoError(t, err)
	scheduler := NewJobScheduler(zap.NewNop())
	return NewWorkerHandler(scheduler, zap.NewNop(), kernel)
}

func TestWorkerHandler_HealthHandler_Healthy(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.HealthHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWorkerHandler_ReadyHandler(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ReadyHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWorkerHandler_SubmitJobHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/submit", http.NoBody)
	rec := httptest.NewRecorder()

	handler.SubmitJobHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestWorkerHandler_SubmitJobHandler_InvalidBody(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader([]byte("bad")))
	rec := httptest.NewRecorder()

	handler.SubmitJobHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWorkerHandler_SubmitJobHandler_SchedulerNotRunning(t *testing.T) {
	handler := newTestWorkerHandler(t)

	body, _ := json.Marshal(Job{Type: "test"})
	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.SubmitJobHandler(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestWorkerHandler_SubmitJobHandler_Success(t *testing.T) {
	handler := newTestWorkerHandler(t)

	handler.scheduler.mu.Lock()
	handler.scheduler.running = true
	handler.scheduler.mu.Unlock()

	body, _ := json.Marshal(map[string]string{"type": "test"})
	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.SubmitJobHandler(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	handler.scheduler.mu.Lock()
	handler.scheduler.running = false
	handler.scheduler.mu.Unlock()
}

func TestWorkerHandler_GetJobStatusHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/status", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetJobStatusHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestWorkerHandler_GetJobStatusHandler_MissingID(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/status", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetJobStatusHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWorkerHandler_GetJobStatusHandler_NotFound(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/status?job_id=nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetJobStatusHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWorkerHandler_CancelJobHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/cancel", http.NoBody)
	rec := httptest.NewRecorder()

	handler.CancelJobHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestWorkerHandler_CancelJobHandler_MissingID(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/cancel", http.NoBody)
	rec := httptest.NewRecorder()

	handler.CancelJobHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWorkerHandler_CancelJobHandler_NotFound(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/cancel?job_id=nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.CancelJobHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWorkerHandler_ListJobsHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/list", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ListJobsHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestWorkerHandler_ListJobsHandler_Success(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/list", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ListJobsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWorkerHandler_ScheduleJobHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/schedule", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ScheduleJobHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestWorkerHandler_ScheduleJobHandler_InvalidBody(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader([]byte("bad")))
	rec := httptest.NewRecorder()

	handler.ScheduleJobHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWorkerHandler_ScheduleJobHandler_SchedulerNotRunning(t *testing.T) {
	handler := newTestWorkerHandler(t)

	body, _ := json.Marshal(ScheduledJob{
		ID:       "sched-1",
		JobType:  "test",
		Schedule: "*/5 * * * *",
		Enabled:  true,
	})
	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ScheduleJobHandler(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestWorkerHandler_NotFoundHandler(t *testing.T) {
	handler := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.NotFoundHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNewJobScheduler(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())
	assert.NotNil(t, scheduler)
	assert.False(t, scheduler.running)
}

func TestJobScheduler_StartAndStop(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.Start(ctx)
	assert.True(t, scheduler.running)

	scheduler.Stop()
	assert.False(t, scheduler.running)
}

func TestJobScheduler_StartIdempotent(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx)
	scheduler.Start(ctx)

	scheduler.Stop()
}

func TestJobScheduler_StopNotRunning(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())
	scheduler.Stop()
}

func TestJobScheduler_SubmitJob_NotRunning(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())

	err := scheduler.SubmitJob(&Job{ID: "job-1", Type: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestJobScheduler_SubmitAndGetJob(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx)

	err := scheduler.SubmitJob(&Job{ID: "job-1", Type: "test"})
	require.NoError(t, err)

	job, err := scheduler.GetJob("job-1")
	require.NoError(t, err)
	assert.Equal(t, "job-1", job.ID)
}

func TestJobScheduler_GetJob_NotFound(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())

	_, err := scheduler.GetJob("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestJobScheduler_CancelJob(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx)

	err := scheduler.SubmitJob(&Job{ID: "job-1", Type: "test"})
	require.NoError(t, err)

	err = scheduler.CancelJob("job-1")
	require.NoError(t, err)

	job, err := scheduler.GetJob("job-1")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, job.Status)
}

func TestJobScheduler_CancelJob_NotFound(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())

	err := scheduler.CancelJob("nonexistent")
	require.Error(t, err)
}

func TestJobScheduler_ListJobs(t *testing.T) {
	scheduler := NewJobScheduler(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx)

	err := scheduler.SubmitJob(&Job{ID: "job-1", Type: "test"})
	require.NoError(t, err)
	err = scheduler.SubmitJob(&Job{ID: "job-2", Type: "test"})
	require.NoError(t, err)

	jobs := scheduler.ListJobs()
	assert.Len(t, jobs, 2)
}

func TestNewWorkerServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewWorkerServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, server)
	assert.NotNil(t, server.scheduler)
}

func TestWorkerServer_Health_NotStarted(t *testing.T) {
	server := &WorkerServer{logger: zap.NewNop()}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestWorkerServer_Health_NoScheduler(t *testing.T) {
	server := &WorkerServer{
		logger: zap.NewNop(),
		server: &http.Server{},
	}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestWorkerPlugin_NameVersion(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewWorkerPlugin(cfg, zap.NewNop())

	assert.Equal(t, "worker", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
}

func TestWorkerPlugin_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewWorkerPlugin(cfg, zap.NewNop())

	err := plugin.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestRetryPolicy_ShouldRetry_MaxRetries(t *testing.T) {
	policy := DefaultRetryPolicy()

	assert.False(t, policy.ShouldRetry(assert.AnError, 3))
}

func TestRetryPolicy_ShouldRetry_NilError(t *testing.T) {
	policy := DefaultRetryPolicy()

	assert.False(t, policy.ShouldRetry(nil, 0))
}

func TestRetryPolicy_GetDelay_Sequence(t *testing.T) {
	policy := DefaultRetryPolicy()

	assert.Equal(t, 1*time.Second, policy.GetDelay(0))
	assert.Equal(t, 2*time.Second, policy.GetDelay(1))
	assert.Equal(t, 4*time.Second, policy.GetDelay(2))
}

func TestWorkerPlugin_Init(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	plugin := NewWorkerPlugin(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, plugin.server)
}

func TestWorkerPlugin_DependsOn(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewWorkerPlugin(cfg, zap.NewNop())

	deps := plugin.DependsOn()
	assert.Nil(t, deps)
}

func TestWorkerPlugin_StartAndStop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	plugin := NewWorkerPlugin(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = plugin.Start(ctx)
	require.NoError(t, err)

	err = plugin.Health(context.Background())
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	err = plugin.Stop(stopCtx)
	require.NoError(t, err)
}

func TestWorkerServer_StartAndStop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewWorkerServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/list", http.NoBody)
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	require.NoError(t, server.Stop(stopCtx))
}

func TestWorkerServer_Health_WithServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewWorkerServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))

	err = server.Health(context.Background())
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}

func TestWorkerHandler_CancelJobHandler_Success(t *testing.T) {
	handler := newTestWorkerHandler(t)

	job := &Job{Type: "test"}
	job.ID = "job-cancel-test"
	handler.scheduler.mu.Lock()
	handler.scheduler.running = true
	handler.scheduler.jobs[job.ID] = job
	handler.scheduler.mu.Unlock()

	cancelReq := httptest.NewRequest(http.MethodPost, "/cancel?job_id=job-cancel-test", http.NoBody)
	cancelRec := httptest.NewRecorder()
	handler.CancelJobHandler(cancelRec, cancelReq)
	assert.Equal(t, http.StatusOK, cancelRec.Code)
}

func TestWorkerHandler_ScheduleJobHandler_Success(t *testing.T) {
	handler := newTestWorkerHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler.scheduler.Start(ctx)

	body, _ := json.Marshal(ScheduledJob{
		ID:       "sched-1",
		JobType:  "test",
		Schedule: "*/5 * * * *",
		Enabled:  true,
	})
	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ScheduleJobHandler(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestWorkerHandler_GetJobStatusHandler_Found(t *testing.T) {
	handler := newTestWorkerHandler(t)

	job := &Job{Type: "test"}
	job.ID = "job-status-test"
	handler.scheduler.mu.Lock()
	handler.scheduler.running = true
	handler.scheduler.jobs[job.ID] = job
	handler.scheduler.mu.Unlock()

	statusReq := httptest.NewRequest(http.MethodGet, "/status?job_id=job-status-test", http.NoBody)
	statusRec := httptest.NewRecorder()
	handler.GetJobStatusHandler(statusRec, statusReq)
	assert.Equal(t, http.StatusOK, statusRec.Code)
}

func TestScheduler_ScheduleJob_Immediate(t *testing.T) {
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
	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, scheduler.ScheduleJob(job, past))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status != JobStatusPending
	}, 5*time.Second, 20*time.Millisecond)
}

func TestScheduler_ScheduleJob_Future(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      2,
		QueueSize:       8,
		JobTimeout:      10 * time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("test", NewFuncExecutor("test", func(ctx context.Context, job *Job) (interface{}, error) {
		return "ok", nil
	}))
	require.NoError(t, scheduler.Start())

	job := NewJob("test", nil)
	future := time.Now().Add(100 * time.Millisecond)
	require.NoError(t, scheduler.ScheduleJob(job, future))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		if err != nil {
			return false
		}
		return loaded.Status != JobStatusPending
	}, 10*time.Second, 50*time.Millisecond)
}

func TestScheduler_RetryJob_Success(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("always_fail", NewFuncExecutor("always_fail", func(ctx context.Context, job *Job) (interface{}, error) {
		return nil, errors.New("fail")
	}))
	require.NoError(t, scheduler.Start())

	job := NewJob("always_fail", nil)
	job.MaxRetries = 1
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusFailed
	}, 3*time.Second, 20*time.Millisecond)

	scheduler.mu.Lock()
	storedJob := scheduler.jobs[job.ID]
	storedJob.RetryCount = 0
	storedJob.MaxRetries = 10
	scheduler.mu.Unlock()

	err := scheduler.RetryJob(job.ID)
	require.NoError(t, err)

	loaded, err := scheduler.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, JobStatusQueued, loaded.Status)
}

func TestScheduler_GetScheduledJobs(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	future := time.Now().Add(1 * time.Hour)
	job := NewJob("test", nil)
	require.NoError(t, scheduler.ScheduleJob(job, future))

	scheduled := scheduler.GetScheduledJobs()
	assert.GreaterOrEqual(t, len(scheduled), 1)
}

func TestScheduler_CleanupJobs(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 50 * time.Millisecond,
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

	scheduler.mu.Lock()
	completedAt := time.Now().Add(-25 * time.Hour)
	scheduler.jobs[job.ID].CompletedAt = &completedAt
	scheduler.mu.Unlock()

	time.Sleep(200 * time.Millisecond)

	scheduler.mu.RLock()
	_, exists := scheduler.jobs[job.ID]
	scheduler.mu.RUnlock()
	assert.False(t, exists)
}

func TestScheduler_EmitEvent_FullChannel(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	for i := 0; i < 1100; i++ {
		scheduler.emitEvent("test", NewJob("test", nil))
	}
}

func TestPriorityQueue_Dequeue_ContextCancelled(t *testing.T) {
	queue := NewPriorityQueue(4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := queue.Dequeue(ctx)
	require.Error(t, err)
}

func TestPriorityQueue_Dequeue_Success(t *testing.T) {
	queue := NewPriorityQueue(4)
	require.NoError(t, queue.Enqueue(&Job{ID: "job1", Priority: JobPriorityHigh, CreatedAt: time.Now()}))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	job, err := queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "job1", job.ID)
}

func TestPriorityQueue_Ordering(t *testing.T) {
	queue := NewPriorityQueue(10)
	now := time.Now()

	require.NoError(t, queue.Enqueue(&Job{ID: "low", Priority: JobPriorityLow, CreatedAt: now}))
	require.NoError(t, queue.Enqueue(&Job{ID: "high", Priority: JobPriorityHigh, CreatedAt: now}))
	require.NoError(t, queue.Enqueue(&Job{ID: "medium", Priority: JobPriorityMedium, CreatedAt: now}))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	job, err := queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "high", job.ID)

	job, err = queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "medium", job.ID)

	job, err = queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "low", job.ID)
}

func TestScheduler_GetJob_WithMetadata(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	job := &Job{
		ID:       "meta-job",
		Type:     "test",
		Priority: JobPriorityMedium,
		Metadata: map[string]interface{}{"key": "value"},
	}
	scheduler.mu.Lock()
	scheduler.jobs["meta-job"] = job
	scheduler.mu.Unlock()

	loaded, err := scheduler.GetJob("meta-job")
	require.NoError(t, err)
	assert.Equal(t, "value", loaded.Metadata["key"])
}

func TestScheduler_CancelJob_QueuedJob(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	require.NoError(t, scheduler.Start())

	job := &Job{ID: "cancel-queued", Type: "test", Status: JobStatusQueued}
	scheduler.mu.Lock()
	scheduler.jobs["cancel-queued"] = job
	scheduler.mu.Unlock()

	err := scheduler.CancelJob("cancel-queued")
	require.NoError(t, err)

	loaded, err := scheduler.GetJob("cancel-queued")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, loaded.Status)
}

func TestScheduler_CancelJob_FailedJob(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	job := &Job{ID: "cancel-failed", Type: "test", Status: JobStatusFailed}
	scheduler.mu.Lock()
	scheduler.jobs["cancel-failed"] = job
	scheduler.mu.Unlock()

	err := scheduler.CancelJob("cancel-failed")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot cancel")
}

func TestScheduler_ListJobs_WithStatus(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      1,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.mu.Lock()
	scheduler.jobs["j1"] = &Job{ID: "j1", Status: JobStatusCompleted}
	scheduler.jobs["j2"] = &Job{ID: "j2", Status: JobStatusFailed}
	scheduler.mu.Unlock()

	completed, err := scheduler.ListJobs(JobStatusCompleted, 10, 0)
	require.NoError(t, err)
	assert.Len(t, completed, 1)
	assert.Equal(t, "j1", completed[0].ID)
}

func TestScheduler_FailJob_WithRetry(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		MaxWorkers:      1,
		QueueSize:       8,
		JobTimeout:      time.Second,
		MaxRetries:      3,
		CleanupInterval: 0,
	}, zap.NewNop())
	t.Cleanup(func() { _ = scheduler.Stop() })

	scheduler.RegisterExecutor("test", NewFuncExecutor("test", func(ctx context.Context, job *Job) (interface{}, error) {
		return "ok", nil
	}))
	require.NoError(t, scheduler.Start())

	job := NewJob("test", nil)
	job.MaxRetries = 3
	require.NoError(t, scheduler.SubmitJob(job))

	require.Eventually(t, func() bool {
		loaded, err := scheduler.GetJob(job.ID)
		return err == nil && loaded.Status == JobStatusCompleted
	}, 2*time.Second, 20*time.Millisecond)
}

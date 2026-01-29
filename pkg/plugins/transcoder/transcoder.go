package transcoder

import (
	"context"
	"fmt"
	"sync"
	"time"

	"streamgate/pkg/core"
)

// TranscodeTask represents a transcoding task
type TranscodeTask struct {
	ID              string
	FileID          string
	FilePath        string
	Status          TaskStatus
	Priority        int
	CreatedAt       time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	Profiles        []TranscodeProfile
	Progress        float64
	Error           string
	WorkerID        string
	RetryCount      int
	MaxRetries      int
}

// TaskStatus represents the status of a transcoding task
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusProcessing TaskStatus = "processing"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

// TranscodeProfile represents a transcoding profile
type TranscodeProfile struct {
	Resolution string
	Bitrate    string
	Format     string
}

// TaskQueue manages transcoding tasks with priority queue
type TaskQueue struct {
	tasks    map[string]*TranscodeTask
	queue    chan *TranscodeTask
	mu       sync.RWMutex
	maxSize  int
	metrics  *QueueMetrics
}

// QueueMetrics tracks queue statistics
type QueueMetrics struct {
	TotalEnqueued   int64
	TotalProcessed  int64
	TotalFailed     int64
	CurrentQueueLen int
	AverageWaitTime time.Duration
	mu              sync.RWMutex
}

// WorkerPool manages concurrent transcoding workers
type WorkerPool struct {
	workers       []*Worker
	taskQueue     *TaskQueue
	eventBus      core.EventBus
	logger        core.Logger
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	metrics       *WorkerMetrics
	scalingPolicy *ScalingPolicy
}

// Worker represents a transcoding worker
type Worker struct {
	ID              string
	Status          WorkerStatus
	CurrentTask     *TranscodeTask
	CompletedTasks  int64
	FailedTasks     int64
	TotalProcessing time.Duration
	LastHeartbeat   time.Time
	mu              sync.RWMutex
}

// WorkerStatus represents the status of a worker
type WorkerStatus string

const (
	WorkerStatusIdle    WorkerStatus = "idle"
	WorkerStatusBusy    WorkerStatus = "busy"
	WorkerStatusUnhealthy WorkerStatus = "unhealthy"
)

// WorkerMetrics tracks worker statistics
type WorkerMetrics struct {
	TotalWorkers      int
	ActiveWorkers     int
	IdleWorkers       int
	UnhealthyWorkers  int
	TotalTasksProcessed int64
	TotalTasksFailed  int64
	AverageTaskTime   time.Duration
	mu                sync.RWMutex
}

// ScalingPolicy defines auto-scaling rules
type ScalingPolicy struct {
	MinWorkers        int
	MaxWorkers        int
	TargetQueueLen    int
	ScaleUpThreshold  float64   // Queue length / workers ratio
	ScaleDownThreshold float64
	CheckInterval     time.Duration
}

// TranscoderPlugin implements the transcoder plugin
type TranscoderPlugin struct {
	name          string
	version       string
	dependencies  []string
	config        *TranscoderConfig
	taskQueue     *TaskQueue
	workerPool    *WorkerPool
	eventBus      core.EventBus
	logger        core.Logger
	mu            sync.RWMutex
}

// TranscoderConfig holds transcoder configuration
type TranscoderConfig struct {
	WorkerPoolSize      int
	MaxConcurrentTasks  int
	MaxQueueSize        int
	TaskTimeout         time.Duration
	HealthCheckInterval time.Duration
	ScalingPolicy       *ScalingPolicy
}

// NewTranscoderPlugin creates a new transcoder plugin
func NewTranscoderPlugin(config *TranscoderConfig) *TranscoderPlugin {
	return &TranscoderPlugin{
		name:         "transcoder",
		version:      "1.0.0",
		dependencies: []string{"storage", "event-bus"},
		config:       config,
	}
}

// Name returns the plugin name
func (tp *TranscoderPlugin) Name() string {
	return tp.name
}

// Version returns the plugin version
func (tp *TranscoderPlugin) Version() string {
	return tp.version
}

// Dependencies returns plugin dependencies
func (tp *TranscoderPlugin) Dependencies() []string {
	return tp.dependencies
}

// Init initializes the transcoder plugin
func (tp *TranscoderPlugin) Init(ctx context.Context, config core.Config) error {
	tp.logger = config.Logger()
	tp.eventBus = config.EventBus()

	// Initialize task queue
	tp.taskQueue = &TaskQueue{
		tasks:   make(map[string]*TranscodeTask),
		queue:   make(chan *TranscodeTask, tp.config.MaxQueueSize),
		maxSize: tp.config.MaxQueueSize,
		metrics: &QueueMetrics{},
	}

	// Initialize worker pool
	tp.workerPool = &WorkerPool{
		workers:       make([]*Worker, 0, tp.config.WorkerPoolSize),
		taskQueue:     tp.taskQueue,
		eventBus:      tp.eventBus,
		logger:        tp.logger,
		metrics:       &WorkerMetrics{},
		scalingPolicy: tp.config.ScalingPolicy,
	}

	tp.logger.Info("Transcoder plugin initialized",
		"workers", tp.config.WorkerPoolSize,
		"max_queue", tp.config.MaxQueueSize)

	return nil
}

// Start starts the transcoder plugin
func (tp *TranscoderPlugin) Start(ctx context.Context) error {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	// Start worker pool
	if err := tp.workerPool.Start(ctx, tp.config.WorkerPoolSize); err != nil {
		return fmt.Errorf("failed to start worker pool: %w", err)
	}

	// Start auto-scaling monitor
	go tp.monitorAutoScaling(ctx)

	// Start health check
	go tp.healthCheck(ctx)

	tp.logger.Info("Transcoder plugin started")
	return nil
}

// Stop stops the transcoder plugin
func (tp *TranscoderPlugin) Stop(ctx context.Context) error {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if err := tp.workerPool.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop worker pool: %w", err)
	}

	tp.logger.Info("Transcoder plugin stopped")
	return nil
}

// Destroy destroys the transcoder plugin
func (tp *TranscoderPlugin) Destroy() error {
	return nil
}

// HealthCheck performs health check
func (tp *TranscoderPlugin) HealthCheck() error {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	if tp.workerPool == nil {
		return fmt.Errorf("worker pool not initialized")
	}

	metrics := tp.workerPool.GetMetrics()
	if metrics.UnhealthyWorkers > 0 {
		return fmt.Errorf("unhealthy workers detected: %d", metrics.UnhealthyWorkers)
	}

	return nil
}

// SubmitTask submits a transcoding task
func (tp *TranscoderPlugin) SubmitTask(task *TranscodeTask) error {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	if err := tp.taskQueue.Enqueue(task); err != nil {
		return err
	}

	tp.eventBus.Publish(core.Event{
		Type: "transcode.task.submitted",
		Data: task,
	})

	return nil
}

// GetTaskStatus returns the status of a task
func (tp *TranscoderPlugin) GetTaskStatus(taskID string) (*TranscodeTask, error) {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	return tp.taskQueue.GetTask(taskID)
}

// CancelTask cancels a transcoding task
func (tp *TranscoderPlugin) CancelTask(taskID string) error {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	return tp.taskQueue.CancelTask(taskID)
}

// GetMetrics returns transcoder metrics
func (tp *TranscoderPlugin) GetMetrics() *WorkerMetrics {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	return tp.workerPool.GetMetrics()
}

// ScaleWorkers scales the worker pool
func (tp *TranscoderPlugin) ScaleWorkers(count int) error {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	return tp.workerPool.Scale(count)
}

// monitorAutoScaling monitors and performs auto-scaling
func (tp *TranscoderPlugin) monitorAutoScaling(ctx context.Context) {
	ticker := time.NewTicker(tp.config.ScalingPolicy.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tp.performAutoScaling()
		}
	}
}

// performAutoScaling performs auto-scaling based on queue length
func (tp *TranscoderPlugin) performAutoScaling() {
	tp.mu.RLock()
	metrics := tp.workerPool.GetMetrics()
	queueLen := tp.taskQueue.Len()
	tp.mu.RUnlock()

	if metrics.ActiveWorkers == 0 {
		return
	}

	ratio := float64(queueLen) / float64(metrics.ActiveWorkers)

	// Scale up
	if ratio > tp.config.ScalingPolicy.ScaleUpThreshold &&
		metrics.ActiveWorkers < tp.config.ScalingPolicy.MaxWorkers {
		newCount := metrics.ActiveWorkers + 1
		if err := tp.ScaleWorkers(newCount); err != nil {
			tp.logger.Error("Failed to scale up workers", "error", err)
		} else {
			tp.logger.Info("Scaled up workers", "new_count", newCount, "queue_len", queueLen)
		}
	}

	// Scale down
	if ratio < tp.config.ScalingPolicy.ScaleDownThreshold &&
		metrics.ActiveWorkers > tp.config.ScalingPolicy.MinWorkers &&
		queueLen == 0 {
		newCount := metrics.ActiveWorkers - 1
		if err := tp.ScaleWorkers(newCount); err != nil {
			tp.logger.Error("Failed to scale down workers", "error", err)
		} else {
			tp.logger.Info("Scaled down workers", "new_count", newCount)
		}
	}
}

// healthCheck performs periodic health checks on workers
func (tp *TranscoderPlugin) healthCheck(ctx context.Context) {
	ticker := time.NewTicker(tp.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tp.workerPool.HealthCheck()
		}
	}
}

// TaskQueue methods

// Enqueue adds a task to the queue
func (tq *TaskQueue) Enqueue(task *TranscodeTask) error {
	tq.mu.Lock()
	defer tq.mu.Unlock()

	if len(tq.queue) >= tq.maxSize {
		return fmt.Errorf("task queue is full")
	}

	task.Status = TaskStatusPending
	task.CreatedAt = time.Now()
	tq.tasks[task.ID] = task
	tq.queue <- task
	tq.metrics.TotalEnqueued++

	return nil
}

// Dequeue removes a task from the queue
func (tq *TaskQueue) Dequeue(ctx context.Context) (*TranscodeTask, error) {
	select {
	case task := <-tq.queue:
		return task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// GetTask returns a task by ID
func (tq *TaskQueue) GetTask(taskID string) (*TranscodeTask, error) {
	tq.mu.RLock()
	defer tq.mu.RUnlock()

	task, exists := tq.tasks[taskID]
	if !exists {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	return task, nil
}

// UpdateTask updates a task
func (tq *TaskQueue) UpdateTask(task *TranscodeTask) error {
	tq.mu.Lock()
	defer tq.mu.Unlock()

	tq.tasks[task.ID] = task
	return nil
}

// CancelTask cancels a task
func (tq *TaskQueue) CancelTask(taskID string) error {
	tq.mu.Lock()
	defer tq.mu.Unlock()

	task, exists := tq.tasks[taskID]
	if !exists {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if task.Status == TaskStatusProcessing {
		return fmt.Errorf("cannot cancel processing task")
	}

	task.Status = TaskStatusCancelled
	return nil
}

// Len returns the current queue length
func (tq *TaskQueue) Len() int {
	tq.mu.RLock()
	defer tq.mu.RUnlock()

	return len(tq.queue)
}

// WorkerPool methods

// Start starts the worker pool
func (wp *WorkerPool) Start(ctx context.Context, workerCount int) error {
	wp.ctx, wp.cancel = context.WithCancel(ctx)

	for i := 0; i < workerCount; i++ {
		worker := &Worker{
			ID:     fmt.Sprintf("worker-%d", i),
			Status: WorkerStatusIdle,
		}
		wp.workers = append(wp.workers, worker)

		wp.wg.Add(1)
		go wp.runWorker(worker)
	}

	wp.metrics.TotalWorkers = workerCount
	wp.metrics.ActiveWorkers = workerCount
	wp.metrics.IdleWorkers = workerCount

	wp.logger.Info("Worker pool started", "workers", workerCount)
	return nil
}

// Stop stops the worker pool
func (wp *WorkerPool) Stop(ctx context.Context) error {
	wp.cancel()
	done := make(chan struct{})

	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Scale scales the worker pool
func (wp *WorkerPool) Scale(targetCount int) error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	currentCount := len(wp.workers)

	if targetCount > currentCount {
		// Scale up
		for i := currentCount; i < targetCount; i++ {
			worker := &Worker{
				ID:     fmt.Sprintf("worker-%d", i),
				Status: WorkerStatusIdle,
			}
			wp.workers = append(wp.workers, worker)

			wp.wg.Add(1)
			go wp.runWorker(worker)
		}
		wp.metrics.TotalWorkers = targetCount
		wp.metrics.ActiveWorkers = targetCount
		wp.metrics.IdleWorkers = targetCount
	} else if targetCount < currentCount {
		// Scale down - mark workers for removal
		wp.workers = wp.workers[:targetCount]
		wp.metrics.TotalWorkers = targetCount
	}

	wp.logger.Info("Worker pool scaled", "target", targetCount, "current", currentCount)
	return nil
}

// runWorker runs a worker
func (wp *WorkerPool) runWorker(worker *Worker) {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.ctx.Done():
			return
		default:
			task, err := wp.taskQueue.Dequeue(wp.ctx)
			if err != nil {
				continue
			}

			wp.processTask(worker, task)
		}
	}
}

// processTask processes a transcoding task
func (wp *WorkerPool) processTask(worker *Worker, task *TranscodeTask) {
	worker.mu.Lock()
	worker.Status = WorkerStatusBusy
	worker.CurrentTask = task
	worker.mu.Unlock()

	task.Status = TaskStatusProcessing
	task.WorkerID = worker.ID
	now := time.Now()
	task.StartedAt = &now

	wp.taskQueue.UpdateTask(task)

	wp.eventBus.Publish(core.Event{
		Type: "transcode.task.started",
		Data: task,
	})

	// Simulate transcoding work
	startTime := time.Now()
	if err := wp.transcode(task); err != nil {
		task.Status = TaskStatusFailed
		task.Error = err.Error()
		task.RetryCount++

		if task.RetryCount < task.MaxRetries {
			task.Status = TaskStatusPending
			wp.taskQueue.Enqueue(task)
		}

		wp.taskQueue.metrics.TotalFailed++
		wp.eventBus.Publish(core.Event{
			Type: "transcode.task.failed",
			Data: task,
		})
	} else {
		task.Status = TaskStatusCompleted
		now := time.Now()
		task.CompletedAt = &now
		wp.taskQueue.metrics.TotalProcessed++

		wp.eventBus.Publish(core.Event{
			Type: "transcode.task.completed",
			Data: task,
		})
	}

	wp.taskQueue.UpdateTask(task)

	worker.mu.Lock()
	worker.Status = WorkerStatusIdle
	worker.CurrentTask = nil
	worker.CompletedTasks++
	worker.TotalProcessing += time.Since(startTime)
	worker.LastHeartbeat = time.Now()
	worker.mu.Unlock()

	wp.updateMetrics()
}

// transcode performs the actual transcoding
func (wp *WorkerPool) transcode(task *TranscodeTask) error {
	// Placeholder for actual transcoding logic
	// In production, this would call FFmpeg or similar
	time.Sleep(100 * time.Millisecond)
	task.Progress = 100.0
	return nil
}

// HealthCheck performs health checks on workers
func (wp *WorkerPool) HealthCheck() {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	now := time.Now()
	for _, worker := range wp.workers {
		worker.mu.RLock()
		lastHeartbeat := worker.LastHeartbeat
		status := worker.Status
		worker.mu.RUnlock()

		if now.Sub(lastHeartbeat) > 5*time.Minute && status == WorkerStatusBusy {
			worker.mu.Lock()
			worker.Status = WorkerStatusUnhealthy
			worker.mu.Unlock()

			wp.logger.Warn("Worker marked unhealthy", "worker_id", worker.ID)
		}
	}

	wp.updateMetrics()
}

// GetMetrics returns worker pool metrics
func (wp *WorkerPool) GetMetrics() *WorkerMetrics {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	metrics := &WorkerMetrics{
		TotalWorkers: len(wp.workers),
	}

	for _, worker := range wp.workers {
		worker.mu.RLock()
		switch worker.Status {
		case WorkerStatusIdle:
			metrics.IdleWorkers++
		case WorkerStatusBusy:
			metrics.ActiveWorkers++
		case WorkerStatusUnhealthy:
			metrics.UnhealthyWorkers++
		}
		metrics.TotalTasksProcessed += worker.CompletedTasks
		metrics.TotalTasksFailed += worker.FailedTasks
		worker.mu.RUnlock()
	}

	metrics.TotalTasksProcessed = wp.taskQueue.metrics.TotalProcessed
	metrics.TotalTasksFailed = wp.taskQueue.metrics.TotalFailed

	return metrics
}

// updateMetrics updates metrics
func (wp *WorkerPool) updateMetrics() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.metrics = wp.GetMetrics()
}

package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Scheduler schedules and manages jobs
type Scheduler struct {
	jobs      map[string]*Job
	queue     *PriorityQueue
	workers   map[string]*Worker
	executors map[string]JobExecutor
	mu        sync.RWMutex
	logger    *zap.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	config    *SchedulerConfig
	stats     *SchedulerStats
	eventChan chan *JobEvent
}

// SchedulerConfig holds scheduler configuration
type SchedulerConfig struct {
	MaxWorkers      int
	QueueSize       int
	JobTimeout      time.Duration
	MaxRetries      int
	CleanupInterval time.Duration
	EnableMetrics   bool
}

// SchedulerStats tracks scheduler statistics
type SchedulerStats struct {
	TotalJobs      int64
	CompletedJobs  int64
	FailedJobs     int64
	RunningJobs    int64
	QueuedJobs     int64
	CancelledJobs  int64
	AverageRuntime time.Duration
	mu             sync.RWMutex
}

// JobEvent represents a job event
type JobEvent struct {
	Type      string
	Job       *Job
	Timestamp time.Time
}

// NewScheduler creates a new job scheduler
func NewScheduler(config *SchedulerConfig, logger *zap.Logger) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	if config.MaxWorkers <= 0 {
		config.MaxWorkers = 10
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 1000
	}
	if config.JobTimeout <= 0 {
		config.JobTimeout = 30 * time.Minute
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 3
	}

	return &Scheduler{
		jobs:      make(map[string]*Job),
		queue:     NewPriorityQueue(config.QueueSize),
		workers:   make(map[string]*Worker),
		executors: make(map[string]JobExecutor),
		logger:    logger,
		ctx:       ctx,
		cancel:    cancel,
		config:    config,
		stats:     &SchedulerStats{},
		eventChan: make(chan *JobEvent, 1000),
	}
}

// Start starts the scheduler
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Starting job scheduler",
		zap.Int("max_workers", s.config.MaxWorkers),
		zap.Int("queue_size", s.config.QueueSize))

	// Start workers
	for i := 0; i < s.config.MaxWorkers; i++ {
		worker := NewWorker(fmt.Sprintf("worker-%d", i), s.logger)
		s.workers[worker.ID] = worker

		s.wg.Add(1)
		go s.runWorker(worker)
	}

	// Start event processor
	s.wg.Add(1)
	go s.processEvents()

	// Start cleanup goroutine
	if s.config.CleanupInterval > 0 {
		s.wg.Add(1)
		go s.cleanupJobs()
	}

	s.logger.Info("Job scheduler started")
	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop() error {
	s.logger.Info("Stopping job scheduler")

	s.cancel()

	// Cancel all running jobs
	s.mu.RLock()
	for _, job := range s.jobs {
		if job.Status == JobStatusRunning || job.Status == JobStatusQueued {
			s.CancelJob(job.ID)
		}
	}
	s.mu.RUnlock()

	// Wait for all workers to finish
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("Job scheduler stopped")
		return nil
	case <-time.After(30 * time.Second):
		s.logger.Warn("Job scheduler stop timeout")
		return fmt.Errorf("timeout waiting for workers to stop")
	}
}

// SubmitJob submits a new job
func (s *Scheduler) SubmitJob(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Set default values
	if job.ID == "" {
		job.ID = generateJobID()
	}
	if job.Status == "" {
		job.Status = JobStatusPending
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	if job.MaxRetries == 0 {
		job.MaxRetries = s.config.MaxRetries
	}
	if job.Timeout == 0 {
		job.Timeout = s.config.JobTimeout
	}
	if job.Priority == 0 {
		job.Priority = JobPriorityMedium
	}

	// Check if job already exists
	if _, exists := s.jobs[job.ID]; exists {
		return fmt.Errorf("job already exists: %s", job.ID)
	}

	// Store job
	s.jobs[job.ID] = job

	// Update stats
	s.stats.TotalJobs++

	// Queue job
	job.Status = JobStatusQueued
	if err := s.queue.Enqueue(job); err != nil {
		delete(s.jobs, job.ID)
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	// Emit event
	s.emitEvent("job.submitted", job)

	s.logger.Debug("Job submitted",
		zap.String("job_id", job.ID),
		zap.String("type", job.Type),
		zap.Int("priority", int(job.Priority)))

	return nil
}

// ScheduleJob schedules a job to run at a specific time
func (s *Scheduler) ScheduleJob(job *Job, scheduledAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Set default values
	if job.ID == "" {
		job.ID = generateJobID()
	}
	if job.Status == "" {
		job.Status = JobStatusPending
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.ScheduledAt = &scheduledAt

	// Store job
	s.jobs[job.ID] = job

	// Update stats
	s.stats.TotalJobs++

	// Start timer for scheduled job
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		now := time.Now()
		if scheduledAt.After(now) {
			duration := scheduledAt.Sub(now)
			s.logger.Debug("Job scheduled",
				zap.String("job_id", job.ID),
				zap.Duration("delay", duration))

			select {
			case <-time.After(duration):
				// Submit to queue
				job.Status = JobStatusQueued
				s.mu.Lock()
				s.queue.Enqueue(job)
				s.mu.Unlock()
				s.emitEvent("job.scheduled", job)
			case <-s.ctx.Done():
				return
			}
		} else {
			// Submit immediately
			job.Status = JobStatusQueued
			s.mu.Lock()
			s.queue.Enqueue(job)
			s.mu.Unlock()
			s.emitEvent("job.scheduled", job)
		}
	}()

	s.logger.Debug("Job scheduled",
		zap.String("job_id", job.ID),
		zap.Time("scheduled_at", scheduledAt))

	return nil
}

// GetJob gets a job by ID
func (s *Scheduler) GetJob(jobID string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	// Return a copy to avoid race conditions
	jobCopy := *job
	return &jobCopy, nil
}

// CancelJob cancels a job
func (s *Scheduler) CancelJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	switch job.Status {
	case JobStatusCompleted, JobStatusFailed, JobStatusCancelled:
		return fmt.Errorf("cannot cancel job with status: %s", job.Status)
	}

	job.Status = JobStatusCancelled
	now := time.Now()
	job.CompletedAt = &now

	// Update stats
	s.stats.CancelledJobs++

	// Emit event
	s.emitEvent("job.cancelled", job)

	s.logger.Debug("Job cancelled", zap.String("job_id", jobID))
	return nil
}

// RetryJob retries a failed job
func (s *Scheduler) RetryJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status != JobStatusFailed {
		return fmt.Errorf("can only retry failed jobs, current status: %s", job.Status)
	}

	if job.RetryCount >= job.MaxRetries {
		return fmt.Errorf("job has reached maximum retry count: %d", job.MaxRetries)
	}

	// Reset job for retry
	job.Status = JobStatusQueued
	job.RetryCount++
	job.Error = ""
	job.StartedAt = nil
	job.CompletedAt = nil
	job.Progress = 0

	// Re-queue job
	if err := s.queue.Enqueue(job); err != nil {
		return fmt.Errorf("failed to re-queue job: %w", err)
	}

	// Emit event
	s.emitEvent("job.retried", job)

	s.logger.Debug("Job retried",
		zap.String("job_id", jobID),
		zap.Int("retry_count", job.RetryCount))

	return nil
}

// ListJobs lists all jobs
func (s *Scheduler) ListJobs(status JobStatus, limit, offset int) ([]*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0)

	for _, job := range s.jobs {
		if status == "" || job.Status == status {
			jobCopy := *job
			jobs = append(jobs, &jobCopy)
		}
	}

	// Apply pagination
	if offset >= len(jobs) {
		return []*Job{}, nil
	}

	end := offset + limit
	if end > len(jobs) {
		end = len(jobs)
	}

	return jobs[offset:end], nil
}

// GetStats returns scheduler statistics
func (s *Scheduler) GetStats() *SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	return &SchedulerStats{
		TotalJobs:      s.stats.TotalJobs,
		CompletedJobs:  s.stats.CompletedJobs,
		FailedJobs:     s.stats.FailedJobs,
		RunningJobs:    s.stats.RunningJobs,
		QueuedJobs:     int64(s.queue.Len()),
		CancelledJobs:  s.stats.CancelledJobs,
		AverageRuntime: s.stats.AverageRuntime,
	}
}

// RegisterExecutor registers a job executor
func (s *Scheduler) RegisterExecutor(jobType string, executor JobExecutor) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.executors[jobType] = executor
	s.logger.Debug("Job executor registered", zap.String("type", jobType))
}

// runWorker runs a worker
func (s *Scheduler) runWorker(worker *Worker) {
	defer s.wg.Done()

	s.logger.Debug("Worker started", zap.String("worker_id", worker.ID))

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Debug("Worker stopped", zap.String("worker_id", worker.ID))
			return
		default:
			job, err := s.queue.Dequeue(s.ctx)
			if err != nil {
				continue
			}

			s.executeJob(worker, job)
		}
	}
}

// executeJob executes a job
func (s *Scheduler) executeJob(worker *Worker, job *Job) {
	// Update job status
	job.Status = JobStatusRunning
	job.WorkerID = worker.ID
	now := time.Now()
	job.StartedAt = &now

	s.mu.Lock()
	s.stats.RunningJobs++
	s.mu.Unlock()

	s.emitEvent("job.started", job)

	// Get executor
	s.mu.RLock()
	executor, exists := s.executors[job.Type]
	s.mu.RUnlock()

	if !exists {
		s.failJob(job, fmt.Errorf("no executor found for job type: %s", job.Type))
		return
	}

	// Execute job with timeout
	ctx, cancel := context.WithTimeout(s.ctx, job.Timeout)
	defer cancel()

	resultChan := make(chan interface{})
	errChan := make(chan error)

	go func() {
		result, err := executor.Execute(ctx, job)
		if err != nil {
			errChan <- err
		} else {
			resultChan <- result
		}
	}()

	// Wait for completion or timeout
	var result interface{}
	var err error

	select {
	case result = <-resultChan:
		// Job completed successfully
		s.completeJob(job, result)
	case err = <-errChan:
		// Job failed
		s.failJob(job, err)
	case <-ctx.Done():
		// Job timed out
		s.failJob(job, fmt.Errorf("job timeout"))
	case <-s.ctx.Done():
		// Scheduler shutting down
		return
	}

	worker.RecordJob(time.Since(now), err == nil)
}

// completeJob marks a job as completed
func (s *Scheduler) completeJob(job *Job, result interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job.Status = JobStatusCompleted
	job.Result = result
	job.Progress = 100
	now := time.Now()
	job.CompletedAt = &now

	// Update stats
	s.stats.RunningJobs--
	s.stats.CompletedJobs++

	// Update average runtime
	if job.StartedAt != nil {
		runtime := job.CompletedAt.Sub(*job.StartedAt)
		totalJobs := s.stats.CompletedJobs + s.stats.FailedJobs
		s.stats.AverageRuntime = time.Duration(
			(int64(s.stats.AverageRuntime)*(totalJobs-1) + int64(runtime)) / totalJobs,
		)
	}

	s.emitEvent("job.completed", job)

	s.logger.Debug("Job completed",
		zap.String("job_id", job.ID),
		zap.Duration("runtime", job.CompletedAt.Sub(*job.StartedAt)))
}

// failJob marks a job as failed
func (s *Scheduler) failJob(job *Job, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job.Status = JobStatusFailed
	job.Error = err.Error()
	job.RetryCount++
	now := time.Now()
	job.CompletedAt = &now

	// Update stats
	s.stats.RunningJobs--
	s.stats.FailedJobs++

	// Auto-retry if within limit
	if job.RetryCount < job.MaxRetries {
		job.Status = JobStatusQueued
		job.StartedAt = nil
		job.CompletedAt = nil
		job.Error = ""
		job.Progress = 0

		s.queue.Enqueue(job)
		s.emitEvent("job.retried", job)

		s.logger.Debug("Job failed, retrying",
			zap.String("job_id", job.ID),
			zap.Int("retry_count", job.RetryCount),
			zap.Error(err))
	} else {
		s.emitEvent("job.failed", job)

		s.logger.Debug("Job failed permanently",
			zap.String("job_id", job.ID),
			zap.Int("retry_count", job.RetryCount),
			zap.Error(err))
	}
}

// processEvents processes job events
func (s *Scheduler) processEvents() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case event := <-s.eventChan:
			s.logger.Debug("Job event",
				zap.String("type", event.Type),
				zap.String("job_id", event.Job.ID))
		}
	}
}

// emitEvent emits a job event
func (s *Scheduler) emitEvent(eventType string, job *Job) {
	select {
	case s.eventChan <- &JobEvent{
		Type:      eventType,
		Job:       job,
		Timestamp: time.Now(),
	}:
	default:
		s.logger.Warn("Event channel full, dropping event", zap.String("type", eventType))
	}
}

// cleanupJobs cleans up old completed/failed jobs
func (s *Scheduler) cleanupJobs() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			cleaned := 0

			for id, job := range s.jobs {
				// Clean up jobs older than 24 hours
				if job.CompletedAt != nil && now.Sub(*job.CompletedAt) > 24*time.Hour {
					if job.Status == JobStatusCompleted || job.Status == JobStatusFailed || job.Status == JobStatusCancelled {
						delete(s.jobs, id)
						cleaned++
					}
				}
			}

			s.mu.Unlock()

			if cleaned > 0 {
				s.logger.Debug("Cleaned up old jobs", zap.Int("count", cleaned))
			}
		}
	}
}

// generateJobID generates a unique job ID
func generateJobID() string {
	return fmt.Sprintf("job-%d", time.Now().UnixNano())
}

// GetScheduledJobs gets all scheduled jobs
func (s *Scheduler) GetScheduledJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0)
	for _, job := range s.jobs {
		if job.ScheduledAt != nil && job.Status == JobStatusPending {
			jobCopy := *job
			jobs = append(jobs, &jobCopy)
		}
	}
	return jobs
}

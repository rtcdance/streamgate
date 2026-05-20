package worker

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"

	"go.uber.org/zap"
)

// WorkerServer handles background job processing
type WorkerServer struct {
	config    *config.Config
	logger    *zap.Logger
	kernel    *core.Microkernel
	server    *http.Server
	scheduler *JobScheduler
}

// NewWorkerServer creates a new worker server
func NewWorkerServer(cfg *config.Config, logger *zap.Logger, kernel *core.Microkernel) (*WorkerServer, error) {
	scheduler := NewJobScheduler(logger)

	return &WorkerServer{
		config:    cfg,
		logger:    logger,
		kernel:    kernel,
		scheduler: scheduler,
	}, nil
}

// Start starts the worker server
func (s *WorkerServer) Start(ctx context.Context) error {
	handler := NewWorkerHandler(s.scheduler, s.logger, s.kernel)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/health/live", handler.HealthHandler)
	mux.HandleFunc("/health/ready", handler.ReadyHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)

	// Job endpoints
	mux.HandleFunc("/api/v1/jobs/submit", handler.SubmitJobHandler)
	mux.HandleFunc("/api/v1/jobs/status", handler.GetJobStatusHandler)
	mux.HandleFunc("/api/v1/jobs/cancel", handler.CancelJobHandler)
	mux.HandleFunc("/api/v1/jobs/list", handler.ListJobsHandler)
	mux.HandleFunc("/api/v1/jobs/schedule", handler.ScheduleJobHandler)

	// Catch-all for 404
	mux.HandleFunc("/", handler.NotFoundHandler)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
	}

	// Start scheduler
	s.scheduler.Start(ctx)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Worker server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop stops the worker server
func (s *WorkerServer) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down worker server", zap.Error(err))
			return err
		}
	}

	if s.scheduler != nil {
		s.scheduler.Stop()
	}

	return nil
}

// Health checks the health of the worker server
func (s *WorkerServer) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("worker server not started")
	}

	if s.scheduler == nil {
		return fmt.Errorf("job scheduler not initialized")
	}

	return nil
}

// JobScheduler manages job scheduling and execution
type JobScheduler struct {
	logger   *zap.Logger
	jobQueue chan *Job
	jobs     map[string]*Job
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.RWMutex
}

// NewJobScheduler creates a new job scheduler
func NewJobScheduler(logger *zap.Logger) *JobScheduler {
	return &JobScheduler{
		logger:   logger,
		jobQueue: make(chan *Job, 100),
		jobs:     make(map[string]*Job),
	}
}

// Start starts the job scheduler
func (s *JobScheduler) Start(ctx context.Context) {
	if s.running {
		return
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.running = true

	go s.processJobs()
	s.logger.Info("Job scheduler started")
}

// Stop stops the job scheduler
func (s *JobScheduler) Stop() {
	if !s.running {
		return
	}

	s.running = false
	s.cancel()
	close(s.jobQueue)

	s.logger.Info("Job scheduler stopped")
}

// SubmitJob submits a job for processing
func (s *JobScheduler) SubmitJob(job *Job) error {
	if !s.running {
		return fmt.Errorf("scheduler not running")
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	select {
	case s.jobQueue <- job:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("job queue full")
	}
}

// processJobs processes jobs from the queue
func (s *JobScheduler) processJobs() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-s.jobQueue:
			if job == nil {
				return
			}
			s.executeJob(job)
		}
	}
}

// executeJob executes a job
func (s *JobScheduler) executeJob(job *Job) {
	s.logger.Info("Executing job", zap.String("job_id", job.ID), zap.String("type", job.Type))

	s.mu.Lock()
	defer s.mu.Unlock()

	if j, exists := s.jobs[job.ID]; exists {
		j.Status = "completed"
		s.logger.Warn("JobScheduler has no executor configured; job marked completed without execution",
			zap.String("job_id", job.ID), zap.String("type", job.Type))
	}
}

func (s *JobScheduler) GetJob(jobID string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}
	return job, nil
}

func (s *JobScheduler) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (s *JobScheduler) CancelJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}
	job.Status = "cancelled"
	return nil
}

type ScheduledJob struct {
	ID       string `json:"id"`
	JobType  string `json:"job_type"`
	Schedule string `json:"schedule"` // cron expression
	Enabled  bool   `json:"enabled"`
	LastRun  int64  `json:"last_run,omitempty"`
	NextRun  int64  `json:"next_run,omitempty"`
}

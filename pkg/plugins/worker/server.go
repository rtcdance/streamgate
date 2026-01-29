package worker

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
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

	// Health endpoints
	mux.HandleFunc("/health", handler.HealthHandler)
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
			s.logger.Error("Worker server error", "error", err)
		}
	}()

	return nil
}

// Stop stops the worker server
func (s *WorkerServer) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down worker server", "error", err)
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
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewJobScheduler creates a new job scheduler
func NewJobScheduler(logger *zap.Logger) *JobScheduler {
	return &JobScheduler{
		logger:   logger,
		jobQueue: make(chan *Job, 100),
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
	s.logger.Info("Executing job", "job_id", job.ID, "type", job.Type)

	// TODO: Implement job execution
	// - Execute job based on type
	// - Handle retries
	// - Update job status
	// - Handle errors

	job.Status = "completed"
	job.CompletedAt = time.Now().Unix()
}

// Job represents a background job
type Job struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`   // email, notification, cleanup, etc.
	Status       string                 `json:"status"` // pending, processing, completed, failed
	Payload      map[string]interface{} `json:"payload"`
	Retries      int                    `json:"retries"`
	MaxRetries   int                    `json:"max_retries"`
	Error        string                 `json:"error,omitempty"`
	CreatedAt    int64                  `json:"created_at"`
	CompletedAt  int64                  `json:"completed_at,omitempty"`
	ScheduledFor int64                  `json:"scheduled_for,omitempty"`
}

// ScheduledJob represents a scheduled job
type ScheduledJob struct {
	ID       string `json:"id"`
	JobType  string `json:"job_type"`
	Schedule string `json:"schedule"` // cron expression
	Enabled  bool   `json:"enabled"`
	LastRun  int64  `json:"last_run,omitempty"`
	NextRun  int64  `json:"next_run,omitempty"`
}

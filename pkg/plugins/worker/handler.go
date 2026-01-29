package worker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/security"
)

// WorkerHandler handles worker requests
type WorkerHandler struct {
	scheduler        *JobScheduler
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
	rateLimiter      *security.RateLimiter
	auditLogger      *security.AuditLogger
}

// NewWorkerHandler creates a new worker handler
func NewWorkerHandler(scheduler *JobScheduler, logger *zap.Logger, kernel *core.Microkernel) *WorkerHandler {
	return &WorkerHandler{
		scheduler:        scheduler,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
		rateLimiter:      security.NewRateLimiter(100, 10, time.Second, logger),
		auditLogger:      security.NewAuditLogger(logger),
	}
}

// HealthHandler handles health check requests
func (h *WorkerHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.kernel.Health(ctx); err != nil {
		h.logger.Error("Health check failed", zap.Error(err))
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// ReadyHandler handles readiness check requests
func (h *WorkerHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// SubmitJobHandler handles job submission
func (h *WorkerHandler) SubmitJobHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("submit_job_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("submit_job_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	var job Job
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		h.logger.Error("Failed to decode job", zap.Error(err))
		h.metricsCollector.IncrementCounter("submit_job_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid job"})
		return
	}

	// Generate job ID
	job.ID = fmt.Sprintf("job_%d", len(job.ID))
	job.Status = "pending"

	if err := h.scheduler.SubmitJob(&job); err != nil {
		h.logger.Error("Failed to submit job", zap.Error(err))
		h.metricsCollector.IncrementCounter("submit_job_failed", map[string]string{})
		h.auditLogger.LogEvent("worker", clientIP, "submit_job", job.ID, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to submit job"})
		return
	}

	h.logger.Info("Job submitted", "job_id", job.ID)

	// Record metrics
	h.metricsCollector.IncrementCounter("submit_job_success", map[string]string{})
	h.metricsCollector.RecordTimer("submit_job_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("worker", clientIP, "submit_job", job.ID, "success", nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(job)
}

// GetJobStatusHandler handles job status requests
func (h *WorkerHandler) GetJobStatusHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_job_status_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("get_job_status_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		h.metricsCollector.IncrementCounter("get_job_status_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing job_id"})
		return
	}

	h.logger.Info("Getting job status", "job_id", jobID)

	// TODO: Retrieve job status from storage
	job := &Job{
		ID:     jobID,
		Status: "processing",
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_job_status_success", map[string]string{})
	h.metricsCollector.RecordTimer("get_job_status_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(job)
}

// CancelJobHandler handles job cancellation
func (h *WorkerHandler) CancelJobHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("cancel_job_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("cancel_job_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		h.metricsCollector.IncrementCounter("cancel_job_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing job_id"})
		return
	}

	h.logger.Info("Cancelling job", "job_id", jobID)

	// TODO: Cancel job in scheduler

	// Record metrics
	h.metricsCollector.IncrementCounter("cancel_job_success", map[string]string{})
	h.metricsCollector.RecordTimer("cancel_job_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("worker", clientIP, "cancel_job", jobID, "success", nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// ListJobsHandler handles job listing
func (h *WorkerHandler) ListJobsHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("list_jobs_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("list_jobs_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	h.logger.Info("Listing jobs")

	// TODO: Retrieve jobs from storage
	jobs := []*Job{}

	// Record metrics
	h.metricsCollector.IncrementCounter("list_jobs_success", map[string]string{})
	h.metricsCollector.RecordHistogram("list_jobs_count", float64(len(jobs)), map[string]string{})
	h.metricsCollector.RecordTimer("list_jobs_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(jobs)
}

// ScheduleJobHandler handles job scheduling
func (h *WorkerHandler) ScheduleJobHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("schedule_job_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("schedule_job_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	var scheduled ScheduledJob
	if err := json.NewDecoder(r.Body).Decode(&scheduled); err != nil {
		h.logger.Error("Failed to decode scheduled job", zap.Error(err))
		h.metricsCollector.IncrementCounter("schedule_job_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid scheduled job"})
		return
	}

	h.logger.Info("Scheduling job", "job_type", scheduled.JobType, "schedule", scheduled.Schedule)

	// TODO: Store scheduled job
	// - Validate cron expression
	// - Store in database
	// - Start scheduler

	// Record metrics
	h.metricsCollector.IncrementCounter("schedule_job_success", map[string]string{})
	h.metricsCollector.RecordTimer("schedule_job_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("worker", clientIP, "schedule_job", scheduled.JobType, "success", nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(scheduled)
}

// NotFoundHandler handles 404 requests
func (h *WorkerHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

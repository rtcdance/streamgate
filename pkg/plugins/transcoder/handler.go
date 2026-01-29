package transcoder

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/security"
	"go.uber.org/zap"
)

// TranscoderHandler handles transcoding requests
type TranscoderHandler struct {
	plugin             *TranscoderPlugin
	logger             *zap.Logger
	kernel             *core.Microkernel
	metricsCollector   *monitoring.MetricsCollector
	rateLimiter        *security.RateLimiter
	auditLogger        *security.AuditLogger
}

// NewTranscoderHandler creates a new transcoder handler
func NewTranscoderHandler(plugin *TranscoderPlugin, logger *zap.Logger, kernel *core.Microkernel) *TranscoderHandler {
	return &TranscoderHandler{
		plugin:             plugin,
		logger:             logger,
		kernel:             kernel,
		metricsCollector:   monitoring.NewMetricsCollector(logger),
		rateLimiter:        security.NewRateLimiter(50, 5, time.Second, logger),
		auditLogger:        security.NewAuditLogger(logger),
	}
}

// HealthHandler handles health check requests
func (h *TranscoderHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.kernel.Health(ctx); err != nil {
		h.logger.Error("Health check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// ReadyHandler handles readiness check requests
func (h *TranscoderHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// SubmitTaskHandler handles transcoding task submission
func (h *TranscoderHandler) SubmitTaskHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("submit_task_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("submit_task_rate_limit_exceeded", map[string]string{})
		h.auditLogger.LogEvent("transcoder", clientIP, "submit_task", "unknown", "rate_limit_exceeded", nil)
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	var task TranscodeTask
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		h.logger.Error("Failed to decode task", "error", err)
		h.metricsCollector.IncrementCounter("submit_task_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid task"})
		return
	}

	// Generate task ID
	task.ID = fmt.Sprintf("task_%d_%d", time.Now().Unix(), len(task.ID))
	task.Status = TaskStatusPending
	task.CreatedAt = time.Now()
	task.MaxRetries = 3

	if err := h.plugin.SubmitTask(&task); err != nil {
		h.logger.Error("Failed to submit task", "error", err)
		h.metricsCollector.IncrementCounter("submit_task_failed", map[string]string{})
		h.auditLogger.LogEvent("transcoder", clientIP, "submit_task", task.ID, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to submit task"})
		return
	}

	h.logger.Info("Transcoding task submitted", "task_id", task.ID, "file_id", task.FileID)

	// Record metrics
	h.metricsCollector.IncrementCounter("submit_task_success", map[string]string{})
	h.metricsCollector.RecordTimer("submit_task_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("transcoder", clientIP, "submit_task", task.ID, "success", nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(task)
}

// GetTaskStatusHandler handles task status requests
func (h *TranscoderHandler) GetTaskStatusHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_task_status_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("get_task_status_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		h.metricsCollector.IncrementCounter("get_task_status_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing task_id"})
		return
	}

	task, err := h.plugin.GetTaskStatus(taskID)
	if err != nil {
		h.logger.Error("Failed to get task status", "error", err)
		h.metricsCollector.IncrementCounter("get_task_status_failed", map[string]string{})
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_task_status_success", map[string]string{})
	h.metricsCollector.RecordTimer("get_task_status_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(task)
}

// CancelTaskHandler handles task cancellation
func (h *TranscoderHandler) CancelTaskHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("cancel_task_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("cancel_task_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		h.metricsCollector.IncrementCounter("cancel_task_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing task_id"})
		return
	}

	if err := h.plugin.CancelTask(taskID); err != nil {
		h.logger.Error("Failed to cancel task", "error", err)
		h.metricsCollector.IncrementCounter("cancel_task_failed", map[string]string{})
		h.auditLogger.LogEvent("transcoder", clientIP, "cancel_task", taskID, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to cancel task"})
		return
	}

	h.logger.Info("Transcoding task cancelled", "task_id", taskID)

	// Record metrics
	h.metricsCollector.IncrementCounter("cancel_task_success", map[string]string{})
	h.metricsCollector.RecordTimer("cancel_task_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("transcoder", clientIP, "cancel_task", taskID, "success", nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// ListTasksHandler handles task listing
func (h *TranscoderHandler) ListTasksHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("list_tasks_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("list_tasks_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	h.logger.Info("Listing transcoding tasks")

	// TODO: Retrieve tasks from storage
	tasks := []*TranscodeTask{}

	// Record metrics
	h.metricsCollector.IncrementCounter("list_tasks_success", map[string]string{})
	h.metricsCollector.RecordHistogram("list_tasks_count", float64(len(tasks)), map[string]string{})
	h.metricsCollector.RecordTimer("list_tasks_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(tasks)
}

// GetMetricsHandler handles metrics requests
func (h *TranscoderHandler) GetMetricsHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_metrics_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("get_metrics_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	metrics := h.plugin.GetMetrics()

	// Record metrics
	h.metricsCollector.IncrementCounter("get_metrics_success", map[string]string{})
	h.metricsCollector.RecordTimer("get_metrics_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(metrics)
}

// NotFoundHandler handles 404 requests
func (h *TranscoderHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

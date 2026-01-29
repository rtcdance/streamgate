package transcoder

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
)

// TranscoderHandler handles transcoding requests
type TranscoderHandler struct {
	plugin           *TranscoderPlugin
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
}

// NewTranscoderHandler creates a new transcoder handler
func NewTranscoderHandler(plugin *TranscoderPlugin, logger *zap.Logger, kernel *core.Microkernel) *TranscoderHandler {
	return &TranscoderHandler{
		plugin:           plugin,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
	}
}

// HealthHandler handles health check requests
func (h *TranscoderHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
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
func (h *TranscoderHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// SubmitTaskHandler handles transcoding task submission
func (h *TranscoderHandler) SubmitTaskHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("submit_task_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	var task TranscodeTask
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		h.logger.Error("Failed to decode task", zap.Error(err))
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
		h.logger.Error("Failed to submit task", zap.Error(err))
		h.metricsCollector.IncrementCounter("submit_task_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to submit task"})
		return
	}

	h.logger.Info("Transcoding task submitted", zap.String("task_id", task.ID), zap.String("file_id", task.FileID))

	// Record metrics
	h.metricsCollector.IncrementCounter("submit_task_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(task)
}

// GetTaskStatusHandler handles task status requests
func (h *TranscoderHandler) GetTaskStatusHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_task_status_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		h.metricsCollector.IncrementCounter("get_task_status_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing task_id"})
		return
	}

	task, err := h.plugin.GetTaskStatus(taskID)
	if err != nil {
		h.logger.Error("Failed to get task status", zap.Error(err))
		h.metricsCollector.IncrementCounter("get_task_status_failed", map[string]string{})
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_task_status_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(task)
}

// CancelTaskHandler handles task cancellation
func (h *TranscoderHandler) CancelTaskHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("cancel_task_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		h.metricsCollector.IncrementCounter("cancel_task_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing task_id"})
		return
	}

	if err := h.plugin.CancelTask(taskID); err != nil {
		h.logger.Error("Failed to cancel task", zap.Error(err))
		h.metricsCollector.IncrementCounter("cancel_task_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to cancel task"})
		return
	}

	h.logger.Info("Transcoding task cancelled", zap.String("task_id", taskID))

	// Record metrics
	h.metricsCollector.IncrementCounter("cancel_task_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// ListTasksHandler handles task listing
func (h *TranscoderHandler) ListTasksHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("list_tasks_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	h.logger.Info("Listing transcoding tasks")

	// TODO: Retrieve tasks from storage
	tasks := []*TranscodeTask{}

	// Record metrics
	h.metricsCollector.IncrementCounter("list_tasks_success", map[string]string{})
	h.metricsCollector.RecordHistogram("list_tasks_count", float64(len(tasks)), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(tasks)
}

// GetMetricsHandler handles metrics requests
func (h *TranscoderHandler) GetMetricsHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_metrics_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	metrics := h.plugin.GetMetrics()

	// Record metrics
	h.metricsCollector.IncrementCounter("get_metrics_success", map[string]string{})

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

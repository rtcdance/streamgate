package transcoder

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/service"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func generateTaskID() string {
	return "task_" + uuid.New().String()
}

func sanitizeFilePath(raw string) string {
	cleaned := filepath.Clean(raw)
	if strings.Contains(cleaned, "..") {
		return ""
	}
	if !filepath.IsAbs(cleaned) {
		return ""
	}
	return cleaned
}

// TranscoderHandler handles transcoding requests
type TranscoderHandler struct {
	plugin           *TranscoderPlugin
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
}

type submitTranscodeRequest struct {
	FileID   string             `json:"file_id"`
	FilePath string             `json:"file_path"`
	Profiles []TranscodeProfile `json:"profiles"`
	Priority int                `json:"priority"`
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

func resolveTaskID(r *http.Request, prefix string) string {
	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
	if taskID != "" {
		return taskID
	}

	if strings.HasPrefix(r.URL.Path, prefix) {
		taskID = strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
		if taskID != "" {
			return taskID
		}
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return ""
}

// HealthHandler handles health check requests
func (h *TranscoderHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.kernel.Health(ctx); err != nil {
		h.logger.Error("Health check failed", zap.Error(err))
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// ReadyHandler handles readiness check requests
func (h *TranscoderHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// SubmitTaskHandler handles transcoding task submission
func (h *TranscoderHandler) SubmitTaskHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("submit_task_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var req submitTranscodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode task", zap.Error(err))
		h.metricsCollector.IncrementCounter("submit_task_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid task"})
		return
	}

	// Generate task ID
	task := TranscodeTask{
		ID:         generateTaskID(),
		FileID:     strings.TrimSpace(req.FileID),
		FilePath:   sanitizeFilePath(strings.TrimSpace(req.FilePath)),
		Status:     TaskStatusPending,
		Priority:   req.Priority,
		CreatedAt:  time.Now(),
		Profiles:   req.Profiles,
		MaxRetries: 3,
	}

	if err := h.plugin.SubmitTask(&task); err != nil {
		h.logger.Error("Failed to submit task", zap.Error(err))
		h.metricsCollector.IncrementCounter("submit_task_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to submit task"})
		return
	}

	h.logger.Info("Transcoding task submitted", zap.String("task_id", task.ID), zap.String("file_id", task.FileID))

	// Record metrics
	h.metricsCollector.IncrementCounter("submit_task_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"task_id": task.ID,
		"status":  task.Status,
	})
}

// GetTaskStatusHandler handles task status requests
func (h *TranscoderHandler) GetTaskStatusHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_task_status_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	taskID := resolveTaskID(r, "/api/v1/transcode/status/")
	if taskID == "" {
		h.metricsCollector.IncrementCounter("get_task_status_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing task_id"})
		return
	}

	task, err := h.plugin.GetTaskStatus(taskID)
	if err != nil {
		h.logger.Error("Failed to get task status", zap.Error(err))
		h.metricsCollector.IncrementCounter("get_task_status_failed", map[string]string{})
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_task_status_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(task)
}

// CancelTaskHandler handles task cancellation
func (h *TranscoderHandler) CancelTaskHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("cancel_task_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	taskID := resolveTaskID(r, "/api/v1/transcode/cancel/")
	if taskID == "" {
		h.metricsCollector.IncrementCounter("cancel_task_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing task_id"})
		return
	}

	if err := h.plugin.CancelTask(taskID); err != nil {
		h.logger.Error("Failed to cancel task", zap.Error(err))
		h.metricsCollector.IncrementCounter("cancel_task_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to cancel task"})
		return
	}

	h.logger.Info("Transcoding task cancelled", zap.String("task_id", taskID))

	// Record metrics
	h.metricsCollector.IncrementCounter("cancel_task_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// ListTasksHandler handles task listing
func (h *TranscoderHandler) ListTasksHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("list_tasks_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	h.logger.Info("Listing transcoding tasks")

	contentID := strings.TrimSpace(r.URL.Query().Get("content_id"))
	limit := 50
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	tasks := make([]*TranscodeTask, 0)
	if h.plugin != nil && h.plugin.taskQueue != nil {
		h.plugin.taskQueue.mu.RLock()
		for _, task := range h.plugin.taskQueue.tasks {
			if contentID != "" && task.FileID != contentID && task.ID != contentID {
				continue
			}
			taskCopy := *task
			tasks = append(tasks, &taskCopy)
		}
		h.plugin.taskQueue.mu.RUnlock()

		sort.Slice(tasks, func(i, j int) bool {
			return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
		})
	}

	if offset >= len(tasks) {
		tasks = []*TranscodeTask{}
	} else {
		end := offset + limit
		if end > len(tasks) {
			end = len(tasks)
		}
		tasks = tasks[offset:end]
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("list_tasks_success", map[string]string{})
	h.metricsCollector.RecordHistogram("list_tasks_count", float64(len(tasks)), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"tasks": tasks})
}

// GetMetricsHandler handles metrics requests
func (h *TranscoderHandler) GetMetricsHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_metrics_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	metrics := h.plugin.GetMetrics()

	// Record metrics
	h.metricsCollector.IncrementCounter("get_metrics_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(metrics)
}

// ListProfilesHandler returns supported transcoding profiles.
func (h *TranscoderHandler) ListProfilesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("list_profiles_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	profiles := make([]service.TranscodingProfile, 0, len(service.DefaultProfiles))
	for _, profile := range service.DefaultProfiles {
		profiles = append(profiles, profile)
	}

	h.metricsCollector.IncrementCounter("list_profiles_success", map[string]string{})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"profiles": profiles})
}

// NotFoundHandler handles 404 requests
func (h *TranscoderHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "NOT_FOUND", "code": "NOT_FOUND", "message": "resource not found"})
}

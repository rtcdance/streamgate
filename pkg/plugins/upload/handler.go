package upload

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/security"
)

// UploadHandler handles upload requests
type UploadHandler struct {
	store            *FileStore
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
	rateLimiter      *security.RateLimiter
	auditLogger      *security.AuditLogger
}

// NewUploadHandler creates a new upload handler
func NewUploadHandler(store *FileStore, logger *zap.Logger, kernel *core.Microkernel) *UploadHandler {
	return &UploadHandler{
		store:            store,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
		rateLimiter:      security.NewRateLimiter(100, 10, time.Second, logger),
		auditLogger:      security.NewAuditLogger(logger),
	}
}

// HealthHandler handles health check requests
func (h *UploadHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
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
func (h *UploadHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// UploadHandler handles file upload requests
func (h *UploadHandler) UploadHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("upload_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("upload_rate_limit_exceeded", map[string]string{})
		h.auditLogger.LogEvent("upload", clientIP, "upload_file", "unknown", "rate_limit_exceeded", nil)
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		h.logger.Error("Failed to parse multipart form", zap.Error(err))
		h.metricsCollector.IncrementCounter("upload_parse_error", map[string]string{})
		h.auditLogger.LogEvent("upload", clientIP, "upload_file", "unknown", "parse_error", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to parse form"})
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		h.logger.Error("Failed to get file from form", zap.Error(err))
		h.metricsCollector.IncrementCounter("upload_get_file_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to get file"})
		return
	}
	defer file.Close()

	// Record metrics
	h.metricsCollector.IncrementCounter("upload_requests", map[string]string{"filename": handler.Filename})
	h.auditLogger.LogEvent("upload", clientIP, "upload_file", handler.Filename, "started", nil)

	// Read file data
	data, err := io.ReadAll(file)
	if err != nil {
		h.logger.Error("Failed to read file", zap.Error(err))
		h.metricsCollector.IncrementCounter("upload_read_error", map[string]string{})
		h.auditLogger.LogEvent("upload", clientIP, "upload_file", handler.Filename, "read_error", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read file"})
		return
	}

	// Generate file ID
	fileID := fmt.Sprintf("file_%d", len(data))

	// Upload file
	if err := h.store.UploadFile(ctx, fileID, data); err != nil {
		h.logger.Error("Failed to upload file", zap.Error(err))
		h.metricsCollector.IncrementCounter("upload_failed", map[string]string{"filename": handler.Filename})
		h.auditLogger.LogEvent("upload", clientIP, "upload_file", handler.Filename, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to upload file"})
		return
	}

	h.logger.Info("File uploaded successfully", "file_id", fileID, "filename", handler.Filename, "size", len(data))

	// Record success metrics
	h.metricsCollector.IncrementCounter("upload_success", map[string]string{"filename": handler.Filename})
	h.metricsCollector.RecordHistogram("upload_size", float64(len(data)), map[string]string{})
	h.metricsCollector.RecordTimer("upload_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("upload", clientIP, "upload_file", handler.Filename, "success", map[string]interface{}{"file_id": fileID, "size": len(data)})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"file_id":  fileID,
		"filename": handler.Filename,
		"size":     len(data),
	})
}

// UploadChunkHandler handles chunked upload requests
func (h *UploadHandler) UploadChunkHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("chunk_upload_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("chunk_upload_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	// Get upload ID and chunk index from query params
	uploadID := r.URL.Query().Get("upload_id")
	chunkIndexStr := r.URL.Query().Get("chunk_index")

	if uploadID == "" || chunkIndexStr == "" {
		h.metricsCollector.IncrementCounter("chunk_upload_missing_params", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id or chunk_index"})
		return
	}

	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil {
		h.metricsCollector.IncrementCounter("chunk_upload_invalid_index", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid chunk_index"})
		return
	}

	// Read chunk data
	data, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("Failed to read chunk", zap.Error(err))
		h.metricsCollector.IncrementCounter("chunk_upload_read_error", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read chunk"})
		return
	}

	// Upload chunk
	if err := h.store.UploadChunk(ctx, uploadID, chunkIndex, data); err != nil {
		h.logger.Error("Failed to upload chunk", zap.Error(err))
		h.metricsCollector.IncrementCounter("chunk_upload_failed", map[string]string{"upload_id": uploadID})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to upload chunk"})
		return
	}

	h.logger.Info("Chunk uploaded successfully", "upload_id", uploadID, "chunk_index", chunkIndex, "size", len(data))

	// Record metrics
	h.metricsCollector.IncrementCounter("chunk_upload_success", map[string]string{"upload_id": uploadID})
	h.metricsCollector.RecordHistogram("chunk_size", float64(len(data)), map[string]string{})
	h.metricsCollector.RecordTimer("chunk_upload_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id":   uploadID,
		"chunk_index": chunkIndex,
		"size":        len(data),
	})
}

// CompleteUploadHandler handles upload completion requests
func (h *UploadHandler) CompleteUploadHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("complete_upload_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("complete_upload_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	// Get upload ID from query params
	uploadID := r.URL.Query().Get("upload_id")
	if uploadID == "" {
		h.metricsCollector.IncrementCounter("complete_upload_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id"})
		return
	}

	// Complete upload
	fileID, err := h.store.CompleteUpload(ctx, uploadID)
	if err != nil {
		h.logger.Error("Failed to complete upload", zap.Error(err))
		h.metricsCollector.IncrementCounter("complete_upload_failed", map[string]string{"upload_id": uploadID})
		h.auditLogger.LogEvent("upload", clientIP, "complete_upload", uploadID, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to complete upload"})
		return
	}

	h.logger.Info("Upload completed successfully", "upload_id", uploadID, "file_id", fileID)

	// Record metrics
	h.metricsCollector.IncrementCounter("complete_upload_success", map[string]string{"upload_id": uploadID})
	h.metricsCollector.RecordTimer("complete_upload_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("upload", clientIP, "complete_upload", uploadID, "success", map[string]interface{}{"file_id": fileID})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id": uploadID,
		"file_id":   fileID,
	})
}

// GetUploadStatusHandler handles upload status requests
func (h *UploadHandler) GetUploadStatusHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_upload_status_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("get_upload_status_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	// Get upload ID from query params
	uploadID := r.URL.Query().Get("upload_id")
	if uploadID == "" {
		h.metricsCollector.IncrementCounter("get_upload_status_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id"})
		return
	}

	// Get upload status
	status, err := h.store.GetUploadStatus(ctx, uploadID)
	if err != nil {
		h.logger.Error("Failed to get upload status", zap.Error(err))
		h.metricsCollector.IncrementCounter("get_upload_status_failed", map[string]string{"upload_id": uploadID})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to get upload status"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_upload_status_success", map[string]string{})
	h.metricsCollector.RecordTimer("get_upload_status_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// NotFoundHandler handles 404 requests
func (h *UploadHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

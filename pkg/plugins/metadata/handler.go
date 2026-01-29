package metadata

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/optimization"
	"streamgate/pkg/security"
)

// MetadataHandler handles metadata requests
type MetadataHandler struct {
	db               *MetadataDB
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
	rateLimiter      *security.RateLimiter
	auditLogger      *security.AuditLogger
	cache            *optimization.LocalCache
}

// NewMetadataHandler creates a new metadata handler
func NewMetadataHandler(db *MetadataDB, logger *zap.Logger, kernel *core.Microkernel) *MetadataHandler {
	return &MetadataHandler{
		db:               db,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
		rateLimiter:      security.NewRateLimiter(500, 50, time.Second, logger),
		auditLogger:      security.NewAuditLogger(logger),
		cache:            optimization.NewLocalCache(10000, 30*time.Minute, logger),
	}
}

// HealthHandler handles health check requests
func (h *MetadataHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
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
func (h *MetadataHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// GetMetadataHandler handles metadata retrieval requests
func (h *MetadataHandler) GetMetadataHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_metadata_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("get_metadata_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()
	contentID := r.URL.Query().Get("content_id")

	if contentID == "" {
		h.metricsCollector.IncrementCounter("get_metadata_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing content_id"})
		return
	}

	// Check cache
	cacheKey := "metadata:" + contentID
	if cached, ok := h.cache.Get(cacheKey); ok {
		h.metricsCollector.IncrementCounter("get_metadata_cache_hit", map[string]string{})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(cached)
		return
	}

	metadata, err := h.db.GetMetadata(ctx, contentID)
	if err != nil {
		h.logger.Error("Failed to get metadata", "error", err)
		h.metricsCollector.IncrementCounter("get_metadata_failed", map[string]string{})
		h.auditLogger.LogEvent("metadata", clientIP, "get_metadata", contentID, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to get metadata"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_metadata_success", map[string]string{})
	h.metricsCollector.RecordTimer("get_metadata_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("metadata", clientIP, "get_metadata", contentID, "success", nil)

	// Cache result
	h.cache.Set(cacheKey, metadata)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(metadata)
}

// CreateMetadataHandler handles metadata creation requests
func (h *MetadataHandler) CreateMetadataHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("create_metadata_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("create_metadata_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	var metadata ContentMetadata
	if err := json.NewDecoder(r.Body).Decode(&metadata); err != nil {
		h.logger.Error("Failed to decode metadata", "error", err)
		h.metricsCollector.IncrementCounter("create_metadata_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid metadata"})
		return
	}

	if err := h.db.CreateMetadata(ctx, &metadata); err != nil {
		h.logger.Error("Failed to create metadata", "error", err)
		h.metricsCollector.IncrementCounter("create_metadata_failed", map[string]string{})
		h.auditLogger.LogEvent("metadata", clientIP, "create_metadata", metadata.ContentID, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create metadata"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("create_metadata_success", map[string]string{})
	h.metricsCollector.RecordTimer("create_metadata_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("metadata", clientIP, "create_metadata", metadata.ContentID, "success", nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(metadata)
}

// UpdateMetadataHandler handles metadata update requests
func (h *MetadataHandler) UpdateMetadataHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPut {
		h.metricsCollector.IncrementCounter("update_metadata_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("update_metadata_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	var metadata ContentMetadata
	if err := json.NewDecoder(r.Body).Decode(&metadata); err != nil {
		h.logger.Error("Failed to decode metadata", "error", err)
		h.metricsCollector.IncrementCounter("update_metadata_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid metadata"})
		return
	}

	if err := h.db.UpdateMetadata(ctx, &metadata); err != nil {
		h.logger.Error("Failed to update metadata", "error", err)
		h.metricsCollector.IncrementCounter("update_metadata_failed", map[string]string{})
		h.auditLogger.LogEvent("metadata", clientIP, "update_metadata", metadata.ContentID, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to update metadata"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("update_metadata_success", map[string]string{})
	h.metricsCollector.RecordTimer("update_metadata_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("metadata", clientIP, "update_metadata", metadata.ContentID, "success", nil)

	// Invalidate cache
	h.cache.Delete("metadata:" + metadata.ContentID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(metadata)
}

// DeleteMetadataHandler handles metadata deletion requests
func (h *MetadataHandler) DeleteMetadataHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodDelete {
		h.metricsCollector.IncrementCounter("delete_metadata_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("delete_metadata_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()
	contentID := r.URL.Query().Get("content_id")

	if contentID == "" {
		h.metricsCollector.IncrementCounter("delete_metadata_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing content_id"})
		return
	}

	if err := h.db.DeleteMetadata(ctx, contentID); err != nil {
		h.logger.Error("Failed to delete metadata", "error", err)
		h.metricsCollector.IncrementCounter("delete_metadata_failed", map[string]string{})
		h.auditLogger.LogEvent("metadata", clientIP, "delete_metadata", contentID, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to delete metadata"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("delete_metadata_success", map[string]string{})
	h.metricsCollector.RecordTimer("delete_metadata_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("metadata", clientIP, "delete_metadata", contentID, "success", nil)

	// Invalidate cache
	h.cache.Delete("metadata:" + contentID)

	w.WriteHeader(http.StatusNoContent)
}

// SearchMetadataHandler handles metadata search requests
func (h *MetadataHandler) SearchMetadataHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("search_metadata_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("search_metadata_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()
	query := r.URL.Query().Get("q")

	if query == "" {
		h.metricsCollector.IncrementCounter("search_metadata_missing_query", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing search query"})
		return
	}

	results, err := h.db.SearchMetadata(ctx, query)
	if err != nil {
		h.logger.Error("Failed to search metadata", "error", err)
		h.metricsCollector.IncrementCounter("search_metadata_failed", map[string]string{})
		h.auditLogger.LogEvent("metadata", clientIP, "search_metadata", query, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to search metadata"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("search_metadata_success", map[string]string{})
	h.metricsCollector.RecordHistogram("search_metadata_results", float64(len(results)), map[string]string{})
	h.metricsCollector.RecordTimer("search_metadata_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("metadata", clientIP, "search_metadata", query, "success", map[string]interface{}{"results": len(results)})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(results)
}

// NotFoundHandler handles 404 requests
func (h *MetadataHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

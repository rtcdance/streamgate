package cache

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
)

// CacheHandler handles cache requests
type CacheHandler struct {
	store            *CacheStore
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
}

// NewCacheHandler creates a new cache handler
func NewCacheHandler(store *CacheStore, logger *zap.Logger, kernel *core.Microkernel) *CacheHandler {
	return &CacheHandler{
		store:            store,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
	}
}

// HealthHandler handles health check requests
func (h *CacheHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
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
func (h *CacheHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// GetHandler handles cache get requests
func (h *CacheHandler) GetHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("cache_get_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	ctx := r.Context()
	key := r.URL.Query().Get("key")

	if key == "" {
		h.metricsCollector.IncrementCounter("cache_get_missing_key", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing key"})
		return
	}

	value, err := h.store.Get(ctx, key)
	if err != nil {
		h.logger.Error("Failed to get cache value", zap.Error(err))
		h.metricsCollector.IncrementCounter("cache_get_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to get value"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("cache_get_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key":   key,
		"value": value,
	})
}

// SetHandler handles cache set requests
func (h *CacheHandler) SetHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("cache_set_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	ctx := r.Context()

	var req struct {
		Key   string      `json:"key"`
		Value interface{} `json:"value"`
		TTL   int         `json:"ttl"` // in seconds
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", zap.Error(err))
		h.metricsCollector.IncrementCounter("cache_set_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	ttl := time.Duration(req.TTL) * time.Second
	if err := h.store.Set(ctx, req.Key, req.Value, ttl); err != nil {
		h.logger.Error("Failed to set cache value", zap.Error(err))
		h.metricsCollector.IncrementCounter("cache_set_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to set value"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("cache_set_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// DeleteHandler handles cache delete requests
func (h *CacheHandler) DeleteHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodDelete {
		h.metricsCollector.IncrementCounter("cache_delete_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	ctx := r.Context()
	key := r.URL.Query().Get("key")

	if key == "" {
		h.metricsCollector.IncrementCounter("cache_delete_missing_key", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing key"})
		return
	}

	if err := h.store.Delete(ctx, key); err != nil {
		h.logger.Error("Failed to delete cache value", zap.Error(err))
		h.metricsCollector.IncrementCounter("cache_delete_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to delete value"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("cache_delete_success", map[string]string{})

	w.WriteHeader(http.StatusNoContent)
}

// ClearHandler handles cache clear requests
func (h *CacheHandler) ClearHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodDelete {
		h.metricsCollector.IncrementCounter("cache_clear_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	ctx := r.Context()

	if err := h.store.Clear(ctx); err != nil {
		h.logger.Error("Failed to clear cache", zap.Error(err))
		h.metricsCollector.IncrementCounter("cache_clear_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to clear cache"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("cache_clear_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}

// StatsHandler handles cache stats requests
func (h *CacheHandler) StatsHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("cache_stats_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	ctx := r.Context()
	stats := h.store.Stats(ctx)

	// Record metrics
	h.metricsCollector.IncrementCounter("cache_stats_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stats)
}

// NotFoundHandler handles 404 requests
func (h *CacheHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/optimization"
	"streamgate/pkg/security"
)

// Handler handles HTTP requests
type Handler struct {
	kernel           *core.Microkernel
	logger           *zap.Logger
	metricsCollector *monitoring.MetricsCollector
	alertManager     *monitoring.AlertManager
	rateLimiter      *security.RateLimiter
	auditLogger      *security.AuditLogger
	cache            *optimization.LocalCache
}

// NewHandler creates a new HTTP handler
func NewHandler(kernel *core.Microkernel, logger *zap.Logger) *Handler {
	return &Handler{
		kernel:           kernel,
		logger:           logger,
		metricsCollector: monitoring.NewMetricsCollector(logger),
		alertManager:     monitoring.NewAlertManager(logger),
		rateLimiter:      security.NewRateLimiter(1000, 100, time.Second, logger),
		auditLogger:      security.NewAuditLogger(logger),
		cache:            optimization.NewLocalCache(10000, 5*time.Minute, logger),
	}
}

// HealthHandler handles health check requests
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	// Get client IP for rate limiting
	clientIP := r.RemoteAddr

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("rate_limit_exceeded", map[string]string{"endpoint": "/health"})
		h.auditLogger.LogEvent("rate_limit", clientIP, "health_check", "health", "denied", nil)
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	// Check cache
	if cached, ok := h.cache.Get("health_status"); ok {
		h.metricsCollector.IncrementCounter("cache_hit", map[string]string{"endpoint": "/health"})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(cached)
		return
	}

	if err := h.kernel.Health(ctx); err != nil {
		h.logger.Error("Health check failed", "error", err)
		h.metricsCollector.IncrementCounter("health_check_failed", map[string]string{})
		h.auditLogger.LogEvent("health_check", clientIP, "health_check", "health", "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}

	// Record metrics
	latency := time.Since(startTime).Milliseconds()
	h.metricsCollector.RecordTimer("request_latency", time.Since(startTime), map[string]string{"endpoint": "/health"})
	h.metricsCollector.IncrementCounter("health_check_success", map[string]string{})
	h.auditLogger.LogEvent("health_check", clientIP, "health_check", "health", "success", nil)

	// Cache result
	h.cache.Set("health_status", map[string]string{"status": "healthy"})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy", "latency_ms": latency})
}

// ReadyHandler handles readiness check requests
func (h *Handler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("rate_limit_exceeded", map[string]string{"endpoint": "/ready"})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	h.metricsCollector.RecordTimer("request_latency", time.Since(startTime), map[string]string{"endpoint": "/ready"})
	h.metricsCollector.IncrementCounter("ready_check", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// NotFoundHandler handles 404 requests
func (h *Handler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	clientIP := r.RemoteAddr

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("rate_limit_exceeded", map[string]string{"endpoint": "404"})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	h.metricsCollector.IncrementCounter("not_found", map[string]string{"path": r.URL.Path})
	h.auditLogger.LogEvent("api_request", clientIP, "not_found", r.URL.Path, "404", nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

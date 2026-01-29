package api

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
)

// Handler handles HTTP requests
type Handler struct {
	kernel           *core.Microkernel
	logger           *zap.Logger
	metricsCollector *monitoring.MetricsCollector
	alertManager     *monitoring.AlertManager
}

// NewHandler creates a new HTTP handler
func NewHandler(kernel *core.Microkernel, logger *zap.Logger) *Handler {
	return &Handler{
		kernel:           kernel,
		logger:           logger,
		metricsCollector: monitoring.NewMetricsCollector(logger),
		alertManager:     monitoring.NewAlertManager(logger),
	}
}

// HealthHandler handles health check requests
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.kernel.Health(ctx); err != nil {
		h.logger.Error("Health check failed", zap.Error(err))
		h.metricsCollector.IncrementCounter("health_check_failed", map[string]string{})
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("health_check_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy"})
}

// ReadyHandler handles readiness check requests
func (h *Handler) ReadyHandler(w http.ResponseWriter, r *http.Request) {

	// Check rate limit
	h.metricsCollector.IncrementCounter("ready_check", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// NotFoundHandler handles 404 requests
func (h *Handler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {

	// Check rate limit

	h.metricsCollector.IncrementCounter("not_found", map[string]string{"path": r.URL.Path})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

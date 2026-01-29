package monitor

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
)

// MonitorHandler handles monitoring requests
type MonitorHandler struct {
	collector        *MetricsCollector
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
}

// NewMonitorHandler creates a new monitor handler
func NewMonitorHandler(collector *MetricsCollector, logger *zap.Logger, kernel *core.Microkernel) *MonitorHandler {
	return &MonitorHandler{
		collector:        collector,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
	}
}

// HealthHandler handles health check requests
func (h *MonitorHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
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
func (h *MonitorHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// GetHealthHandler handles health status requests
func (h *MonitorHandler) GetHealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_health_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	health := h.collector.GetHealth()

	// Record metrics
	h.metricsCollector.IncrementCounter("get_health_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(health)
}

// GetMetricsHandler handles metrics requests
func (h *MonitorHandler) GetMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_metrics_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	metrics := h.collector.GetMetrics()

	// Record metrics
	h.metricsCollector.IncrementCounter("get_metrics_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(metrics)
}

// GetAlertsHandler handles alert requests
func (h *MonitorHandler) GetAlertsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_alerts_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	h.logger.Info("Getting alerts")

	// TODO: Retrieve alerts from storage
	alerts := []*Alert{}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_alerts_success", map[string]string{})
	h.metricsCollector.RecordHistogram("get_alerts_count", float64(len(alerts)), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(alerts)
}

// GetLogsHandler handles log requests
func (h *MonitorHandler) GetLogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("get_logs_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	h.logger.Info("Getting logs")

	// TODO: Retrieve logs from storage
	logs := []map[string]interface{}{}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_logs_success", map[string]string{})
	h.metricsCollector.RecordHistogram("get_logs_count", float64(len(logs)), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(logs)
}

// PrometheusMetricsHandler handles Prometheus metrics requests
func (h *MonitorHandler) PrometheusMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("prometheus_metrics_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	h.logger.Info("Serving Prometheus metrics")

	// TODO: Generate Prometheus metrics format
	// - CPU usage
	// - Memory usage
	// - Request count
	// - Error count
	// - Response time

	// Record metrics
	h.metricsCollector.IncrementCounter("prometheus_metrics_success", map[string]string{})

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("# HELP streamgate_requests_total Total requests\n"))
	w.Write([]byte("# TYPE streamgate_requests_total counter\n"))
	w.Write([]byte("streamgate_requests_total 0\n"))
}

// NotFoundHandler handles 404 requests
func (h *MonitorHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

package monitoring

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PrometheusExporter exports metrics in Prometheus format
type PrometheusExporter struct {
	logger     *zap.Logger
	collector  *MetricsCollector
	svcTracker *ServiceMetricsTracker
	mu         sync.RWMutex
	lastExport time.Time
}

// NewPrometheusExporter creates a new Prometheus exporter
func NewPrometheusExporter(collector *MetricsCollector, svcTracker *ServiceMetricsTracker, logger *zap.Logger) *PrometheusExporter {
	return &PrometheusExporter{
		logger:     logger,
		collector:  collector,
		svcTracker: svcTracker,
		lastExport: time.Now(),
	}
}

// Export exports metrics in Prometheus format
func (pe *PrometheusExporter) Export() string {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	output := ""

	// Export counter metrics
	output += pe.exportCounterMetrics()

	// Export gauge metrics
	output += pe.exportGaugeMetrics()

	// Export histogram metrics
	output += pe.exportHistogramMetrics()

	// Export service metrics
	output += pe.exportServiceMetrics()

	pe.lastExport = time.Now()
	return output
}

// exportCounterMetrics exports counter metrics
func (pe *PrometheusExporter) exportCounterMetrics() string {
	output := "# HELP streamgate_requests_total Total requests\n"
	output += "# TYPE streamgate_requests_total counter\n"

	metrics := pe.collector.GetAllMetrics()
	for _, metric := range metrics {
		if metric.Type == "counter" {
			labels := pe.formatLabels(metric.Tags)
			output += fmt.Sprintf("streamgate_requests_total{%s} %d\n", labels, int64(metric.Value))
		}
	}

	return output
}

// exportGaugeMetrics exports gauge metrics
func (pe *PrometheusExporter) exportGaugeMetrics() string {
	output := "# HELP streamgate_gauge_value Gauge value\n"
	output += "# TYPE streamgate_gauge_value gauge\n"

	metrics := pe.collector.GetAllMetrics()
	for _, metric := range metrics {
		if metric.Type == "gauge" {
			labels := pe.formatLabels(metric.Tags)
			output += fmt.Sprintf("streamgate_gauge_value{%s} %.2f\n", labels, metric.Value)
		}
	}

	return output
}

// exportHistogramMetrics exports histogram metrics
func (pe *PrometheusExporter) exportHistogramMetrics() string {
	output := "# HELP streamgate_histogram_value Histogram value\n"
	output += "# TYPE streamgate_histogram_value histogram\n"

	metrics := pe.collector.GetAllMetrics()
	for _, metric := range metrics {
		if metric.Type == "histogram" {
			labels := pe.formatLabels(metric.Tags)
			output += fmt.Sprintf("streamgate_histogram_value_sum{%s} %.2f\n", labels, metric.Sum)
			output += fmt.Sprintf("streamgate_histogram_value_count{%s} %d\n", labels, metric.Count)
			output += fmt.Sprintf("streamgate_histogram_value_bucket{%s,le=\"+Inf\"} %d\n", labels, metric.Count)
		}
	}

	return output
}

// exportServiceMetrics exports service-specific metrics
func (pe *PrometheusExporter) exportServiceMetrics() string {
	output := "# HELP streamgate_service_requests Service request count\n"
	output += "# TYPE streamgate_service_requests counter\n"

	svcMetrics := pe.svcTracker.GetAllServiceMetrics()
	for svcName, metrics := range svcMetrics {
		output += fmt.Sprintf("streamgate_service_requests{service=\"%s\"} %d\n", svcName, metrics.RequestCount)
		output += fmt.Sprintf("streamgate_service_errors{service=\"%s\"} %d\n", svcName, metrics.ErrorCount)
		output += fmt.Sprintf("streamgate_service_success{service=\"%s\"} %d\n", svcName, metrics.SuccessCount)
		output += fmt.Sprintf("streamgate_service_latency_avg{service=\"%s\"} %.2f\n", svcName, metrics.AverageLatency)
		output += fmt.Sprintf("streamgate_service_latency_min{service=\"%s\"} %d\n", svcName, metrics.MinLatency)
		output += fmt.Sprintf("streamgate_service_latency_max{service=\"%s\"} %d\n", svcName, metrics.MaxLatency)
	}

	return output
}

// formatLabels formats tags as Prometheus labels
func (pe *PrometheusExporter) formatLabels(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}

	labels := ""
	for key, value := range tags {
		if labels != "" {
			labels += ","
		}
		labels += fmt.Sprintf("%s=\"%s\"", key, value)
	}

	return labels
}

// GetMetricsSnapshot returns a snapshot of all metrics
func (pe *PrometheusExporter) GetMetricsSnapshot() map[string]interface{} {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	snapshot := make(map[string]interface{})
	snapshot["timestamp"] = time.Now()
	snapshot["last_export"] = pe.lastExport
	snapshot["uptime"] = time.Since(pe.lastExport).Seconds()

	// Add collector metrics
	snapshot["metrics"] = pe.collector.GetMetricsSnapshot()

	// Add service metrics
	svcMetrics := pe.svcTracker.GetAllServiceMetrics()
	svcSnapshot := make(map[string]interface{})
	for svcName, metrics := range svcMetrics {
		svcSnapshot[svcName] = map[string]interface{}{
			"request_count":   metrics.RequestCount,
			"error_count":     metrics.ErrorCount,
			"success_count":   metrics.SuccessCount,
			"average_latency": metrics.AverageLatency,
			"min_latency":     metrics.MinLatency,
			"max_latency":     metrics.MaxLatency,
			"error_rate":      pe.svcTracker.GetErrorRate(svcName),
			"success_rate":    pe.svcTracker.GetSuccessRate(svcName),
		}
	}
	snapshot["services"] = svcSnapshot

	return snapshot
}

// PrometheusMetricsHandler handles Prometheus metrics requests
type PrometheusMetricsHandler struct {
	exporter *PrometheusExporter
	logger   *zap.Logger
}

// NewPrometheusMetricsHandler creates a new Prometheus metrics handler
func NewPrometheusMetricsHandler(exporter *PrometheusExporter, logger *zap.Logger) *PrometheusMetricsHandler {
	return &PrometheusMetricsHandler{
		exporter: exporter,
		logger:   logger,
	}
}

// ServeMetrics serves Prometheus metrics
func (pmh *PrometheusMetricsHandler) ServeMetrics() string {
	pmh.logger.Debug("Exporting Prometheus metrics")
	return pmh.exporter.Export()
}

// GetSnapshot returns a metrics snapshot
func (pmh *PrometheusMetricsHandler) GetSnapshot() map[string]interface{} {
	return pmh.exporter.GetMetricsSnapshot()
}

// MetricsRegistry manages multiple metric exporters
type MetricsRegistry struct {
	logger    *zap.Logger
	exporters map[string]*PrometheusExporter
	mu        sync.RWMutex
}

// NewMetricsRegistry creates a new metrics registry
func NewMetricsRegistry(logger *zap.Logger) *MetricsRegistry {
	return &MetricsRegistry{
		logger:    logger,
		exporters: make(map[string]*PrometheusExporter),
	}
}

// RegisterExporter registers a metrics exporter
func (mr *MetricsRegistry) RegisterExporter(name string, exporter *PrometheusExporter) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	mr.exporters[name] = exporter
	mr.logger.Debug("Metrics exporter registered", zap.String("name", name))
}

// GetExporter gets a metrics exporter
func (mr *MetricsRegistry) GetExporter(name string) *PrometheusExporter {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	return mr.exporters[name]
}

// ExportAll exports metrics from all exporters
func (mr *MetricsRegistry) ExportAll() map[string]string {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	result := make(map[string]string)
	for name, exporter := range mr.exporters {
		result[name] = exporter.Export()
	}

	return result
}

// GetAllSnapshots returns snapshots from all exporters
func (mr *MetricsRegistry) GetAllSnapshots() map[string]map[string]interface{} {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	result := make(map[string]map[string]interface{})
	for name, exporter := range mr.exporters {
		result[name] = exporter.GetMetricsSnapshot()
	}

	return result
}

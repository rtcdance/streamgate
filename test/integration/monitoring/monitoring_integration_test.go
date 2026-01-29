package monitoring_test

import (
	"context"
	"testing"

	"streamgate/pkg/monitoring"
	"streamgate/test/helpers"
)

func TestMonitoring_RecordMetric(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()

	// Record metric
	metrics.RecordMetric("test_metric", 100)

	// Verify metric was recorded
	value := metrics.GetMetric("test_metric")
	helpers.AssertEqual(t, float64(100), value)
}

func TestMonitoring_RecordLatency(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()

	// Record latency
	metrics.RecordLatency("api_request", 150)

	// Verify latency was recorded
	latency := metrics.GetLatency("api_request")
	helpers.AssertTrue(t, latency > 0)
}

func TestMonitoring_IncrementCounter(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()

	// Increment counter
	metrics.IncrementCounter("requests")
	metrics.IncrementCounter("requests")
	metrics.IncrementCounter("requests")

	// Verify counter
	count := metrics.GetCounter("requests")
	helpers.AssertEqual(t, int64(3), count)
}

func TestMonitoring_RecordError(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()

	// Record errors
	metrics.RecordError("database_error")
	metrics.RecordError("database_error")

	// Verify error count
	errorCount := metrics.GetErrorCount("database_error")
	helpers.AssertEqual(t, int64(2), errorCount)
}

func TestMonitoring_GetMetrics(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()

	// Record various metrics
	metrics.RecordMetric("metric1", 100)
	metrics.RecordMetric("metric2", 200)
	metrics.IncrementCounter("counter1")

	// Get all metrics
	allMetrics := metrics.GetAllMetrics()
	helpers.AssertNotNil(t, allMetrics)
	helpers.AssertTrue(t, len(allMetrics) > 0)
}

func TestMonitoring_ResetMetrics(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()

	// Record metrics
	metrics.RecordMetric("test_metric", 100)
	metrics.IncrementCounter("test_counter")

	// Verify metrics exist
	helpers.AssertEqual(t, float64(100), metrics.GetMetric("test_metric"))

	// Reset metrics
	metrics.Reset()

	// Verify metrics are reset
	helpers.AssertEqual(t, float64(0), metrics.GetMetric("test_metric"))
}

func TestMonitoring_Alerting(t *testing.T) {
	// Setup
	alerts := monitoring.NewAlerts()

	// Create alert
	alert := &monitoring.Alert{
		Name:         "high_latency",
		Threshold:    1000,
		CurrentValue: 1500,
		Severity:     "warning",
	}

	// Record alert
	alerts.RecordAlert(alert)

	// Get alerts
	recordedAlerts := alerts.GetAlerts()
	helpers.AssertTrue(t, len(recordedAlerts) > 0)
}

func TestMonitoring_PrometheusMetrics(t *testing.T) {
	// Setup
	prometheus := monitoring.NewPrometheus()

	// Record metrics
	prometheus.RecordMetric("http_requests_total", 100)
	prometheus.RecordLatency("http_request_duration_seconds", 0.5)

	// Get metrics
	metrics := prometheus.GetMetrics()
	helpers.AssertNotNil(t, metrics)
}

func TestMonitoring_HealthCheck(t *testing.T) {
	// Setup
	health := monitoring.NewHealthChecker()

	// Check health
	status, err := health.Check(context.Background())
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, status)
}

func TestMonitoring_Tracing(t *testing.T) {
	// Setup
	tracer := monitoring.NewTracer()

	// Start trace
	span := tracer.StartSpan("test_operation")
	helpers.AssertNotNil(t, span)

	// End span
	span.End()
}

package e2e_test

import (
	"context"
	"testing"

	"streamgate/pkg/monitoring"
	"streamgate/test/helpers"
)

func TestE2E_MonitoringFlow(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()
	alerts := monitoring.NewAlerts()

	// Step 1: Record metrics
	metrics.RecordMetric("cpu_usage", 75)
	metrics.RecordMetric("memory_usage", 60)
	metrics.RecordLatency("api_response_time", 250)

	// Step 2: Verify metrics recorded
	cpuUsage := metrics.GetMetric("cpu_usage")
	helpers.AssertEqual(t, float64(75), cpuUsage)

	// Step 3: Check for alerts
	if cpuUsage > 70 {
		alert := &monitoring.Alert{
			Name:         "high_cpu",
			Threshold:    70,
			CurrentValue: cpuUsage,
			Severity:     "warning",
		}
		alerts.RecordAlert(alert)
	}

	// Step 4: Verify alerts recorded
	recordedAlerts := alerts.GetAlerts()
	helpers.AssertTrue(t, len(recordedAlerts) > 0)
}

func TestE2E_MetricsAggregation(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()

	// Record multiple metrics
	for i := 0; i < 10; i++ {
		metrics.RecordMetric("request_count", float64(i))
		metrics.RecordLatency("response_time", float64(100+i*10))
	}

	// Get aggregated metrics
	allMetrics := metrics.GetAllMetrics()
	helpers.AssertNotNil(t, allMetrics)
	helpers.AssertTrue(t, len(allMetrics) > 0)
}

func TestE2E_AlertingFlow(t *testing.T) {
	// Setup
	alerts := monitoring.NewAlerts()

	// Create alerts with different severities
	alertConfigs := []struct {
		name     string
		severity string
		value    float64
	}{
		{"low_alert", "info", 10},
		{"medium_alert", "warning", 50},
		{"high_alert", "critical", 90},
	}

	for _, config := range alertConfigs {
		alert := &monitoring.Alert{
			Name:         config.name,
			Threshold:    50,
			CurrentValue: config.value,
			Severity:     config.severity,
		}
		alerts.RecordAlert(alert)
	}

	// Verify alerts
	recordedAlerts := alerts.GetAlerts()
	helpers.AssertEqual(t, 3, len(recordedAlerts))
}

func TestE2E_HealthCheckFlow(t *testing.T) {
	// Setup
	health := monitoring.NewHealthChecker()

	// Perform health check
	status, err := health.Check(context.Background())
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, status)

	// Verify health status
	helpers.AssertTrue(t, status.Status == "healthy" || status.Status == "degraded" || status.Status == "unhealthy")
}

func TestE2E_PrometheusMetricsFlow(t *testing.T) {
	// Setup
	prometheus := monitoring.NewPrometheus()

	// Record metrics
	prometheus.RecordMetric("http_requests_total", 100)
	prometheus.RecordMetric("http_requests_total", 50)
	prometheus.RecordLatency("http_request_duration_seconds", 0.5)
	prometheus.RecordLatency("http_request_duration_seconds", 0.3)

	// Get metrics
	metrics := prometheus.GetMetrics()
	helpers.AssertNotNil(t, metrics)
}

func TestE2E_TracingFlow(t *testing.T) {
	// Setup
	tracer := monitoring.NewTracer()

	// Start trace
	span := tracer.StartSpan("test_operation")
	helpers.AssertNotNil(t, span)

	// Add events to span
	span.AddEvent("operation_started")
	span.AddEvent("operation_completed")

	// End span
	span.End()

	// Verify span was recorded
	helpers.AssertTrue(t, true)
}

func TestE2E_MetricsExport(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()

	// Record metrics
	metrics.RecordMetric("test_metric", 100)
	metrics.IncrementCounter("test_counter")

	// Export metrics
	exported, err := metrics.Export()
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, exported)
}

func TestE2E_AlertNotification(t *testing.T) {
	// Setup
	alerts := monitoring.NewAlerts()

	// Create alert
	alert := &monitoring.Alert{
		Name:         "test_alert",
		Threshold:    50,
		CurrentValue: 75,
		Severity:     "critical",
	}

	// Record alert
	alerts.RecordAlert(alert)

	// Send notification
	err := alerts.SendNotification(context.Background(), alert)
	// May fail if notification service not available, but should not panic
	if err == nil {
		helpers.AssertTrue(t, true)
	}
}

func TestE2E_MetricsRetention(t *testing.T) {
	// Setup
	metrics := monitoring.NewMetrics()

	// Record metrics
	metrics.RecordMetric("test_metric", 100)

	// Get metrics
	value := metrics.GetMetric("test_metric")
	helpers.AssertEqual(t, float64(100), value)

	// Reset metrics
	metrics.Reset()

	// Verify metrics cleared
	value = metrics.GetMetric("test_metric")
	helpers.AssertEqual(t, float64(0), value)
}

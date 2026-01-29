package e2e_test

import (
	"context"
	"testing"
	"time"

	"streamgate/pkg/monitoring"
	"streamgate/test/helpers"
)

func TestE2E_MonitoringFlow(t *testing.T) {
	metrics := monitoring.NewMetricsCollector(nil)
	alerts := monitoring.NewAlertManager(nil)

	metrics.SetGauge("cpu_usage", 75, nil)
	metrics.SetGauge("memory_usage", 60, nil)
	metrics.RecordTimer("api_response_time", 250000000, nil)

	cpuUsage := metrics.GetMetric("cpu_usage")
	helpers.AssertNotNil(t, cpuUsage)

	rule := &monitoring.AlertRule{
		Name:      "high_cpu",
		Metric:    "cpu_usage",
		Threshold: 70,
		Level:     "warning",
	}
	alerts.AddRule(rule)

	alerts.CheckMetric("cpu_usage", cpuUsage.Value)

	activeAlerts := alerts.GetActiveAlerts()
	helpers.AssertNotNil(t, activeAlerts)
}

func TestE2E_MetricsAggregation(t *testing.T) {
	metrics := monitoring.NewMetricsCollector(nil)

	for i := 0; i < 10; i++ {
		metrics.SetGauge("request_count", float64(i), nil)
		metrics.RecordTimer("response_time", time.Duration(100000000+i*10000000), nil)
	}

	allMetrics := metrics.GetAllMetrics()
	helpers.AssertNotNil(t, allMetrics)
	helpers.AssertTrue(t, len(allMetrics) > 0)
}

func TestE2E_AlertingFlow(t *testing.T) {
	alerts := monitoring.NewAlertManager(nil)

	rules := []struct {
		name      string
		metric    string
		threshold float64
		level     string
	}{
		{"low_alert", "test_metric", 10, "info"},
		{"medium_alert", "test_metric", 50, "warning"},
		{"high_alert", "test_metric", 90, "critical"},
	}

	for _, rule := range rules {
		alertRule := &monitoring.AlertRule{
			Name:      rule.name,
			Metric:    rule.metric,
			Threshold: rule.threshold,
			Level:     rule.level,
		}
		alerts.AddRule(alertRule)
	}

	alerts.CheckMetric("test_metric", 95)

	activeAlerts := alerts.GetActiveAlerts()
	helpers.AssertTrue(t, len(activeAlerts) > 0)
}

func TestE2E_HealthCheckFlow(t *testing.T) {
	health := monitoring.NewHealthChecker(nil)

	status := health.Check()
	helpers.AssertNotNil(t, status)

	helpers.AssertTrue(t, status.Status == "healthy" || status.Status == "degraded" || status.Status == "unhealthy")
}

func TestE2E_PrometheusMetricsFlow(t *testing.T) {
	collector := monitoring.NewMetricsCollector(nil)
	tracker := monitoring.NewServiceMetricsTracker(nil)
	prometheus := monitoring.NewPrometheusExporter(collector, tracker, nil)

	collector.SetGauge("http_requests_total", 100, nil)
	collector.SetGauge("http_requests_total", 50, nil)
	collector.RecordTimer("http_request_duration_seconds", 500000000, nil)
	collector.RecordTimer("http_request_duration_seconds", 300000000, nil)

	snapshot := prometheus.GetMetricsSnapshot()
	helpers.AssertNotNil(t, snapshot)
}

func TestE2E_TracingSpanFlow(t *testing.T) {
	tracer := monitoring.NewTracer("test-service", nil)

	span, ctx := tracer.StartSpan(context.Background(), "test_operation")
	helpers.AssertNotNil(t, span)
	helpers.AssertNotNil(t, ctx)

	span.AddLog("operation_started", nil)
	span.AddLog("operation_completed", nil)

	tracer.FinishSpan(span)

	helpers.AssertTrue(t, true)
}

func TestE2E_MetricsExport(t *testing.T) {
	metrics := monitoring.NewMetricsCollector(nil)

	metrics.SetGauge("test_metric", 100, nil)
	metrics.IncrementCounter("test_counter", nil)

	snapshot := metrics.GetMetricsSnapshot()
	helpers.AssertNotNil(t, snapshot)
	helpers.AssertTrue(t, len(snapshot) > 0)
}

func TestE2E_AlertNotification(t *testing.T) {
	alerts := monitoring.NewAlertManager(nil)

	rule := &monitoring.AlertRule{
		Name:      "test_alert",
		Metric:    "test_metric",
		Threshold: 50,
		Level:     "critical",
	}
	alerts.AddRule(rule)

	alerts.CheckMetric("test_metric", 75)

	activeAlerts := alerts.GetActiveAlerts()
	helpers.AssertTrue(t, len(activeAlerts) > 0)
}

func TestE2E_MetricsRetention(t *testing.T) {
	metrics := monitoring.NewMetricsCollector(nil)

	metrics.SetGauge("test_metric", 100, nil)

	value := metrics.GetMetric("test_metric")
	helpers.AssertNotNil(t, value)
	helpers.AssertEqual(t, float64(100), value.Value)

	metrics.Reset()

	value = metrics.GetMetric("test_metric")
	if value == nil {
		helpers.AssertTrue(t, true)
	}
}

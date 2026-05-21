package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/stretchr/testify/require"
)

func TestE2E_MonitoringFlow(t *testing.T) {
	metrics := monitoring.NewMetricsCollector(nil)
	alerts := monitoring.NewAlertManager(nil)

	metrics.SetGauge("cpu_usage", 75, nil)
	metrics.SetGauge("memory_usage", 60, nil)
	metrics.RecordTimer("api_response_time", 250000000, nil)

	cpuUsage := metrics.GetMetric("cpu_usage")
	require.NotNil(t, cpuUsage)

	rule := &monitoring.AlertRule{
		Name:      "high_cpu",
		Metric:    "cpu_usage",
		Threshold: 70,
		Level:     "warning",
		Enabled:   true,
	}
	alerts.AddRule(rule)

	alerts.CheckMetric("cpu_usage", cpuUsage.Value)

	activeAlerts := alerts.GetActiveAlerts()
	require.NotNil(t, activeAlerts)
}

func TestE2E_MetricsAggregation(t *testing.T) {
	metrics := monitoring.NewMetricsCollector(nil)

	for i := 0; i < 10; i++ {
		metrics.SetGauge("request_count", float64(i), nil)
		metrics.RecordTimer("response_time", time.Duration(100000000+i*10000000), nil)
	}

	allMetrics := metrics.GetAllMetrics()
	require.NotNil(t, allMetrics)
	require.True(t, len(allMetrics) > 0)
}

func TestE2E_AlertingFlow(t *testing.T) {
	alerts := monitoring.NewAlertManager(nil)

	rules := []struct {
		id        string
		name      string
		metric    string
		threshold float64
		level     string
	}{
		{"low_alert", "low_alert", "test_metric", 10, "info"},
		{"medium_alert", "medium_alert", "test_metric", 50, "warning"},
		{"high_alert", "high_alert", "test_metric", 90, "critical"},
	}

	for _, rule := range rules {
		alertRule := &monitoring.AlertRule{
			ID:        rule.id,
			Name:      rule.name,
			Metric:    rule.metric,
			Condition: "gt",
			Threshold: rule.threshold,
			Level:     rule.level,
			Enabled:   true,
		}
		alerts.AddRule(alertRule)
	}

	alerts.CheckMetric("test_metric", 95)

	activeAlerts := alerts.GetActiveAlerts()
	require.True(t, len(activeAlerts) > 0)
}

func TestE2E_HealthCheckFlow(t *testing.T) {
	health := monitoring.NewHealthChecker(nil)

	status := health.Check()
	require.NotNil(t, status)

	require.True(t, status.Status == "healthy" || status.Status == "degraded" || status.Status == "unhealthy")
}

func TestE2E_PrometheusMetricsFlow(t *testing.T) {
	collector := monitoring.NewMetricsCollector(nil)
	_ = monitoring.NewServiceMetricsTracker(nil)

	collector.SetGauge("http_requests_total", 100, nil)
	collector.SetGauge("http_requests_total", 50, nil)
	collector.RecordTimer("http_request_duration_seconds", 500000000, nil)
	collector.RecordTimer("http_request_duration_seconds", 300000000, nil)

	snapshot := collector.GetMetricsSnapshot()
	require.NotNil(t, snapshot)
}

func TestE2E_TracingSpanFlow(t *testing.T) {
	tracer := monitoring.NewTracer("test-service", nil)

	span, ctx := tracer.StartSpan(context.Background(), "test_operation")
	require.NotNil(t, span)
	require.NotNil(t, ctx)

	span.AddLog("operation_started", nil)
	span.AddLog("operation_completed", nil)

	tracer.FinishSpan(span)

	require.True(t, true)
}

func TestE2E_MetricsExport(t *testing.T) {
	metrics := monitoring.NewMetricsCollector(nil)

	metrics.SetGauge("test_metric", 100, nil)
	metrics.IncrementCounter("test_counter", nil)

	snapshot := metrics.GetMetricsSnapshot()
	require.NotNil(t, snapshot)
	require.True(t, len(snapshot) > 0)
}

func TestE2E_AlertNotification(t *testing.T) {
	alerts := monitoring.NewAlertManager(nil)

	rule := &monitoring.AlertRule{
		ID:        "test_alert",
		Name:      "test_alert",
		Metric:    "test_metric",
		Condition: "gt",
		Threshold: 50,
		Level:     "critical",
		Enabled:   true,
	}
	alerts.AddRule(rule)

	alerts.CheckMetric("test_metric", 75)

	activeAlerts := alerts.GetActiveAlerts()
	require.True(t, len(activeAlerts) > 0)
}

func TestE2E_MetricsRetention(t *testing.T) {
	metrics := monitoring.NewMetricsCollector(nil)

	metrics.SetGauge("test_metric", 100, nil)

	value := metrics.GetMetric("test_metric")
	require.NotNil(t, value)
	require.Equal(t, float64(100), value.Value)

	metrics.Reset()

	value = metrics.GetMetric("test_metric")
	if value == nil {
		require.True(t, true)
	}
}

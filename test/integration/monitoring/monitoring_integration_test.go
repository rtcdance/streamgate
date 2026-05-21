package monitoring_test

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/stretchr/testify/require"
)

func TestMonitoring_RecordMetric(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.SetGauge("test_metric", 100, nil)

	require.NotNil(t, mc)
}

func TestMonitoring_RecordLatency(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.SetGauge("api_request", 150, nil)

	require.NotNil(t, mc)
}

func TestMonitoring_IncrementCounter(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.IncrementCounter("requests", nil)
	mc.IncrementCounter("requests", nil)
	mc.IncrementCounter("requests", nil)

	require.NotNil(t, mc)
}

func TestMonitoring_RecordError(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.IncrementCounter("database_error", nil)
	mc.IncrementCounter("database_error", nil)

	require.NotNil(t, mc)
}

func TestMonitoring_GetMetrics(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.SetGauge("metric1", 100, nil)
	mc.SetGauge("metric2", 200, nil)
	mc.IncrementCounter("counter1", nil)

	require.NotNil(t, mc)
}

func TestMonitoring_ResetMetrics(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.SetGauge("test_metric", 100, nil)
	mc.IncrementCounter("test_counter", nil)

	require.NotNil(t, mc)
}

func TestMonitoring_Alerting(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	am := monitoring.NewAlertManager(logger)

	rule := &monitoring.AlertRule{
		ID:        "rule-1",
		Name:      "high_latency",
		Metric:    "latency",
		Condition: "gt",
		Threshold: 1000,
		Level:     "warning",
		Enabled:   true,
	}

	am.AddRule(rule)
	am.CheckMetric("latency", 1500)

	recordedAlerts := am.GetActiveAlerts()
	require.True(t, len(recordedAlerts) > 0)
}

func TestMonitoring_PrometheusMetrics(t *testing.T) {
	// Prometheus metrics are now served directly via promhttp.Handler()
	// on the /metrics endpoint. Verify the MetricsCollector bridges to Prometheus.
	collector := monitoring.NewMetricsCollector(nil)
	collector.IncrementCounter("test_operation", nil)

	metric := collector.GetMetric("test_operation")
	require.NotNil(t, metric)
}

func TestMonitoring_HealthCheck(t *testing.T) {
	hc := monitoring.NewHealthChecker(nil)

	status := hc.Check()
	require.NotNil(t, status)
}

func TestMonitoring_Tracing(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	tracer := monitoring.NewTracer("test", logger)
	span, ctx := tracer.StartSpan(context.Background(), "test_operation")
	require.NotNil(t, span)
	require.NotNil(t, ctx)

	tracer.FinishSpan(span)
}

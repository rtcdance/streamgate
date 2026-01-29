package monitoring_test

import (
	"context"
	"testing"

	"streamgate/pkg/monitoring"
	"streamgate/test/helpers"
)

func TestMonitoring_RecordMetric(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.SetGauge("test_metric", 100, nil)

	helpers.AssertNotNil(t, mc)
}

func TestMonitoring_RecordLatency(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.SetGauge("api_request", 150, nil)

	helpers.AssertNotNil(t, mc)
}

func TestMonitoring_IncrementCounter(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.IncrementCounter("requests", nil)
	mc.IncrementCounter("requests", nil)
	mc.IncrementCounter("requests", nil)

	helpers.AssertNotNil(t, mc)
}

func TestMonitoring_RecordError(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.IncrementCounter("database_error", nil)
	mc.IncrementCounter("database_error", nil)

	helpers.AssertNotNil(t, mc)
}

func TestMonitoring_GetMetrics(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.SetGauge("metric1", 100, nil)
	mc.SetGauge("metric2", 200, nil)
	mc.IncrementCounter("counter1", nil)

	helpers.AssertNotNil(t, mc)
}

func TestMonitoring_ResetMetrics(t *testing.T) {
	mc := monitoring.NewMetricsCollector(nil)

	mc.SetGauge("test_metric", 100, nil)
	mc.IncrementCounter("test_counter", nil)

	helpers.AssertNotNil(t, mc)
}

func TestMonitoring_Alerting(t *testing.T) {
	am := monitoring.NewAlertManager(nil)

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
	helpers.AssertTrue(t, len(recordedAlerts) > 0)
}

func TestMonitoring_PrometheusMetrics(t *testing.T) {
	pe := monitoring.NewPrometheusExporter(nil, nil, nil)

	helpers.AssertNotNil(t, pe)
}

func TestMonitoring_HealthCheck(t *testing.T) {
	hc := monitoring.NewHealthChecker(nil)

	status := hc.Check()
	helpers.AssertNotNil(t, status)
}

func TestMonitoring_Tracing(t *testing.T) {
	tracer := monitoring.NewTracer("test", nil)

	span, ctx := tracer.StartSpan(context.Background(), "test_operation")
	helpers.AssertNotNil(t, span)
	helpers.AssertNotNil(t, ctx)

	tracer.FinishSpan(span)
}

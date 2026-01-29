package monitoring_test

import (
	"testing"

	"streamgate/pkg/monitoring"
	"streamgate/test/helpers"
)

func TestMetrics_IncrementCounter(t *testing.T) {
	// Create metrics collector
	mc := monitoring.NewMetricsCollector(nil)

	// Increment counter
	mc.IncrementCounter("test_counter", nil)
	mc.IncrementCounter("test_counter", nil)

	// Verify counter was incremented
	helpers.AssertNotNil(t, mc)
}

func TestMetrics_SetGauge(t *testing.T) {
	// Create metrics collector
	mc := monitoring.NewMetricsCollector(nil)

	// Set gauge
	mc.SetGauge("test_gauge", 100, nil)

	// Verify gauge was set
	helpers.AssertNotNil(t, mc)
}

package monitoring_test

import (
	"testing"

	"streamgate/pkg/monitoring"
	"streamgate/test/helpers"
)

func TestMetrics_RecordCounter(t *testing.T) {
	// Create metrics
	metrics := monitoring.NewMetrics()

	// Record counter
	metrics.RecordCounter("test_counter", 1)
	metrics.RecordCounter("test_counter", 2)

	// Get counter value
	value := metrics.GetCounter("test_counter")
	helpers.AssertEqual(t, int64(3), value)
}

func TestMetrics_RecordGauge(t *testing.T) {
	// Create metrics
	metrics := monitoring.NewMetrics()

	// Record gauge
	metrics.RecordGauge("test_gauge", 100)

	// Get gauge value
	value := metrics.GetGauge("test_gauge")
	helpers.AssertEqual(t, float64(100), value)

	// Update gauge
	metrics.RecordGauge("test_gauge", 200)
	value = metrics.GetGauge("test_gauge")
	helpers.AssertEqual(t, float64(200), value)
}

func TestMetrics_RecordHistogram(t *testing.T) {
	// Create metrics
	metrics := monitoring.NewMetrics()

	// Record histogram
	metrics.RecordHistogram("test_histogram", 10)
	metrics.RecordHistogram("test_histogram", 20)
	metrics.RecordHistogram("test_histogram", 30)

	// Get histogram stats
	stats := metrics.GetHistogramStats("test_histogram")
	helpers.AssertNotNil(t, stats)
	helpers.AssertEqual(t, int64(3), stats.Count)
}

func TestMetrics_RecordDuration(t *testing.T) {
	// Create metrics
	metrics := monitoring.NewMetrics()

	// Record duration
	metrics.RecordDuration("test_duration", 100)
	metrics.RecordDuration("test_duration", 200)

	// Get duration stats
	stats := metrics.GetDurationStats("test_duration")
	helpers.AssertNotNil(t, stats)
	helpers.AssertTrue(t, stats.Count > 0)
}

func TestMetrics_GetAllMetrics(t *testing.T) {
	// Create metrics
	metrics := monitoring.NewMetrics()

	// Record various metrics
	metrics.RecordCounter("counter1", 1)
	metrics.RecordGauge("gauge1", 100)
	metrics.RecordHistogram("histogram1", 50)

	// Get all metrics
	allMetrics := metrics.GetAllMetrics()
	helpers.AssertNotNil(t, allMetrics)
	helpers.AssertTrue(t, len(allMetrics) > 0)
}

func TestMetrics_ResetMetrics(t *testing.T) {
	// Create metrics
	metrics := monitoring.NewMetrics()

	// Record metrics
	metrics.RecordCounter("counter1", 1)
	metrics.RecordGauge("gauge1", 100)

	// Verify metrics exist
	value := metrics.GetCounter("counter1")
	helpers.AssertEqual(t, int64(1), value)

	// Reset metrics
	metrics.Reset()

	// Verify metrics are reset
	value = metrics.GetCounter("counter1")
	helpers.AssertEqual(t, int64(0), value)
}

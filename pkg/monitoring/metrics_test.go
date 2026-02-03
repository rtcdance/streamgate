package monitoring

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewMetricsCollector(t *testing.T) {
	t.Run("creates new collector", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		assert.NotNil(t, collector)
		assert.NotNil(t, collector.metrics)
		assert.NotNil(t, collector.logger)
	})
}

func TestMetricsCollector_IncrementCounter(t *testing.T) {
	t.Run("increment counter", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.IncrementCounter("test_counter", map[string]string{"tag": "value"})

		metric := collector.GetMetric("test_counter")
		assert.NotNil(t, metric)
		assert.Equal(t, "counter", metric.Type)
		assert.Equal(t, 1.0, metric.Value)
		assert.Equal(t, int64(1), metric.Count)
	})

	t.Run("increment counter multiple times", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.IncrementCounter("test_counter", nil)
		collector.IncrementCounter("test_counter", nil)
		collector.IncrementCounter("test_counter", nil)

		metric := collector.GetMetric("test_counter")
		assert.Equal(t, 3.0, metric.Value)
		assert.Equal(t, int64(3), metric.Count)
	})

	t.Run("increment counter with different tags", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.IncrementCounter("test_counter", map[string]string{"env": "prod"})
		collector.IncrementCounter("test_counter", map[string]string{"env": "dev"})

		metrics := collector.GetAllMetrics()
		assert.Len(t, metrics, 2)
	})
}

func TestMetricsCollector_SetGauge(t *testing.T) {
	t.Run("set gauge", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.SetGauge("test_gauge", 42.5, map[string]string{"tag": "value"})

		metric := collector.GetMetric("test_gauge")
		assert.NotNil(t, metric)
		assert.Equal(t, "gauge", metric.Type)
		assert.Equal(t, 42.5, metric.Value)
		assert.Equal(t, 42.5, metric.Min)
		assert.Equal(t, 42.5, metric.Max)
	})

	t.Run("update gauge", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.SetGauge("test_gauge", 10.0, nil)
		collector.SetGauge("test_gauge", 20.0, nil)
		collector.SetGauge("test_gauge", 15.0, nil)

		metric := collector.GetMetric("test_gauge")
		assert.Equal(t, 15.0, metric.Value)
		assert.Equal(t, 10.0, metric.Min)
		assert.Equal(t, 20.0, metric.Max)
	})
}

func TestMetricsCollector_RecordHistogram(t *testing.T) {
	t.Run("record histogram", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.RecordHistogram("test_histogram", 100.0, map[string]string{"tag": "value"})

		metric := collector.GetMetric("test_histogram")
		assert.NotNil(t, metric)
		assert.Equal(t, "histogram", metric.Type)
		assert.Equal(t, 100.0, metric.Value)
		assert.Equal(t, int64(1), metric.Count)
		assert.Equal(t, 100.0, metric.Sum)
	})

	t.Run("record multiple histogram values", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.RecordHistogram("test_histogram", 100.0, nil)
		collector.RecordHistogram("test_histogram", 200.0, nil)
		collector.RecordHistogram("test_histogram", 300.0, nil)

		metric := collector.GetMetric("test_histogram")
		assert.Equal(t, int64(3), metric.Count)
		assert.Equal(t, 600.0, metric.Sum)
		assert.Equal(t, 200.0, metric.Value)
		assert.Equal(t, 100.0, metric.Min)
		assert.Equal(t, 300.0, metric.Max)
	})
}

func TestMetricsCollector_RecordTimer(t *testing.T) {
	t.Run("record timer", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		duration := 150 * time.Millisecond
		collector.RecordTimer("test_timer", duration, nil)

		metric := collector.GetMetric("test_timer")
		assert.NotNil(t, metric)
		assert.Equal(t, "histogram", metric.Type)
		assert.Equal(t, 150.0, metric.Value)
	})
}

func TestMetricsCollector_GetMetric(t *testing.T) {
	t.Run("get existing metric", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.IncrementCounter("test", nil)

		metric := collector.GetMetric("test")
		assert.NotNil(t, metric)
		assert.Equal(t, "test", metric.Name)
	})

	t.Run("get non-existing metric", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		metric := collector.GetMetric("nonexistent")
		assert.Nil(t, metric)
	})
}

func TestMetricsCollector_GetAllMetrics(t *testing.T) {
	t.Run("get all metrics", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.IncrementCounter("counter1", nil)
		collector.SetGauge("gauge1", 10.0, nil)
		collector.RecordHistogram("histogram1", 100.0, nil)

		metrics := collector.GetAllMetrics()
		assert.Len(t, metrics, 3)
		assert.Contains(t, metrics, "counter1")
		assert.Contains(t, metrics, "gauge1")
		assert.Contains(t, metrics, "histogram1")
	})
}

func TestMetricsCollector_GetMetricsSnapshot(t *testing.T) {
	t.Run("get snapshot", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.IncrementCounter("test", nil)

		snapshot := collector.GetMetricsSnapshot()
		assert.NotNil(t, snapshot)
		assert.Contains(t, snapshot, "uptime")
		assert.Contains(t, snapshot, "metrics_count")
		assert.Contains(t, snapshot, "metrics")
		assert.Greater(t, snapshot["uptime"], 0.0)
		assert.Equal(t, 1, snapshot["metrics_count"])
	})
}

func TestMetricsCollector_Reset(t *testing.T) {
	t.Run("reset metrics", func(t *testing.T) {
		logger := zap.NewNop()
		collector := NewMetricsCollector(logger)

		collector.IncrementCounter("test", nil)
		collector.SetGauge("gauge", 10.0, nil)

		assert.Len(t, collector.GetAllMetrics(), 2)

		collector.Reset()

		assert.Len(t, collector.GetAllMetrics(), 0)
	})
}

func TestNewServiceMetricsTracker(t *testing.T) {
	t.Run("creates new tracker", func(t *testing.T) {
		logger := zap.NewNop()
		tracker := NewServiceMetricsTracker(logger)

		assert.NotNil(t, tracker)
		assert.NotNil(t, tracker.services)
		assert.NotNil(t, tracker.logger)
	})
}

func TestServiceMetricsTracker_RecordRequest(t *testing.T) {
	t.Run("record successful request", func(t *testing.T) {
		logger := zap.NewNop()
		tracker := NewServiceMetricsTracker(logger)

		tracker.RecordRequest("test-service", 100, true)

		metrics := tracker.GetServiceMetrics("test-service")
		assert.NotNil(t, metrics)
		assert.Equal(t, int64(1), metrics.RequestCount)
		assert.Equal(t, int64(1), metrics.SuccessCount)
		assert.Equal(t, int64(0), metrics.ErrorCount)
		assert.Equal(t, int64(100), metrics.TotalLatency)
		assert.Equal(t, int64(100), metrics.MinLatency)
		assert.Equal(t, int64(100), metrics.MaxLatency)
	})

	t.Run("record failed request", func(t *testing.T) {
		logger := zap.NewNop()
		tracker := NewServiceMetricsTracker(logger)

		tracker.RecordRequest("test-service", 100, false)

		metrics := tracker.GetServiceMetrics("test-service")
		assert.Equal(t, int64(0), metrics.SuccessCount)
		assert.Equal(t, int64(1), metrics.ErrorCount)
	})

	t.Run("record multiple requests", func(t *testing.T) {
		logger := zap.NewNop()
		tracker := NewServiceMetricsTracker(logger)

		tracker.RecordRequest("test-service", 100, true)
		tracker.RecordRequest("test-service", 200, true)
		tracker.RecordRequest("test-service", 150, true)

		metrics := tracker.GetServiceMetrics("test-service")
		assert.Equal(t, int64(3), metrics.RequestCount)
		assert.Equal(t, int64(3), metrics.SuccessCount)
		assert.Equal(t, int64(450), metrics.TotalLatency)
		assert.Equal(t, 150.0, metrics.AverageLatency)
		assert.Equal(t, int64(100), metrics.MinLatency)
		assert.Equal(t, int64(200), metrics.MaxLatency)
	})
}

func TestServiceMetricsTracker_GetErrorRate(t *testing.T) {
	t.Run("get error rate", func(t *testing.T) {
		logger := zap.NewNop()
		tracker := NewServiceMetricsTracker(logger)

		tracker.RecordRequest("test-service", 100, true)
		tracker.RecordRequest("test-service", 100, false)
		tracker.RecordRequest("test-service", 100, true)
		tracker.RecordRequest("test-service", 100, false)

		rate := tracker.GetErrorRate("test-service")
		assert.Equal(t, 0.5, rate)
	})

	t.Run("get error rate for non-existent service", func(t *testing.T) {
		logger := zap.NewNop()
		tracker := NewServiceMetricsTracker(logger)

		rate := tracker.GetErrorRate("nonexistent")
		assert.Equal(t, 0.0, rate)
	})
}

func TestServiceMetricsTracker_GetSuccessRate(t *testing.T) {
	t.Run("get success rate", func(t *testing.T) {
		logger := zap.NewNop()
		tracker := NewServiceMetricsTracker(logger)

		tracker.RecordRequest("test-service", 100, true)
		tracker.RecordRequest("test-service", 100, false)
		tracker.RecordRequest("test-service", 100, true)
		tracker.RecordRequest("test-service", 100, false)

		rate := tracker.GetSuccessRate("test-service")
		assert.Equal(t, 0.5, rate)
	})
}

func TestServiceMetricsTracker_GetAllServiceMetrics(t *testing.T) {
	t.Run("get all service metrics", func(t *testing.T) {
		logger := zap.NewNop()
		tracker := NewServiceMetricsTracker(logger)

		tracker.RecordRequest("service1", 100, true)
		tracker.RecordRequest("service2", 200, true)
		tracker.RecordRequest("service3", 150, true)

		metrics := tracker.GetAllServiceMetrics()
		assert.Len(t, metrics, 3)
		assert.Contains(t, metrics, "service1")
		assert.Contains(t, metrics, "service2")
		assert.Contains(t, metrics, "service3")
	})
}

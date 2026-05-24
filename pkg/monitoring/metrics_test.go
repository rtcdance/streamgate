package monitoring

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewMetricsCollector_NilLogger(t *testing.T) {
	collector := NewMetricsCollector(nil)
	assert.NotNil(t, collector)
	assert.NotNil(t, collector.metrics)
}

func TestMetricsCollector_GetMetricKey(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	tests := []struct {
		name     string
		metric   string
		tags     map[string]string
		expected string
	}{
		{"no tags", "cpu", nil, "cpu"},
		{"single tag", "cpu", map[string]string{"env": "prod"}, "cpu:env=prod"},
		{"multiple tags sorted", "cpu", map[string]string{"zone": "a", "env": "prod"}, "cpu:env=prod:zone=a"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := collector.getMetricKey(tc.metric, tc.tags)
			assert.Equal(t, tc.expected, key)
		})
	}
}

func TestMetricsCollector_IncrementCounter_Concurrent(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			collector.IncrementCounter("concurrent_counter", nil)
		}()
	}
	wg.Wait()

	metric := collector.GetMetric("concurrent_counter")
	require.NotNil(t, metric)
	assert.Equal(t, 100.0, metric.Value)
	assert.Equal(t, int64(100), metric.Count)
}

func TestMetricsCollector_SetGauge_Concurrent(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(v float64) {
			defer wg.Done()
			collector.SetGauge("concurrent_gauge", v, nil)
		}(float64(i))
	}
	wg.Wait()

	metric := collector.GetMetric("concurrent_gauge")
	require.NotNil(t, metric)
	assert.Equal(t, int64(50), metric.Count)
}

func TestMetricsCollector_RecordHistogram_SingleValue(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	collector.RecordHistogram("latency", 50.0, nil)

	metric := collector.GetMetric("latency")
	require.NotNil(t, metric)
	assert.Equal(t, "histogram", metric.Type)
	assert.Equal(t, 50.0, metric.Value)
	assert.Equal(t, int64(1), metric.Count)
	assert.Equal(t, 50.0, metric.Sum)
	assert.Equal(t, 50.0, metric.Min)
	assert.Equal(t, 50.0, metric.Max)
}

func TestMetricsCollector_RecordTimer_ConvertsToMs(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	collector.RecordTimer("timer", 250*time.Millisecond, nil)

	metric := collector.GetMetric("timer")
	require.NotNil(t, metric)
	assert.Equal(t, 250.0, metric.Value)
}

func TestMetricsCollector_GetMetricsSnapshot_Empty(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	snapshot := collector.GetMetricsSnapshot()

	assert.NotNil(t, snapshot)
	assert.Equal(t, 0, snapshot["metrics_count"])
	assert.GreaterOrEqual(t, snapshot["uptime"], 0.0)
}

func TestMetricsCollector_Reset_ResetsStartTime(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	collector.IncrementCounter("test", nil)

	snapBefore := collector.GetMetricsSnapshot()
	collector.Reset()
	snapAfter := collector.GetMetricsSnapshot()

	assert.GreaterOrEqual(t, snapAfter["uptime"], 0.0)
	assert.LessOrEqual(t, snapAfter["uptime"].(float64), snapBefore["uptime"].(float64)+1.0)
}

func TestMetricsCollector_SetGauge_NegativeValues(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	collector.SetGauge("temp", -10.0, nil)
	collector.SetGauge("temp", 20.0, nil)
	collector.SetGauge("temp", -5.0, nil)

	metric := collector.GetMetric("temp")
	require.NotNil(t, metric)
	assert.Equal(t, -5.0, metric.Value)
	assert.Equal(t, -10.0, metric.Min)
	assert.Equal(t, 20.0, metric.Max)
}

func TestMetricsCollector_RecordHistogram_UpdatesAverage(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	collector.RecordHistogram("avg_test", 100.0, nil)
	collector.RecordHistogram("avg_test", 200.0, nil)
	collector.RecordHistogram("avg_test", 300.0, nil)

	metric := collector.GetMetric("avg_test")
	require.NotNil(t, metric)
	assert.Equal(t, 200.0, metric.Value)
	assert.Equal(t, 600.0, metric.Sum)
}

func TestServiceMetricsTracker_RecordRequest_MixedSuccessFailure(t *testing.T) {
	tracker := NewServiceMetricsTracker(zap.NewNop())

	tracker.RecordRequest("svc", 50, true)
	tracker.RecordRequest("svc", 100, false)
	tracker.RecordRequest("svc", 150, true)

	metrics := tracker.GetServiceMetrics("svc")
	require.NotNil(t, metrics)
	assert.Equal(t, int64(3), metrics.RequestCount)
	assert.Equal(t, int64(2), metrics.SuccessCount)
	assert.Equal(t, int64(1), metrics.ErrorCount)
	assert.Equal(t, int64(50), metrics.MinLatency)
	assert.Equal(t, int64(150), metrics.MaxLatency)
	assert.InDelta(t, 100.0, metrics.AverageLatency, 0.01)
}

func TestServiceMetricsTracker_GetSuccessRate_FullSuccess(t *testing.T) {
	tracker := NewServiceMetricsTracker(zap.NewNop())
	tracker.RecordRequest("svc", 10, true)
	tracker.RecordRequest("svc", 10, true)

	rate := tracker.GetSuccessRate("svc")
	assert.Equal(t, 1.0, rate)
}

func TestServiceMetricsTracker_GetSuccessRate_NonExistent(t *testing.T) {
	tracker := NewServiceMetricsTracker(zap.NewNop())
	rate := tracker.GetSuccessRate("missing")
	assert.Equal(t, 1.0, rate)
}

func TestServiceMetricsTracker_MultipleServices(t *testing.T) {
	tracker := NewServiceMetricsTracker(zap.NewNop())

	tracker.RecordRequest("auth", 10, true)
	tracker.RecordRequest("streaming", 50, true)
	tracker.RecordRequest("streaming", 100, false)

	authMetrics := tracker.GetServiceMetrics("auth")
	require.NotNil(t, authMetrics)
	assert.Equal(t, int64(1), authMetrics.RequestCount)

	streamMetrics := tracker.GetServiceMetrics("streaming")
	require.NotNil(t, streamMetrics)
	assert.Equal(t, int64(2), streamMetrics.RequestCount)
	assert.Equal(t, int64(1), streamMetrics.ErrorCount)
}

func TestRPCProviderFromURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"infura url", "https://mainnet.infura.io/v3/key", "infura"},
		{"alchemy url", "https://eth-mainnet.g.alchemy.com/v2/key", "alchemy"},
		{"local url", "http://localhost:8545", "localhost"},
		{"invalid url", "not-a-url", "not-a-url"},
		{"single part host", "http://myhost:8545", "myhost"},
		{"empty string", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := RPCProviderFromURL(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

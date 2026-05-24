package analytics

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewAggregator(t *testing.T) {
	agg := NewAggregator()
	assert.NotNil(t, agg)
	assert.NotNil(t, agg.aggregations)
	assert.NotNil(t, agg.events)
	assert.Equal(t, []string{"1m", "5m", "15m", "1h", "1d"}, agg.periods)

	err := agg.Close()
	assert.NoError(t, err)
}

func TestAggregator_AddEvent(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	agg.AddEvent(&AnalyticsEvent{
		ID:        "evt-1",
		Timestamp: time.Now(),
		EventType: "request",
		ServiceID: "api-gateway",
		UserID:    "user-1",
	})

	agg.mu.RLock()
	count := len(agg.events)
	agg.mu.RUnlock()
	assert.Equal(t, 1, count)
}

func TestAggregator_AddMetrics(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	agg.AddMetrics(&MetricsSnapshot{
		ID:        "m-1",
		Timestamp: time.Now(),
		ServiceID: "api-gateway",
		CPUUsage:  50.0,
		Latency:   100.0,
	})

	agg.mu.RLock()
	count := len(agg.metrics)
	agg.mu.RUnlock()
	assert.Equal(t, 1, count)
}

func TestAggregator_AddBehavior(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	agg.AddBehavior(&UserBehavior{
		ID:        "b-1",
		Timestamp: time.Now(),
		UserID:    "user-1",
		Action:    "play",
		ContentID: "content-1",
	})

	agg.mu.RLock()
	count := len(agg.behaviors)
	agg.mu.RUnlock()
	assert.Equal(t, 1, count)
}

func TestAggregator_AddPerformanceMetric(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	agg.AddPerformanceMetric(&PerformanceMetric{
		ID:        "p-1",
		Timestamp: time.Now(),
		ServiceID: "api-gateway",
		Operation: "query",
		Duration:  150.0,
		Success:   true,
	})

	agg.mu.RLock()
	count := len(agg.perfMetrics)
	agg.mu.RUnlock()
	assert.Equal(t, 1, count)
}

func TestAggregator_AddBusinessMetric(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	agg.AddBusinessMetric(&BusinessMetric{
		ID:         "bm-1",
		Timestamp:  time.Now(),
		MetricType: "revenue",
		Value:      99.99,
		Unit:       "USD",
	})

	agg.mu.RLock()
	count := len(agg.businessMetrics)
	agg.mu.RUnlock()
	assert.Equal(t, 1, count)
}

func TestAggregator_AggregateNow(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	agg.AddEvent(&AnalyticsEvent{
		ID:        "evt-1",
		Timestamp: time.Now(),
		EventType: "request",
		ServiceID: "api-gateway",
	})
	agg.AddPerformanceMetric(&PerformanceMetric{
		ID:        "p-1",
		Timestamp: time.Now(),
		ServiceID: "api-gateway",
		Operation: "query",
		Duration:  100.0,
		Success:   true,
	})

	agg.AggregateNow()

	result := agg.GetAggregations("api-gateway")
	assert.NotEmpty(t, result)
}

func TestAggregator_GetAggregations_Empty(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	result := agg.GetAggregations("nonexistent")
	assert.Empty(t, result)
}

func TestAggregator_GetLatestAggregation(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	agg.AddEvent(&AnalyticsEvent{
		ID:        "evt-1",
		Timestamp: time.Now(),
		EventType: "request",
		ServiceID: "api-gateway",
	})
	agg.AddPerformanceMetric(&PerformanceMetric{
		ID:        "p-1",
		Timestamp: time.Now(),
		ServiceID: "api-gateway",
		Operation: "query",
		Duration:  200.0,
		Success:   true,
	})

	agg.AggregateNow()

	latest := agg.GetLatestAggregation("api-gateway", "1m")
	assert.NotNil(t, latest)
	assert.Equal(t, "api-gateway", latest.ServiceID)
	assert.Equal(t, "1m", latest.Period)
	assert.GreaterOrEqual(t, latest.EventCount, int64(1))
}

func TestAggregator_GetLatestAggregation_NotFound(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	latest := agg.GetLatestAggregation("nonexistent", "1m")
	assert.Nil(t, latest)
}

func TestAggregator_Percentile(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	assert.Equal(t, 0.0, agg.percentile([]float64{}, 50))
	assert.Equal(t, 60.0, agg.percentile([]float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}, 50))
	assert.Equal(t, 100.0, agg.percentile([]float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}, 90))
	assert.Equal(t, 10.0, agg.percentile([]float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}, 0))
	assert.Equal(t, 100.0, agg.percentile([]float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}, 100))
}

func TestAggregator_Average(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	assert.Equal(t, 0.0, agg.average([]float64{}))
	assert.Equal(t, 55.0, agg.average([]float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}))
	assert.Equal(t, 3.0, agg.average([]float64{1, 2, 3, 4, 5}))
}

func TestAggregator_GetCutoffTime(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	now := time.Now()

	tests := []struct {
		period  string
		wantDur time.Duration
	}{
		{"1m", 1 * time.Minute},
		{"5m", 5 * time.Minute},
		{"15m", 15 * time.Minute},
		{"1h", 1 * time.Hour},
		{"1d", 24 * time.Hour},
		{"unknown", 1 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			cutoff := agg.getCutoffTime(now, tt.period)
			expected := now.Add(-tt.wantDur)
			assert.WithinDuration(t, expected, cutoff, time.Second)
		})
	}
}

func TestAggregator_GetPeriodSeconds(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	tests := []struct {
		period string
		want   float64
	}{
		{"1m", 60},
		{"5m", 300},
		{"15m", 900},
		{"1h", 3600},
		{"1d", 86400},
		{"unknown", 60},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			assert.Equal(t, tt.want, agg.getPeriodSeconds(tt.period))
		})
	}
}

func TestAggregator_CleanupOldData(t *testing.T) {
	agg := NewAggregator()
	defer agg.Close()

	oldTime := time.Now().Add(-48 * time.Hour)
	recentTime := time.Now()

	agg.AddEvent(&AnalyticsEvent{ID: "old", Timestamp: oldTime, ServiceID: "svc"})
	agg.AddEvent(&AnalyticsEvent{ID: "recent", Timestamp: recentTime, ServiceID: "svc"})
	agg.AddMetrics(&MetricsSnapshot{ID: "old-m", Timestamp: oldTime, ServiceID: "svc"})
	agg.AddMetrics(&MetricsSnapshot{ID: "recent-m", Timestamp: recentTime, ServiceID: "svc"})
	agg.AddBehavior(&UserBehavior{ID: "old-b", Timestamp: oldTime})
	agg.AddBehavior(&UserBehavior{ID: "recent-b", Timestamp: recentTime})
	agg.AddPerformanceMetric(&PerformanceMetric{ID: "old-p", Timestamp: oldTime, ServiceID: "svc"})
	agg.AddPerformanceMetric(&PerformanceMetric{ID: "recent-p", Timestamp: recentTime, ServiceID: "svc"})
	agg.AddBusinessMetric(&BusinessMetric{ID: "old-bm", Timestamp: oldTime})
	agg.AddBusinessMetric(&BusinessMetric{ID: "recent-bm", Timestamp: recentTime})

	agg.AggregateNow()

	agg.mu.RLock()
	events := len(agg.events)
	metrics := len(agg.metrics)
	behaviors := len(agg.behaviors)
	perfMetrics := len(agg.perfMetrics)
	bizMetrics := len(agg.businessMetrics)
	agg.mu.RUnlock()

	assert.Equal(t, 1, events)
	assert.Equal(t, 1, metrics)
	assert.Equal(t, 1, behaviors)
	assert.Equal(t, 1, perfMetrics)
	assert.Equal(t, 1, bizMetrics)
}

func TestNewEventCollector(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())
	assert.NotNil(t, ec)
	assert.Equal(t, 100, ec.bufferSize)
	assert.Equal(t, 5*time.Second, ec.flushInterval)

	err := ec.Close()
	assert.NoError(t, err)
}

func TestEventCollector_RecordEvent(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())
	defer ec.Close()

	ec.RecordEvent("request", "api-gateway", "user-1", map[string]interface{}{}, map[string]string{})
}

func TestEventCollector_RecordMetrics(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())
	defer ec.Close()

	ec.RecordMetrics("api-gateway", 50.0, 60.0, 30.0, 1000.0, 0.01, 50.0, 0.95)
}

func TestEventCollector_RecordUserBehavior(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())
	defer ec.Close()

	ec.RecordUserBehavior("user-1", "play", "content-1", "127.0.0.1", "test-agent", "session-1", 120, true, "")
}

func TestEventCollector_RecordPerformanceMetric(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())
	defer ec.Close()

	ec.RecordPerformanceMetric("api-gateway", "query", 150.0, 0.5, 1000.0, true, "")
}

func TestEventCollector_RecordBusinessMetric(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())
	defer ec.Close()

	ec.RecordBusinessMetric("revenue", 99.99, "USD", map[string]string{"region": "us"})
}

func TestEventCollector_Subscribe(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())
	defer ec.Close()

	var mu sync.Mutex
	called := false
	ec.Subscribe("event", func(event interface{}) error {
		mu.Lock()
		called = true
		mu.Unlock()
		return nil
	})

	ec.RecordEvent("request", "api-gateway", "user-1", nil, nil)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return called
	}, 2*time.Second, 10*time.Millisecond)
}

func TestEventCollector_FlushNow(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())
	defer ec.Close()

	ec.FlushNow()
}

func TestEventCollector_Close_Twice(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())

	err := ec.Close()
	assert.NoError(t, err)

	err = ec.Close()
	assert.NoError(t, err)
}

func TestEventCollector_RecordAfterClose(t *testing.T) {
	ec := NewEventCollector(100, 5*time.Second, zap.NewNop())
	ec.Close()

	ec.RecordEvent("request", "svc", "user", nil, nil)
	ec.RecordMetrics("svc", 1, 2, 3, 4, 5, 6, 7)
	ec.RecordUserBehavior("user", "act", "content", "ip", "ua", "sess", 1, true, "")
	ec.RecordPerformanceMetric("svc", "op", 1, 2, 3, true, "")
	ec.RecordBusinessMetric("type", 1, "unit", nil)
}

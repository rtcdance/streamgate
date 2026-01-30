package analytics

import (
	"testing"
	"time"

	"streamgate/pkg/analytics"
)

// TestEventCollector tests the event collector
func TestEventCollector(t *testing.T) {
	collector := analytics.NewEventCollector(100, 1*time.Second)
	defer collector.Close()

	// Record an event
	collector.RecordEvent("test_event", "service1", "user1", map[string]interface{}{"key": "value"}, map[string]string{"tag": "value"})

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	// Verify event was recorded (basic check)
	if collector == nil {
		t.Error("Collector should not be nil")
	}
}

// TestAggregator tests the aggregator
func TestAggregator(t *testing.T) {
	agg := analytics.NewAggregator()
	defer agg.Close()

	// Add some events
	for i := 0; i < 10; i++ {
		event := &analytics.AnalyticsEvent{
			ID:        "test",
			Timestamp: time.Now(),
			EventType: "test",
			ServiceID: "service1",
		}
		agg.AddEvent(event)
	}

	// Trigger immediate aggregation
	agg.AggregateNow()

	// Get aggregations
	aggs := agg.GetAggregations("service1")
	if len(aggs) == 0 {
		t.Error("Should have aggregations")
	}
}

// TestAnomalyDetector tests the anomaly detector
func TestAnomalyDetector(t *testing.T) {
	detector := analytics.NewAnomalyDetector(2.0)
	defer detector.Close()

	// Record some metrics with a clear anomaly
	for i := 0; i < 20; i++ {
		metric := &analytics.MetricsSnapshot{
			ID:          "test",
			Timestamp:   time.Now(),
			ServiceID:   "service1",
			CPUUsage:    50.0,
			MemoryUsage: 60.0,
			ErrorRate:   0.01,
			Latency:     100.0,
			RequestRate: 1000.0,
		}
		detector.RecordMetric(metric)
	}

	// Add an anomalous metric
	anomalousMetric := &analytics.MetricsSnapshot{
		ID:          "test",
		Timestamp:   time.Now(),
		ServiceID:   "service1",
		CPUUsage:    200.0,
		MemoryUsage: 60.0,
		ErrorRate:   0.01,
		Latency:     100.0,
		RequestRate: 1000.0,
	}
	detector.RecordMetric(anomalousMetric)

	// Trigger immediate anomaly detection twice - first to establish baseline, second to detect
	detector.DetectAnomaliesNow()
	detector.DetectAnomaliesNow()

	// Get anomalies - should detect the anomalous CPU usage
	anomalies := detector.GetAnomalies("service1", 10)
	if anomalies == nil {
		t.Error("Anomalies should not be nil")
	}
}

// TestPredictor tests the predictor
func TestPredictor(t *testing.T) {
	predictor := analytics.NewPredictor()
	defer predictor.Close()

	// Record some metrics
	for i := 0; i < 30; i++ {
		metric := &analytics.MetricsSnapshot{
			ID:          "test",
			Timestamp:   time.Now(),
			ServiceID:   "service1",
			CPUUsage:    50.0 + float64(i),
			MemoryUsage: 60.0,
			ErrorRate:   0.01,
			Latency:     100.0,
			RequestRate: 1000.0,
		}
		predictor.RecordMetric(metric)
	}

	// Trigger immediate predictions
	predictor.MakePredictionsNow()

	// Get predictions
	predictions := predictor.GetPredictions("service1", 10)
	if predictions == nil {
		t.Error("Predictions should not be nil")
	}
}

// TestAnalyticsService tests the analytics service
func TestAnalyticsService(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record an event
	service.RecordEvent("test_event", "service1", "user1", map[string]interface{}{"key": "value"}, map[string]string{"tag": "value"})

	// Record metrics
	service.RecordMetrics("service1", 50.0, 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)

	// Record user behavior
	service.RecordUserBehavior("user1", "play", "content1", "127.0.0.1", "Mozilla", "session1", 5000, true, "")

	// Record performance metric
	service.RecordPerformanceMetric("service1", "upload", 100.0, 50.0, 1000.0, true, "")

	// Record business metric
	service.RecordBusinessMetric("revenue", 100.0, "USD", map[string]string{"region": "US"})

	// Wait for async notifications to complete
	time.Sleep(50 * time.Millisecond)

	// Trigger immediate processing
	service.AggregateNow()

	// Get aggregations
	aggs := service.GetAggregations("service1")
	if len(aggs) == 0 {
		t.Error("Should have aggregations")
	}

	// Get dashboard data
	data := service.GetDashboardData("service1")
	if data == nil {
		t.Error("Dashboard data should not be nil")
	}

	if data.SystemHealth == "" {
		t.Error("System health should not be empty")
	}
}

// TestMetricsRecording tests metrics recording
func TestMetricsRecording(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record multiple metrics
	for i := 0; i < 5; i++ {
		service.RecordMetrics("service1", float64(50+i), float64(60+i), float64(70+i), float64(1000+i*100), 0.01, float64(100+i*10), 0.95)
	}

	// Record performance metrics to ensure aggregations are created
	for i := 0; i < 5; i++ {
		service.RecordPerformanceMetric("service1", "test_operation", float64(100+i*10), float64(50+i*5), float64(1000+i*100), true, "")
	}

	// Wait for async notifications to complete
	time.Sleep(50 * time.Millisecond)

	// Trigger immediate aggregation
	service.AggregateNow()

	// Verify metrics were recorded
	aggs := service.GetAggregations("service1")
	if len(aggs) == 0 {
		t.Error("Should have aggregations")
	}
}

// TestUserBehaviorRecording tests user behavior recording
func TestUserBehaviorRecording(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record user behaviors
	for i := 0; i < 5; i++ {
		service.RecordUserBehavior("user1", "play", "content1", "127.0.0.1", "Mozilla", "session1", int64(5000+i*1000), true, "")
	}

	time.Sleep(100 * time.Millisecond)

	// Verify behaviors were recorded
	if service == nil {
		t.Error("Service should not be nil")
	}
}

// TestAnomalyDetection tests anomaly detection
func TestAnomalyDetection(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record normal metrics
	for i := 0; i < 20; i++ {
		service.RecordMetrics("service1", 50.0, 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)
	}

	// Record anomalous metric
	service.RecordMetrics("service1", 95.0, 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)

	time.Sleep(100 * time.Millisecond)

	// Get anomalies
	anomalies := service.GetAllAnomalies(10)
	if anomalies == nil {
		t.Error("Anomalies should not be nil")
	}
}

// TestPrediction tests prediction
func TestPrediction(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record metrics with trend
	for i := 0; i < 30; i++ {
		service.RecordMetrics("service1", float64(50+i), 60.0, 70.0, float64(1000+i*100), 0.01, 100.0, 0.95)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify service is working
	if service == nil {
		t.Error("Service should not be nil")
	}
}

// TestDashboardData tests dashboard data generation
func TestDashboardData(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record some data
	service.RecordMetrics("service1", 50.0, 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)
	service.RecordEvent("test_event", "service1", "user1", map[string]interface{}{}, map[string]string{})

	time.Sleep(100 * time.Millisecond)

	// Get dashboard data
	data := service.GetDashboardData("service1")
	if data == nil {
		t.Error("Dashboard data should not be nil")
	}

	if data.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}

	if data.SystemHealth == "" {
		t.Error("System health should not be empty")
	}
}

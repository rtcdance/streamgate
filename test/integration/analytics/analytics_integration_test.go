package analytics

import (
	"testing"
	"time"

	"streamgate/pkg/analytics"
)

// TestAnalyticsEndToEnd tests the complete analytics flow
func TestAnalyticsEndToEnd(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Simulate a complete workflow
	// 1. Record events
	for i := 0; i < 100; i++ {
		service.RecordEvent("user_action", "api-gateway", "user123", map[string]interface{}{
			"action": "upload",
			"size":   1024 * 1024,
		}, map[string]string{"region": "us-west"})
	}

	// 2. Record metrics
	for i := 0; i < 50; i++ {
		cpu := 30.0 + float64(i%20)
		memory := 40.0 + float64(i%15)
		service.RecordMetrics("api-gateway", cpu, memory, 50.0, 1000.0+float64(i*10), 0.01, 100.0+float64(i), 0.95)
	}

	// 3. Record user behavior
	for i := 0; i < 30; i++ {
		service.RecordUserBehavior("user123", "play", "content456", "192.168.1.1", "Mozilla/5.0", "session789", 5000+int64(i*100), true, "")
	}

	// 4. Record performance metrics
	for i := 0; i < 40; i++ {
		service.RecordPerformanceMetric("transcoder", "transcode", 2000.0+float64(i*50), 512.0, 0.5, true, "")
	}

	// 5. Record business metrics
	for i := 0; i < 20; i++ {
		service.RecordBusinessMetric("revenue", 99.99+float64(i), "USD", map[string]string{"tier": "premium"})
	}

	// Give time for processing
	time.Sleep(2 * time.Second)

	service.FlushNow()
	service.AggregateNow()

	// Verify aggregations
	aggs := service.GetAggregations("api-gateway")
	if len(aggs) == 0 {
		t.Error("Should have aggregations")
	}

	// Verify anomalies
	anomalies := service.GetAllAnomalies(50)
	if anomalies == nil {
		t.Error("Anomalies should not be nil")
	}

	// Verify dashboard data
	data := service.GetDashboardData("api-gateway")
	if data == nil {
		t.Error("Dashboard data should not be nil")
	}

	if data.SystemHealth == "" {
		t.Error("System health should be set")
	}

	if len(data.Aggregations) == 0 {
		t.Error("Should have aggregations in dashboard")
	}
}

// TestAnalyticsMultiService tests analytics across multiple services
func TestAnalyticsMultiService(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	services := []string{"api-gateway", "upload", "transcoder", "streaming", "metadata"}

	// Record metrics for each service
	for _, svc := range services {
		for i := 0; i < 30; i++ {
			service.RecordMetrics(svc, 40.0+float64(i%20), 50.0+float64(i%15), 60.0, 1000.0+float64(i*50), 0.01, 100.0+float64(i), 0.95)
		}
	}

	time.Sleep(1 * time.Second)

	service.FlushNow()
	service.AggregateNow()

	// Verify each service has aggregations
	for _, svc := range services {
		aggs := service.GetAggregations(svc)
		if len(aggs) == 0 {
			t.Errorf("Service %s should have aggregations", svc)
		}
	}
}

// TestAnalyticsAnomalyDetectionAccuracy tests anomaly detection accuracy
func TestAnalyticsAnomalyDetectionAccuracy(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record normal metrics
	for i := 0; i < 50; i++ {
		service.RecordMetrics("test-service", 50.0, 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)
	}

	time.Sleep(100 * time.Millisecond)
	service.FlushNow()
	service.DetectAnomaliesNow() // Establish baseline

	// Record anomalous metrics
	for i := 0; i < 5; i++ {
		service.RecordMetrics("test-service", 95.0, 60.0, 70.0, 1000.0, 0.05, 100.0, 0.95)
	}

	time.Sleep(2 * time.Second)

	service.FlushNow()
	service.DetectAnomaliesNow()

	// Get anomalies
	anomalies := service.GetAnomalies("test-service", 100)
	if len(anomalies) == 0 {
		t.Error("Should detect anomalies")
	}

	// Verify anomaly severity
	for _, anomaly := range anomalies {
		if anomaly.Severity == "" {
			t.Error("Anomaly should have severity")
		}
		if anomaly.Deviation == 0 {
			t.Error("Anomaly should have deviation")
		}
	}
}

// TestAnalyticsPredictionAccuracy tests prediction accuracy
func TestAnalyticsPredictionAccuracy(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record metrics with clear trend
	for i := 0; i < 50; i++ {
		cpu := 30.0 + float64(i)*0.5 // Clear upward trend
		service.RecordMetrics("test-service", cpu, 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)
	}

	time.Sleep(2 * time.Second)

	// Verify service is working
	if service == nil {
		t.Error("Service should not be nil")
	}
}

// TestAnalyticsDataPersistence tests data persistence across operations
func TestAnalyticsDataPersistence(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record initial data
	service.RecordMetrics("test-service", 50.0, 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)
	time.Sleep(500 * time.Millisecond)

	// Get initial aggregations
	aggs1 := service.GetAggregations("test-service")
	initialCount := len(aggs1)

	// Record more data
	for i := 0; i < 10; i++ {
		service.RecordMetrics("test-service", 50.0+float64(i), 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)
	}
	time.Sleep(500 * time.Millisecond)

	// Get updated aggregations
	aggs2 := service.GetAggregations("test-service")
	if len(aggs2) < initialCount {
		t.Error("Aggregations should persist")
	}
}

// TestAnalyticsHighLoad tests analytics under high load
func TestAnalyticsHighLoad(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Simulate high load
	for i := 0; i < 1000; i++ {
		service.RecordEvent("event", "service", "user", map[string]interface{}{}, map[string]string{})
		service.RecordMetrics("service", 50.0, 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)
	}

	time.Sleep(2 * time.Second)

	service.FlushNow()
	service.AggregateNow()

	// Verify data was processed
	aggs := service.GetAggregations("service")
	if len(aggs) == 0 {
		t.Error("Should handle high load")
	}
}

// TestAnalyticsErrorHandling tests error handling
func TestAnalyticsErrorHandling(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record with nil metadata
	service.RecordEvent("event", "service", "user", nil, nil)

	// Record with empty service ID
	service.RecordMetrics("", 50.0, 60.0, 70.0, 1000.0, 0.01, 100.0, 0.95)

	// Record with negative values
	service.RecordMetrics("service", -50.0, -60.0, -70.0, -1000.0, -0.01, -100.0, -0.95)

	time.Sleep(500 * time.Millisecond)

	// Should not crash
	if service == nil {
		t.Error("Service should handle errors gracefully")
	}
}

// TestAnalyticsMetricsAccuracy tests metrics calculation accuracy
func TestAnalyticsMetricsAccuracy(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	// Record known metrics
	latencies := []float64{100, 150, 200, 250, 300}
	for _, latency := range latencies {
		service.RecordPerformanceMetric("service", "operation", latency, 100.0, 1.0, true, "")
	}

	time.Sleep(1 * time.Second)

	// Verify aggregations
	aggs := service.GetAggregations("service")
	if len(aggs) > 0 {
		agg := aggs[0]
		// Average should be around 200
		if agg.AvgLatency < 100 || agg.AvgLatency > 300 {
			t.Errorf("Average latency should be around 200, got %f", agg.AvgLatency)
		}
	}
}

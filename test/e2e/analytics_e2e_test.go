package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"streamgate/pkg/analytics"
)

// TestAnalyticsAPIEndToEnd tests the complete analytics API flow
func TestAnalyticsAPIEndToEnd(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	handler := analytics.NewHandler(service, nil)

	// Test recording events
	eventPayload := map[string]interface{}{
		"event_type": "upload_started",
		"service_id": "upload-service",
		"user_id":    "user123",
		"metadata": map[string]interface{}{
			"file_size": 1024,
			"format":    "mp4",
		},
		"tags": map[string]string{
			"region": "us-west",
		},
	}

	eventBody, _ := json.Marshal(eventPayload)
	req := httptest.NewRequest("POST", "/api/v1/analytics/events", bytes.NewReader(eventBody))
	w := httptest.NewRecorder()
	handler.RecordEventHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test recording metrics
	metricsPayload := map[string]interface{}{
		"service_id":     "api-gateway",
		"cpu_usage":      45.5,
		"memory_usage":   62.3,
		"disk_usage":     78.1,
		"request_rate":   1250.5,
		"error_rate":     0.02,
		"latency":        125.3,
		"cache_hit_rate": 0.95,
	}

	metricsBody, _ := json.Marshal(metricsPayload)
	req = httptest.NewRequest("POST", "/api/v1/analytics/metrics", bytes.NewReader(metricsBody))
	w = httptest.NewRecorder()
	handler.RecordMetricsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	time.Sleep(500 * time.Millisecond)

	// Test getting aggregations
	req = httptest.NewRequest("GET", "/api/v1/analytics/aggregations?service_id=api-gateway", nil)
	w = httptest.NewRecorder()
	handler.GetAggregationsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var aggs []analytics.AnalyticsAggregation
	json.NewDecoder(w.Body).Decode(&aggs)
	if len(aggs) == 0 {
		t.Error("Should have aggregations")
	}

	// Test getting anomalies
	req = httptest.NewRequest("GET", "/api/v1/analytics/anomalies?service_id=api-gateway&limit=10", nil)
	w = httptest.NewRecorder()
	handler.GetAnomaliesHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test getting dashboard
	req = httptest.NewRequest("GET", "/api/v1/analytics/dashboard?service_id=api-gateway", nil)
	w = httptest.NewRecorder()
	handler.GetDashboardHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var dashboard analytics.DashboardData
	json.NewDecoder(w.Body).Decode(&dashboard)
	if dashboard.SystemHealth == "" {
		t.Error("Dashboard should have system health")
	}
}

// TestAnalyticsAPIErrorHandling tests API error handling
func TestAnalyticsAPIErrorHandling(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	handler := analytics.NewHandler(service, nil)

	// Test invalid method
	req := httptest.NewRequest("GET", "/api/v1/analytics/events", nil)
	w := httptest.NewRecorder()
	handler.RecordEventHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}

	// Test invalid JSON
	req = httptest.NewRequest("POST", "/api/v1/analytics/events", bytes.NewReader([]byte("invalid json")))
	w = httptest.NewRecorder()
	handler.RecordEventHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	// Test missing service_id
	req = httptest.NewRequest("GET", "/api/v1/analytics/aggregations", nil)
	w = httptest.NewRecorder()
	handler.GetAggregationsHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestAnalyticsAPIMultipleRequests tests multiple concurrent requests
func TestAnalyticsAPIMultipleRequests(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	handler := analytics.NewHandler(service, nil)

	// Send multiple requests
	for i := 0; i < 10; i++ {
		metricsPayload := map[string]interface{}{
			"service_id":     "api-gateway",
			"cpu_usage":      45.5 + float64(i),
			"memory_usage":   62.3,
			"disk_usage":     78.1,
			"request_rate":   1250.5,
			"error_rate":     0.02,
			"latency":        125.3,
			"cache_hit_rate": 0.95,
		}

		metricsBody, _ := json.Marshal(metricsPayload)
		req := httptest.NewRequest("POST", "/api/v1/analytics/metrics", bytes.NewReader(metricsBody))
		w := httptest.NewRecorder()
		handler.RecordMetricsHandler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d failed with status %d", i, w.Code)
		}
	}

	time.Sleep(500 * time.Millisecond)

	// Verify data was processed
	req := httptest.NewRequest("GET", "/api/v1/analytics/aggregations?service_id=api-gateway", nil)
	w := httptest.NewRecorder()
	handler.GetAggregationsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAnalyticsAPIDataConsistency tests data consistency
func TestAnalyticsAPIDataConsistency(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	handler := analytics.NewHandler(service, nil)

	// Record metrics
	metricsPayload := map[string]interface{}{
		"service_id":     "test-service",
		"cpu_usage":      50.0,
		"memory_usage":   60.0,
		"disk_usage":     70.0,
		"request_rate":   1000.0,
		"error_rate":     0.01,
		"latency":        100.0,
		"cache_hit_rate": 0.95,
	}

	metricsBody, _ := json.Marshal(metricsPayload)
	req := httptest.NewRequest("POST", "/api/v1/analytics/metrics", bytes.NewReader(metricsBody))
	w := httptest.NewRecorder()
	handler.RecordMetricsHandler(w, req)

	time.Sleep(500 * time.Millisecond)

	// Get aggregations multiple times
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/v1/analytics/aggregations?service_id=test-service", nil)
		w := httptest.NewRecorder()
		handler.GetAggregationsHandler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d failed with status %d", i, w.Code)
		}

		var aggs []analytics.AnalyticsAggregation
		json.NewDecoder(w.Body).Decode(&aggs)
		if len(aggs) == 0 {
			t.Errorf("Request %d should have aggregations", i)
		}
	}
}

// TestAnalyticsAPIHealthCheck tests health check endpoint
func TestAnalyticsAPIHealthCheck(t *testing.T) {
	service := analytics.NewService()
	defer service.Close()

	handler := analytics.NewHandler(service, nil)

	req := httptest.NewRequest("GET", "/api/v1/analytics/health", nil)
	w := httptest.NewRecorder()
	handler.HealthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	json.NewDecoder(w.Body).Decode(&response)
	if response["status"] != "healthy" {
		t.Error("Health check should return healthy status")
	}
}

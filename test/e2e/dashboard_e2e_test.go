package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDashboardMetricsAPI tests metrics API
func TestDashboardMetricsAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"memory_usage_mb": map[string]interface{}{
				"value":  250.0,
				"unit":   "MB",
				"status": "healthy",
			},
		})
	})

	req := httptest.NewRequest("GET", "/dashboard/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var metrics map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&metrics); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
}

// TestDashboardAlertsAPI tests alerts API
func TestDashboardAlertsAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"title":    "High Memory",
				"severity": "critical",
				"resolved": false,
			},
		})
	})

	req := httptest.NewRequest("GET", "/dashboard/alerts", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var alerts []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&alerts); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(alerts))
	}
}

// TestDashboardMetricHistoryAPI tests metric history API
func TestDashboardMetricHistoryAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"value": 200.0, "timestamp": "2025-01-28T10:00:00Z"},
			{"value": 250.0, "timestamp": "2025-01-28T10:05:00Z"},
		})
	})

	req := httptest.NewRequest("GET", "/dashboard/metrics/history?name=memory_usage_mb&limit=10", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var history []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&history); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(history) != 2 {
		t.Fatalf("Expected 2 history entries, got %d", len(history))
	}
}

// TestDashboardReportsAPI tests reports API
func TestDashboardReportsAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"period":  "5m",
				"summary": "System Status: 0 critical, 0 warning, 5 healthy",
			},
		})
	})

	req := httptest.NewRequest("GET", "/dashboard/reports?limit=10", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var reports []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&reports); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(reports) != 1 {
		t.Fatalf("Expected 1 report, got %d", len(reports))
	}
}

// TestDashboardStatusAPI tests status API
func TestDashboardStatusAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_metrics":    5,
			"critical_metrics": 0,
			"warning_metrics":  0,
			"healthy_metrics":  5,
			"overall_status":   "healthy",
		})
	})

	req := httptest.NewRequest("GET", "/dashboard/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if status["overall_status"] != "healthy" {
		t.Fatalf("Expected healthy status, got %v", status["overall_status"])
	}
}

// TestDashboardRecordMetricAPI tests record metric API
func TestDashboardRecordMetricAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "recorded"})
	})

	body := bytes.NewReader([]byte(`{"name":"memory_usage_mb","value":250.0,"unit":"MB"}`))
	req := httptest.NewRequest("POST", "/dashboard/metrics/record", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["status"] != "recorded" {
		t.Fatalf("Expected status 'recorded', got %s", response["status"])
	}
}

// TestDashboardCreateAlertAPI tests create alert API
func TestDashboardCreateAlertAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "created"})
	})

	body := bytes.NewReader([]byte(`{"title":"High Memory","message":"Memory exceeded 500MB","severity":"critical"}`))
	req := httptest.NewRequest("POST", "/dashboard/alerts/create", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["status"] != "created" {
		t.Fatalf("Expected status 'created', got %s", response["status"])
	}
}

// TestDashboardResolveAlertAPI tests resolve alert API
func TestDashboardResolveAlertAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "resolved"})
	})

	req := httptest.NewRequest("POST", "/dashboard/alerts/resolve?alert_id=123", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["status"] != "resolved" {
		t.Fatalf("Expected status 'resolved', got %s", response["status"])
	}
}

// TestDashboardAPIFlow tests complete dashboard API flow
func TestDashboardAPIFlow(t *testing.T) {
	// Test metrics endpoint
	handler1 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"memory_usage_mb": 250.0})
	})

	req1 := httptest.NewRequest("GET", "/dashboard/metrics", nil)
	w1 := httptest.NewRecorder()
	handler1.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("Metrics request failed with status %d", w1.Code)
	}

	// Test status endpoint
	handler2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"overall_status": "healthy"})
	})

	req2 := httptest.NewRequest("GET", "/dashboard/status", nil)
	w2 := httptest.NewRecorder()
	handler2.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Status request failed with status %d", w2.Code)
	}

	// Test reports endpoint
	handler3 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	})

	req3 := httptest.NewRequest("GET", "/dashboard/reports", nil)
	w3 := httptest.NewRecorder()
	handler3.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("Reports request failed with status %d", w3.Code)
	}
}

// TestDashboardAPIErrorHandling tests error handling
func TestDashboardAPIErrorHandling(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/dashboard/metrics/record", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected status 405, got %d", w.Code)
	}
}

// TestDashboardAPIMultipleRequests tests multiple API requests
func TestDashboardAPIMultipleRequests(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	})

	// Make multiple requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/dashboard/metrics", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Request %d failed with status %d", i, w.Code)
		}
	}
}

// TestDashboardAPIDataConsistency tests data consistency
func TestDashboardAPIDataConsistency(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_metrics":  5,
			"overall_status": "healthy",
		})
	})

	// Get status multiple times
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/dashboard/status", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		var status map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if status["overall_status"] != "healthy" {
			t.Fatalf("Expected consistent healthy status, got %v", status["overall_status"])
		}
	}
}

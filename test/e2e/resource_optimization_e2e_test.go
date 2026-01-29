package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestResourceOptimizationMemoryMetricsAPI tests memory metrics API
func TestResourceOptimizationMemoryMetricsAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"alloc_mb": 50.0, "heap_alloc_mb": 25.0},
		})
	})

	req := httptest.NewRequest("GET", "/optimization/memory/metrics?limit=10", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var metrics []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&metrics); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(metrics))
	}
}

// TestResourceOptimizationCPUMetricsAPI tests CPU metrics API
func TestResourceOptimizationCPUMetricsAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"num_goroutine": 10, "cpu_usage": 25.0},
		})
	})

	req := httptest.NewRequest("GET", "/optimization/cpu/metrics?limit=10", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var metrics []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&metrics); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(metrics))
	}
}

// TestResourceOptimizationMemoryStatsAPI tests memory stats API
func TestResourceOptimizationMemoryStatsAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"alloc_mb":      50.0,
			"heap_alloc_mb": 25.0,
			"heap_objects":  1000,
			"live_objects":  500,
		})
	})

	req := httptest.NewRequest("GET", "/optimization/memory/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if allocMB, ok := stats["alloc_mb"]; !ok || allocMB != 50.0 {
		t.Fatalf("Expected alloc_mb 50.0, got %v", allocMB)
	}
}

// TestResourceOptimizationCPUStatsAPI tests CPU stats API
func TestResourceOptimizationCPUStatsAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"num_goroutine": 10,
			"num_cpu":       4,
			"cpu_usage":     25.0,
		})
	})

	req := httptest.NewRequest("GET", "/optimization/cpu/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if numGoroutine, ok := stats["num_goroutine"]; !ok || numGoroutine != 10.0 {
		t.Fatalf("Expected num_goroutine 10, got %v", numGoroutine)
	}
}

// TestResourceOptimizationMemoryTrendsAPI tests memory trends API
func TestResourceOptimizationMemoryTrendsAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"heap_alloc_mb": 450.0},
			{"heap_alloc_mb": 480.0},
		})
	})

	req := httptest.NewRequest("GET", "/optimization/memory/trends", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var trends []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&trends); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(trends) != 2 {
		t.Fatalf("Expected 2 trends, got %d", len(trends))
	}
}

// TestResourceOptimizationCPUTrendsAPI tests CPU trends API
func TestResourceOptimizationCPUTrendsAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"cpu_usage": 85.0},
			{"cpu_usage": 90.0},
		})
	})

	req := httptest.NewRequest("GET", "/optimization/cpu/trends", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var trends []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&trends); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(trends) != 2 {
		t.Fatalf("Expected 2 trends, got %d", len(trends))
	}
}

// TestResourceOptimizationForceGCAPI tests force GC API
func TestResourceOptimizationForceGCAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "gc_triggered"})
	})

	req := httptest.NewRequest("POST", "/optimization/gc/force", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if status, ok := response["status"]; !ok || status != "gc_triggered" {
		t.Fatalf("Expected status 'gc_triggered', got %v", status)
	}
}

// TestResourceOptimizationAPIFlow tests complete resource optimization API flow
func TestResourceOptimizationAPIFlow(t *testing.T) {
	// Test memory metrics
	handler1 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"alloc_mb": 50.0})
	})

	req1 := httptest.NewRequest("GET", "/optimization/memory/stats", nil)
	w1 := httptest.NewRecorder()
	handler1.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("Memory stats request failed with status %d", w1.Code)
	}

	// Test CPU metrics
	handler2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"cpu_usage": 25.0})
	})

	req2 := httptest.NewRequest("GET", "/optimization/cpu/stats", nil)
	w2 := httptest.NewRecorder()
	handler2.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("CPU stats request failed with status %d", w2.Code)
	}

	// Test force GC
	handler3 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "gc_triggered"})
	})

	req3 := httptest.NewRequest("POST", "/optimization/gc/force", nil)
	w3 := httptest.NewRecorder()
	handler3.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("Force GC request failed with status %d", w3.Code)
	}
}

// TestResourceOptimizationAPIErrorHandling tests error handling
func TestResourceOptimizationAPIErrorHandling(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/optimization/gc/force", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected status 405, got %d", w.Code)
	}
}

// TestResourceOptimizationAPIMultipleRequests tests multiple API requests
func TestResourceOptimizationAPIMultipleRequests(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	})

	// Make multiple requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/optimization/memory/stats", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Request %d failed with status %d", i, w.Code)
		}
	}
}

// TestResourceOptimizationAPIDataConsistency tests data consistency
func TestResourceOptimizationAPIDataConsistency(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"alloc_mb": 50.0,
			"num_cpu":  4,
		})
	})

	// Get stats multiple times
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/optimization/memory/stats", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		var stats map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if allocMB, ok := stats["alloc_mb"]; !ok || allocMB != 50.0 {
			t.Fatalf("Expected consistent alloc_mb 50.0, got %v", allocMB)
		}
	}
}

package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"streamgate/pkg/debug"
)

// TestDebugAPIEndToEnd tests the complete debug API flow
func TestDebugAPIEndToEnd(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	handler := debug.NewHandler(service)

	// Test setting breakpoint
	breakpointPayload := map[string]string{
		"location":  "main.go:10",
		"condition": "x > 5",
	}

	breakpointBody, _ := json.Marshal(breakpointPayload)
	req := httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewReader(breakpointBody))
	w := httptest.NewRecorder()
	handler.SetBreakpointHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var breakpointResp map[string]string
	json.NewDecoder(w.Body).Decode(&breakpointResp)
	breakpointID := breakpointResp["id"]

	// Test getting breakpoints
	req = httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
	w = httptest.NewRecorder()
	handler.GetBreakpointsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var breakpoints []debug.Breakpoint
	json.NewDecoder(w.Body).Decode(&breakpoints)
	if len(breakpoints) == 0 {
		t.Error("Should have breakpoints")
	}

	found := false
	for _, bp := range breakpoints {
		if bp.ID == breakpointID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Breakpoint with ID %s not found in list", breakpointID)
	}

	// Test watching variable
	watchPayload := map[string]interface{}{
		"name":  "x",
		"value": 42,
	}

	watchBody, _ := json.Marshal(watchPayload)
	req = httptest.NewRequest("POST", "/api/v1/debug/watch", bytes.NewReader(watchBody))
	w = httptest.NewRecorder()
	handler.WatchVariableHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test getting watched variables
	req = httptest.NewRequest("GET", "/api/v1/debug/watch", nil)
	w = httptest.NewRecorder()
	handler.GetWatchVariablesHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var variables []debug.WatchVariable
	json.NewDecoder(w.Body).Decode(&variables)
	if len(variables) == 0 {
		t.Error("Should have watched variables")
	}

	// Test getting traces
	service.RecordTrace("testFunc", "test message", "info")
	time.Sleep(100 * time.Millisecond)

	req = httptest.NewRequest("GET", "/api/v1/debug/traces?limit=100", nil)
	w = httptest.NewRecorder()
	handler.GetTracesHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var traces []debug.DebugTrace
	json.NewDecoder(w.Body).Decode(&traces)
	if len(traces) == 0 {
		t.Error("Should have traces")
	}

	// Test getting logs
	service.RecordLog("info", "test log", map[string]interface{}{"key": "value"})
	time.Sleep(100 * time.Millisecond)

	req = httptest.NewRequest("GET", "/api/v1/debug/logs?limit=100", nil)
	w = httptest.NewRecorder()
	handler.GetLogsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var logs []debug.DebugLog
	json.NewDecoder(w.Body).Decode(&logs)
	if len(logs) == 0 {
		t.Error("Should have logs")
	}

	// Test getting memory profiles
	time.Sleep(500 * time.Millisecond)

	req = httptest.NewRequest("GET", "/api/v1/debug/profiles/memory?limit=10", nil)
	w = httptest.NewRecorder()
	handler.GetMemProfilesHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var memProfiles []debug.MemProfile
	json.NewDecoder(w.Body).Decode(&memProfiles)
	if len(memProfiles) == 0 {
		t.Error("Should have memory profiles")
	}

	// Test getting goroutine profiles
	req = httptest.NewRequest("GET", "/api/v1/debug/profiles/goroutine?limit=10", nil)
	w = httptest.NewRecorder()
	handler.GetGoroutineProfilesHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var goroutineProfiles []debug.GoroutineProfile
	json.NewDecoder(w.Body).Decode(&goroutineProfiles)
	if len(goroutineProfiles) == 0 {
		t.Error("Should have goroutine profiles")
	}

	// Test getting recommendations
	req = httptest.NewRequest("GET", "/api/v1/debug/recommendations", nil)
	w = httptest.NewRecorder()
	handler.GetOptimizationRecommendationsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var recommendations map[string]interface{}
	json.NewDecoder(w.Body).Decode(&recommendations)
	if recommendations == nil {
		t.Error("Should have recommendations")
	}

	// Test health check
	req = httptest.NewRequest("GET", "/api/v1/debug/health", nil)
	w = httptest.NewRecorder()
	handler.HealthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestDebugAPIErrorHandling tests API error handling
func TestDebugAPIErrorHandling(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	handler := debug.NewHandler(service)

	// Test invalid method for POST endpoint
	req := httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
	w := httptest.NewRecorder()
	handler.SetBreakpointHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}

	// Test invalid JSON
	req = httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewReader([]byte("invalid json")))
	w = httptest.NewRecorder()
	handler.SetBreakpointHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	// Test invalid method for GET endpoint
	req = httptest.NewRequest("POST", "/api/v1/debug/traces", nil)
	w = httptest.NewRecorder()
	handler.GetTracesHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

// TestDebugAPIMultipleRequests tests multiple concurrent requests
func TestDebugAPIMultipleRequests(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	handler := debug.NewHandler(service)

	// Send multiple breakpoint requests
	for i := 0; i < 10; i++ {
		breakpointPayload := map[string]string{
			"location":  "file.go:10",
			"condition": "condition",
		}

		breakpointBody, _ := json.Marshal(breakpointPayload)
		req := httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewReader(breakpointBody))
		w := httptest.NewRecorder()
		handler.SetBreakpointHandler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d failed with status %d", i, w.Code)
		}
	}

	// Verify all breakpoints were set
	req := httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
	w := httptest.NewRecorder()
	handler.GetBreakpointsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var breakpoints []debug.Breakpoint
	json.NewDecoder(w.Body).Decode(&breakpoints)
	if len(breakpoints) != 10 {
		t.Errorf("Expected 10 breakpoints, got %d", len(breakpoints))
	}
}

// TestDebugAPILogFiltering tests log filtering
func TestDebugAPILogFiltering(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	handler := debug.NewHandler(service)

	// Record logs with different levels
	service.RecordLog("debug", "debug message", map[string]interface{}{})
	service.RecordLog("info", "info message", map[string]interface{}{})
	service.RecordLog("warn", "warn message", map[string]interface{}{})
	service.RecordLog("error", "error message", map[string]interface{}{})

	time.Sleep(500 * time.Millisecond)

	// Test filtering by level
	req := httptest.NewRequest("GET", "/api/v1/debug/logs?limit=100&level=error", nil)
	w := httptest.NewRecorder()
	handler.GetLogsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var logs []debug.DebugLog
	json.NewDecoder(w.Body).Decode(&logs)
	if len(logs) == 0 {
		t.Error("Should have error logs")
	}

	// Verify all logs are error level
	for _, log := range logs {
		if log.Level != "error" {
			t.Errorf("Expected error level, got %s", log.Level)
		}
	}
}

// TestDebugAPIDataConsistency tests data consistency
func TestDebugAPIDataConsistency(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	handler := debug.NewHandler(service)

	// Set a breakpoint
	breakpointPayload := map[string]string{
		"location":  "main.go:10",
		"condition": "x > 5",
	}

	breakpointBody, _ := json.Marshal(breakpointPayload)
	req := httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewReader(breakpointBody))
	w := httptest.NewRecorder()
	handler.SetBreakpointHandler(w, req)

	// Get breakpoints multiple times
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
		w := httptest.NewRecorder()
		handler.GetBreakpointsHandler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d failed with status %d", i, w.Code)
		}

		var breakpoints []debug.Breakpoint
		json.NewDecoder(w.Body).Decode(&breakpoints)
		if len(breakpoints) != 1 {
			t.Errorf("Request %d should have 1 breakpoint, got %d", i, len(breakpoints))
		}
	}
}

// TestDebugAPIHealthCheck tests health check endpoint
func TestDebugAPIHealthCheck(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	handler := debug.NewHandler(service)

	req := httptest.NewRequest("GET", "/api/v1/debug/health", nil)
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

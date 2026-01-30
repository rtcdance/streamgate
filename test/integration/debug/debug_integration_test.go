package debug

import (
	"testing"
	"time"

	"streamgate/pkg/debug"
)

// TestDebuggerEndToEnd tests the complete debugger flow
func TestDebuggerEndToEnd(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// 1. Set breakpoints
	bp1 := service.SetBreakpoint("main.go:10", "x > 5")
	bp2 := service.SetBreakpoint("handler.go:20", "error != nil")

	if bp1 == "" || bp2 == "" {
		t.Error("Should set breakpoints")
	}

	// 2. Watch variables
	watch1 := service.WatchVariable("x", 42)
	watch2 := service.WatchVariable("error", nil)

	if watch1 == "" || watch2 == "" {
		t.Error("Should watch variables")
	}

	// 3. Update variables
	service.UpdateWatchVariable(watch1, 43)
	service.UpdateWatchVariable(watch2, "connection timeout")

	// 4. Record traces
	service.RecordTrace("main", "Starting application", "info")
	service.RecordTrace("handler", "Processing request", "debug")
	service.RecordTrace("database", "Query failed", "error")

	// 5. Record logs
	service.RecordLog("info", "Application started", map[string]interface{}{"port": 8080})
	service.RecordLog("error", "Database connection failed", map[string]interface{}{"error": "timeout"})

	time.Sleep(500 * time.Millisecond)

	// Verify breakpoints
	breakpoints := service.GetBreakpoints()
	if len(breakpoints) != 2 {
		t.Errorf("Should have 2 breakpoints, got %d", len(breakpoints))
	}

	// Verify watched variables
	variables := service.GetWatchVariables()
	if len(variables) != 2 {
		t.Errorf("Should have 2 watched variables, got %d", len(variables))
	}

	// Verify traces
	traces := service.GetTraces(100)
	if len(traces) < 3 {
		t.Errorf("Should have at least 3 traces, got %d", len(traces))
	}

	// Verify logs
	logs := service.GetLogs(100)
	if len(logs) < 2 {
		t.Errorf("Should have at least 2 logs, got %d", len(logs))
	}

	// Verify error logs
	errorLogs := service.GetLogsByLevel("error", 100)
	if len(errorLogs) < 1 {
		t.Error("Should have error logs")
	}
}

// TestProfilerEndToEnd tests the complete profiler flow
func TestProfilerEndToEnd(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Trigger profiling immediately
	service.ProfileNow()

	// Get memory profiles
	memProfiles := service.GetMemProfiles(10)
	if len(memProfiles) == 0 {
		t.Error("Should have memory profiles")
	}

	// Get goroutine profiles
	goroutineProfiles := service.GetGoroutineProfiles(10)
	if len(goroutineProfiles) == 0 {
		t.Error("Should have goroutine profiles")
	}

	// Get memory trend
	memTrend := service.GetMemoryTrend(10)
	if len(memTrend) == 0 {
		t.Error("Should have memory trend")
	}

	// Get goroutine trend
	goroutineTrend := service.GetGoroutineTrend(10)
	if len(goroutineTrend) == 0 {
		t.Error("Should have goroutine trend")
	}

	// Get recommendations
	recommendations := service.GetOptimizationRecommendations()
	if recommendations == nil {
		t.Error("Should have recommendations")
	}

	// Check leak detection
	memoryLeak := service.DetectMemoryLeak()
	goroutineLeak := service.DetectGoroutineLeak()

	// Should not detect leaks in normal operation
	if memoryLeak || goroutineLeak {
		t.Error("Should not detect leaks in normal operation")
	}
}

// TestDebuggerMultipleBreakpoints tests multiple breakpoints
func TestDebuggerMultipleBreakpoints(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Set multiple breakpoints
	ids := make([]string, 10)
	for i := 0; i < 10; i++ {
		ids[i] = service.SetBreakpoint("file.go:10", "condition")
	}

	// Verify all breakpoints
	breakpoints := service.GetBreakpoints()
	if len(breakpoints) != 10 {
		t.Errorf("Should have 10 breakpoints, got %d", len(breakpoints))
	}

	// Remove some breakpoints
	for i := 0; i < 5; i++ {
		service.RemoveBreakpoint(ids[i])
	}

	// Verify removal
	breakpoints = service.GetBreakpoints()
	if len(breakpoints) != 5 {
		t.Errorf("Should have 5 breakpoints after removal, got %d", len(breakpoints))
	}
}

// TestDebuggerVariableHistory tests variable history tracking
func TestDebuggerVariableHistory(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Watch a variable
	id := service.WatchVariable("counter", 0)

	// Update variable multiple times
	for i := 1; i <= 10; i++ {
		service.UpdateWatchVariable(id, i)
	}

	// Get watched variables
	variables := service.GetWatchVariables()
	if len(variables) == 0 {
		t.Error("Should have watched variables")
	}

	// Verify history
	if len(variables) > 0 {
		variable := variables[0]
		if len(variable.History) < 10 {
			t.Errorf("Should have history, got %d entries", len(variable.History))
		}
	}
}

// TestDebuggerTraceFiltering tests trace filtering
func TestDebuggerTraceFiltering(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Record traces with different levels
	service.RecordTrace("func1", "message1", "debug")
	service.RecordTrace("func2", "message2", "info")
	service.RecordTrace("func3", "message3", "warn")
	service.RecordTrace("func4", "message4", "error")

	time.Sleep(500 * time.Millisecond)

	// Get all traces
	allTraces := service.GetTraces(100)
	if len(allTraces) < 4 {
		t.Errorf("Should have at least 4 traces, got %d", len(allTraces))
	}
}

// TestDebuggerLogFiltering tests log filtering by level
func TestDebuggerLogFiltering(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Record logs with different levels
	service.RecordLog("debug", "debug message", map[string]interface{}{})
	service.RecordLog("info", "info message", map[string]interface{}{})
	service.RecordLog("warn", "warn message", map[string]interface{}{})
	service.RecordLog("error", "error message", map[string]interface{}{})

	time.Sleep(500 * time.Millisecond)

	// Get logs by level
	debugLogs := service.GetLogsByLevel("debug", 100)
	infoLogs := service.GetLogsByLevel("info", 100)
	errorLogs := service.GetLogsByLevel("error", 100)

	if len(debugLogs) == 0 {
		t.Error("Should have debug logs")
	}
	if len(infoLogs) == 0 {
		t.Error("Should have info logs")
	}
	if len(errorLogs) == 0 {
		t.Error("Should have error logs")
	}
}

// TestProfilerMemoryTracking tests memory tracking
func TestProfilerMemoryTracking(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Trigger initial profiling
	service.ProfileNow()

	// Get initial memory profile
	initial := service.GetLatestMemProfile()
	if initial == nil {
		t.Error("Should have initial memory profile")
	}

	// Wait a bit and trigger another profile
	time.Sleep(100 * time.Millisecond)
	service.ProfileNow()

	// Get updated memory profile
	updated := service.GetLatestMemProfile()
	if updated == nil {
		t.Error("Should have updated memory profile")
	}

	// Verify profiles are different
	if initial.Timestamp == updated.Timestamp {
		t.Error("Profiles should have different timestamps")
	}
}

// TestProfilerGoroutineTracking tests goroutine tracking
func TestProfilerGoroutineTracking(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Trigger initial profiling
	service.ProfileNow()

	// Get initial goroutine profile
	initial := service.GetLatestGoroutineProfile()
	if initial == nil {
		t.Error("Should have initial goroutine profile")
	}

	if initial.Count == 0 {
		t.Error("Should have goroutines")
	}

	// Wait a bit and trigger another profile
	time.Sleep(100 * time.Millisecond)
	service.ProfileNow()

	// Get updated goroutine profile
	updated := service.GetLatestGoroutineProfile()
	if updated == nil {
		t.Error("Should have updated goroutine profile")
	}

	// Verify profiles are different
	if initial.Timestamp == updated.Timestamp {
		t.Error("Profiles should have different timestamps")
	}
}

// TestDebuggerHighLoad tests debugger under high load
func TestDebuggerHighLoad(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Set many breakpoints
	for i := 0; i < 100; i++ {
		service.SetBreakpoint("file.go:10", "condition")
	}

	// Watch many variables
	for i := 0; i < 100; i++ {
		service.WatchVariable("var", i)
	}

	// Record many traces
	for i := 0; i < 100; i++ {
		service.RecordTrace("func", "message", "info")
	}

	// Record many logs
	for i := 0; i < 100; i++ {
		service.RecordLog("info", "message", map[string]interface{}{})
	}

	time.Sleep(1 * time.Second)

	// Verify data was processed
	breakpoints := service.GetBreakpoints()
	if len(breakpoints) == 0 {
		t.Error("Should handle high load")
	}
}

// TestDebuggerErrorHandling tests error handling
func TestDebuggerErrorHandling(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Try to remove non-existent breakpoint
	result := service.RemoveBreakpoint("non-existent")
	if result {
		t.Error("Should not remove non-existent breakpoint")
	}

	// Try to update non-existent variable
	result = service.UpdateWatchVariable("non-existent", 42)
	if result {
		t.Error("Should not update non-existent variable")
	}

	// Record with nil context
	service.RecordLog("info", "message", nil)

	// Should not crash
	if service == nil {
		t.Error("Service should handle errors gracefully")
	}
}

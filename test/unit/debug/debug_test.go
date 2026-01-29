package debug

import (
	"testing"
	"time"

	"streamgate/pkg/debug"
)

// TestDebugger tests the debugger
func TestDebugger(t *testing.T) {
	debugger := debug.NewDebugger()
	defer debugger.Close()

	// Set a breakpoint
	id := debugger.SetBreakpoint("main.go:10", "x > 5")
	if id == "" {
		t.Error("Breakpoint ID should not be empty")
	}

	// Get breakpoints
	breakpoints := debugger.GetBreakpoints()
	if len(breakpoints) == 0 {
		t.Error("Should have at least one breakpoint")
	}

	// Remove breakpoint
	if !debugger.RemoveBreakpoint(id) {
		t.Error("Should successfully remove breakpoint")
	}

	// Verify removal
	breakpoints = debugger.GetBreakpoints()
	if len(breakpoints) != 0 {
		t.Error("Should have no breakpoints after removal")
	}
}

// TestWatchVariable tests variable watching
func TestWatchVariable(t *testing.T) {
	debugger := debug.NewDebugger()
	defer debugger.Close()

	// Watch a variable
	id := debugger.WatchVariable("x", 42)
	if id == "" {
		t.Error("Watch variable ID should not be empty")
	}

	// Get watched variables
	variables := debugger.GetWatchVariables()
	if len(variables) == 0 {
		t.Error("Should have at least one watched variable")
	}

	// Update variable
	if !debugger.UpdateWatchVariable(id, 43) {
		t.Error("Should successfully update variable")
	}

	// Verify update
	variables = debugger.GetWatchVariables()
	if len(variables) > 0 && variables[0].Value != 43 {
		t.Error("Variable value should be updated")
	}
}

// TestDebugTrace tests debug tracing
func TestDebugTrace(t *testing.T) {
	debugger := debug.NewDebugger()
	defer debugger.Close()

	// Record a trace
	debugger.RecordTrace("testFunc", "test message", "info")

	// Get traces
	traces := debugger.GetTraces(10)
	if len(traces) == 0 {
		t.Error("Should have at least one trace")
	}

	if traces[0].Message != "test message" {
		t.Error("Trace message should match")
	}
}

// TestDebugLog tests debug logging
func TestDebugLog(t *testing.T) {
	debugger := debug.NewDebugger()
	defer debugger.Close()

	// Record a log
	debugger.RecordLog("info", "test log", map[string]interface{}{"key": "value"})

	// Get logs
	logs := debugger.GetLogs(10)
	if len(logs) == 0 {
		t.Error("Should have at least one log")
	}

	if logs[0].Message != "test log" {
		t.Error("Log message should match")
	}

	// Get logs by level
	infoLogs := debugger.GetLogsByLevel("info", 10)
	if len(infoLogs) == 0 {
		t.Error("Should have at least one info log")
	}
}

// TestProfiler tests the profiler
func TestProfiler(t *testing.T) {
	profiler := debug.NewProfiler()
	defer profiler.Close()

	// Give profiler time to collect profiles
	time.Sleep(100 * time.Millisecond)

	// Get memory profiles
	memProfiles := profiler.GetMemProfiles(10)
	if len(memProfiles) == 0 {
		t.Error("Should have at least one memory profile")
	}

	// Get latest memory profile
	latest := profiler.GetLatestMemProfile()
	if latest == nil {
		t.Error("Should have a latest memory profile")
	}

	if latest.Goroutines == 0 {
		t.Error("Goroutine count should be greater than 0")
	}
}

// TestGoroutineProfile tests goroutine profiling
func TestGoroutineProfile(t *testing.T) {
	profiler := debug.NewProfiler()
	defer profiler.Close()

	// Give profiler time to collect profiles
	time.Sleep(100 * time.Millisecond)

	// Get goroutine profiles
	profiles := profiler.GetGoroutineProfiles(10)
	if len(profiles) == 0 {
		t.Error("Should have at least one goroutine profile")
	}

	// Get latest goroutine profile
	latest := profiler.GetLatestGoroutineProfile()
	if latest == nil {
		t.Error("Should have a latest goroutine profile")
	}

	if latest.Count == 0 {
		t.Error("Goroutine count should be greater than 0")
	}
}

// TestMemoryTrend tests memory trend
func TestMemoryTrend(t *testing.T) {
	profiler := debug.NewProfiler()
	defer profiler.Close()

	// Give profiler time to collect profiles
	time.Sleep(100 * time.Millisecond)

	// Get memory trend
	trend := profiler.GetMemoryTrend(10)
	if len(trend) == 0 {
		t.Error("Should have memory trend data")
	}
}

// TestGoroutineTrend tests goroutine trend
func TestGoroutineTrend(t *testing.T) {
	profiler := debug.NewProfiler()
	defer profiler.Close()

	// Give profiler time to collect profiles
	time.Sleep(100 * time.Millisecond)

	// Get goroutine trend
	trend := profiler.GetGoroutineTrend(10)
	if len(trend) == 0 {
		t.Error("Should have goroutine trend data")
	}
}

// TestDebugService tests the debug service
func TestDebugService(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Set breakpoint
	id := service.SetBreakpoint("main.go:10", "x > 5")
	if id == "" {
		t.Error("Breakpoint ID should not be empty")
	}

	// Watch variable
	watchID := service.WatchVariable("x", 42)
	if watchID == "" {
		t.Error("Watch variable ID should not be empty")
	}

	// Record trace
	service.RecordTrace("testFunc", "test message", "info")

	// Record log
	service.RecordLog("info", "test log", map[string]interface{}{"key": "value"})

	// Get data
	breakpoints := service.GetBreakpoints()
	if len(breakpoints) == 0 {
		t.Error("Should have breakpoints")
	}

	variables := service.GetWatchVariables()
	if len(variables) == 0 {
		t.Error("Should have watched variables")
	}

	traces := service.GetTraces(10)
	if len(traces) == 0 {
		t.Error("Should have traces")
	}

	logs := service.GetLogs(10)
	if len(logs) == 0 {
		t.Error("Should have logs")
	}

	// Get profiles
	memProfiles := service.GetMemProfiles(10)
	if memProfiles == nil {
		t.Error("Should have memory profiles")
	}

	goroutineProfiles := service.GetGoroutineProfiles(10)
	if goroutineProfiles == nil {
		t.Error("Should have goroutine profiles")
	}

	// Get recommendations
	recommendations := service.GetOptimizationRecommendations()
	if recommendations == nil {
		t.Error("Should have recommendations")
	}
}

// TestLeakDetection tests leak detection
func TestLeakDetection(t *testing.T) {
	profiler := debug.NewProfiler()
	defer profiler.Close()

	// Give profiler time to collect profiles
	time.Sleep(100 * time.Millisecond)

	// Check for leaks
	memoryLeak := profiler.DetectMemoryLeak()
	goroutineLeak := profiler.DetectGoroutineLeak()

	// These should be false for a normal test
	if memoryLeak {
		t.Error("Should not detect memory leak in normal operation")
	}

	if goroutineLeak {
		t.Error("Should not detect goroutine leak in normal operation")
	}
}

// TestOptimizationRecommendations tests optimization recommendations
func TestOptimizationRecommendations(t *testing.T) {
	service := debug.NewService()
	defer service.Close()

	// Get recommendations
	recommendations := service.GetOptimizationRecommendations()
	if recommendations == nil {
		t.Error("Should have recommendations")
	}

	// Should be empty or have valid recommendations
	for _, rec := range recommendations {
		if rec == "" {
			t.Error("Recommendation should not be empty")
		}
	}
}

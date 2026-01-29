# StreamGate Debugging & Profiling Guide

**Date**: 2025-01-28  
**Status**: Debugging & Profiling Implementation  
**Version**: 1.0.0

## Overview

StreamGate includes comprehensive debugging and profiling capabilities for faster issue resolution and performance optimization. The system provides breakpoints, variable watching, trace collection, and continuous profiling.

## Architecture

### Components

1. **Debugger** - Breakpoints, variable watching, trace collection
2. **Profiler** - Memory, CPU, goroutine, and block profiling
3. **Debug Service** - Orchestrates debugging and profiling
4. **HTTP Handler** - Provides REST API for debugging

### Data Flow

```
Application
   │
   ├─> SetBreakpoint
   ├─> WatchVariable
   ├─> RecordTrace
   ├─> RecordLog
   └─> Profile System
        │
        ▼
   Debug Service
        │
        ├─> Debugger (breakpoints, traces, logs)
        └─> Profiler (memory, CPU, goroutines)
        │
        ▼
   HTTP API / IDE Integration
```

## Debugging

### Breakpoints

```go
import "streamgate/pkg/debug"

// Create debug service
service := debug.NewService()
defer service.Close()

// Set a breakpoint
id := service.SetBreakpoint("main.go:10", "x > 5")

// Get all breakpoints
breakpoints := service.GetBreakpoints()

// Remove breakpoint
service.RemoveBreakpoint(id)
```

### Variable Watching

```go
// Watch a variable
id := service.WatchVariable("x", 42)

// Update variable value
service.UpdateWatchVariable(id, 43)

// Get all watched variables
variables := service.GetWatchVariables()
for _, v := range variables {
    fmt.Printf("Variable: %s = %v (type: %s)\n", v.Name, v.Value, v.Type)
}
```

### Trace Collection

```go
// Record a trace
service.RecordTrace("myFunction", "Processing started", "info")

// Get traces
traces := service.GetTraces(100)
for _, trace := range traces {
    fmt.Printf("[%s] %s:%d - %s\n", trace.Level, trace.File, trace.Line, trace.Message)
}
```

### Debug Logging

```go
// Record a log
service.RecordLog("info", "User logged in", map[string]interface{}{
    "user_id": "user123",
    "ip": "192.168.1.1",
})

// Get logs
logs := service.GetLogs(100)

// Get logs by level
errorLogs := service.GetLogsByLevel("error", 50)
```

## Profiling

### Memory Profiling

```go
// Get memory profiles
profiles := service.GetMemProfiles(10)
for _, profile := range profiles {
    fmt.Printf("Memory: %d MB, Goroutines: %d\n", 
        profile.Alloc/1024/1024, profile.Goroutines)
}

// Get latest memory profile
latest := service.GetLatestMemProfile()
if latest != nil {
    fmt.Printf("Current memory: %d MB\n", latest.Alloc/1024/1024)
}

// Get memory trend
trend := service.GetMemoryTrend(100)
```

### Goroutine Profiling

```go
// Get goroutine profiles
profiles := service.GetGoroutineProfiles(10)
for _, profile := range profiles {
    fmt.Printf("Goroutines: %d\n", profile.Count)
}

// Get latest goroutine profile
latest := service.GetLatestGoroutineProfile()
if latest != nil {
    fmt.Printf("Current goroutines: %d\n", latest.Count)
}

// Get goroutine trend
trend := service.GetGoroutineTrend(100)
```

### CPU Profiling

```go
// Get CPU profiles
profiles := service.GetCPUProfiles(10)
for _, profile := range profiles {
    fmt.Printf("CPU Profile: %d samples in %v\n", 
        profile.Samples, profile.Duration)
    for _, fn := range profile.TopFunctions {
        fmt.Printf("  %s: %d (%.2f%%)\n", fn.Function, fn.Count, fn.Percent)
    }
}
```

### Block Profiling

```go
// Get block profiles
profiles := service.GetBlockProfiles(10)
for _, profile := range profiles {
    fmt.Printf("Block Contention: %v\n", profile.Contention)
}
```

## Leak Detection

### Memory Leak Detection

```go
// Detect memory leaks
if service.DetectMemoryLeak() {
    fmt.Println("WARNING: Potential memory leak detected!")
}
```

### Goroutine Leak Detection

```go
// Detect goroutine leaks
if service.DetectGoroutineLeak() {
    fmt.Println("WARNING: Potential goroutine leak detected!")
}
```

## Optimization Recommendations

```go
// Get optimization recommendations
recommendations := service.GetOptimizationRecommendations()
for _, rec := range recommendations {
    fmt.Println("Recommendation:", rec)
}
```

## HTTP API

### Set Breakpoint

```bash
POST /api/v1/debug/breakpoints
Content-Type: application/json

{
  "location": "main.go:10",
  "condition": "x > 5"
}
```

Response:
```json
{
  "id": "bp-123"
}
```

### Get Breakpoints

```bash
GET /api/v1/debug/breakpoints
```

Response:
```json
[
  {
    "id": "bp-123",
    "location": "main.go:10",
    "condition": "x > 5",
    "hit_count": 0,
    "enabled": true,
    "created": "2025-01-28T10:00:00Z"
  }
]
```

### Watch Variable

```bash
POST /api/v1/debug/watch
Content-Type: application/json

{
  "name": "x",
  "value": 42
}
```

Response:
```json
{
  "id": "watch-123"
}
```

### Get Watched Variables

```bash
GET /api/v1/debug/watch
```

Response:
```json
[
  {
    "id": "watch-123",
    "name": "x",
    "value": 43,
    "type": "int",
    "updated": "2025-01-28T10:05:00Z",
    "history": [42, 43]
  }
]
```

### Get Traces

```bash
GET /api/v1/debug/traces?limit=100
```

Response:
```json
[
  {
    "id": "trace-123",
    "timestamp": "2025-01-28T10:05:00Z",
    "function": "myFunction",
    "file": "main.go",
    "line": 10,
    "message": "Processing started",
    "level": "info",
    "stack": ["main.main", "runtime.main"]
  }
]
```

### Get Logs

```bash
GET /api/v1/debug/logs?limit=100&level=error
```

Response:
```json
[
  {
    "id": "log-123",
    "timestamp": "2025-01-28T10:05:00Z",
    "level": "error",
    "message": "Database connection failed",
    "context": {
      "error": "connection timeout",
      "retry_count": 3
    }
  }
]
```

### Get Memory Profiles

```bash
GET /api/v1/debug/profiles/memory?limit=10
```

Response:
```json
[
  {
    "id": "mem-123",
    "timestamp": "2025-01-28T10:05:00Z",
    "alloc": 1073741824,
    "total_alloc": 5368709120,
    "sys": 1610612736,
    "num_gc": 42,
    "goroutines": 150
  }
]
```

### Get Goroutine Profiles

```bash
GET /api/v1/debug/profiles/goroutine?limit=10
```

Response:
```json
[
  {
    "id": "gr-123",
    "timestamp": "2025-01-28T10:05:00Z",
    "count": 150,
    "running": 10,
    "blocked": 5,
    "waiting": 135,
    "stacks": ["goroutine 1 [running]..."]
  }
]
```

### Get Optimization Recommendations

```bash
GET /api/v1/debug/recommendations
```

Response:
```json
{
  "recommendations": [
    "Potential memory leak detected - review memory allocation patterns",
    "High memory usage: 1024 MB - consider optimization"
  ],
  "memory_leak": true,
  "goroutine_leak": false
}
```

## IDE Integration

### VSCode Integration

1. Install Go extension
2. Configure launch.json:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Connect to StreamGate",
      "type": "go",
      "request": "attach",
      "mode": "local",
      "port": 2345,
      "host": "127.0.0.1"
    }
  ]
}
```

### GoLand Integration

1. Go to Run → Edit Configurations
2. Add new "Go Remote" configuration
3. Set host to localhost and port to 2345

## Best Practices

1. **Use Breakpoints Wisely** - Set breakpoints only on critical paths
2. **Monitor Memory** - Regularly check memory profiles for leaks
3. **Track Goroutines** - Monitor goroutine count for leaks
4. **Review Logs** - Regularly review debug logs for issues
5. **Act on Recommendations** - Implement optimization recommendations
6. **Clean Up** - Remove breakpoints and watches when done

## Performance Considerations

- **Profiling Overhead** - ~5% CPU overhead
- **Memory Overhead** - ~10MB for profiling data
- **Trace Collection** - ~1ms per trace
- **Log Recording** - ~0.5ms per log

## Troubleshooting

### Breakpoints Not Hitting

- Ensure breakpoint location is correct
- Check breakpoint condition
- Verify code is compiled with debug symbols

### High Memory Usage

- Check for memory leaks
- Review memory profiles
- Implement recommendations

### High Goroutine Count

- Check for goroutine leaks
- Review goroutine profiles
- Ensure goroutines are properly cleaned up

### Slow Performance

- Check CPU profiles
- Review block profiles
- Implement optimization recommendations

## Future Enhancements

- [ ] Remote debugging
- [ ] Conditional breakpoints
- [ ] Watch expressions
- [ ] Call stack visualization
- [ ] Performance flamegraphs
- [ ] Memory allocation tracking
- [ ] Lock contention analysis
- [ ] Custom profilers

---

**Document Status**: Debugging & Profiling Guide  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0

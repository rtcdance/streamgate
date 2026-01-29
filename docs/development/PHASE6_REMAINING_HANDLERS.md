# StreamGate - Phase 6 Remaining Handlers Integration

**Date**: 2025-01-28  
**Status**: Quick Reference for Remaining 3 Handlers

## Overview

This document provides a quick reference for integrating the remaining 3 handlers with Phase 6 monitoring, security, and caching features.

## Remaining Handlers

1. **Auth Handler** (`pkg/plugins/auth/handler.go`)
2. **Worker Handler** (`pkg/plugins/worker/handler.go`)
3. **Transcoder Handler** (`pkg/plugins/transcoder/handler.go`)
4. **Monitor Handler** (`pkg/plugins/monitor/handler.go`)

## Integration Template

### Step 1: Update Imports

```go
import (
    "time"
    "github.com/yourusername/streamgate/pkg/monitoring"
    "github.com/yourusername/streamgate/pkg/optimization"
    "github.com/yourusername/streamgate/pkg/security"
)
```

### Step 2: Add Fields to Handler Struct

```go
type YourHandler struct {
    // ... existing fields ...
    metricsCollector  *monitoring.MetricsCollector
    rateLimiter       *security.RateLimiter
    auditLogger       *security.AuditLogger
    cache             *optimization.LocalCache  // optional
}
```

### Step 3: Initialize in Constructor

```go
func NewYourHandler(...) *YourHandler {
    return &YourHandler{
        // ... existing fields ...
        metricsCollector:  monitoring.NewMetricsCollector(logger),
        rateLimiter:       security.NewRateLimiter(capacity, refillRate, time.Second, logger),
        auditLogger:       security.NewAuditLogger(logger),
        cache:             optimization.NewLocalCache(maxSize, ttl, logger), // optional
    }
}
```

### Step 4: Add to Each Endpoint Handler

```go
func (h *YourHandler) YourEndpointHandler(w http.ResponseWriter, r *http.Request) {
    startTime := time.Now()
    clientIP := r.RemoteAddr

    // Check method
    if r.Method != http.MethodPost {
        h.metricsCollector.IncrementCounter("endpoint_invalid_method", map[string]string{})
        w.WriteHeader(http.StatusMethodNotAllowed)
        json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
        return
    }

    // Check rate limit
    if !h.rateLimiter.Allow(clientIP) {
        h.metricsCollector.IncrementCounter("endpoint_rate_limit_exceeded", map[string]string{})
        w.WriteHeader(http.StatusTooManyRequests)
        json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
        return
    }

    // Check cache (optional)
    cacheKey := "endpoint:" + someID
    if cached, ok := h.cache.Get(cacheKey); ok {
        h.metricsCollector.IncrementCounter("endpoint_cache_hit", map[string]string{})
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(cached)
        return
    }

    // Process request
    result, err := h.processRequest(r)
    if err != nil {
        h.metricsCollector.IncrementCounter("endpoint_failed", map[string]string{})
        h.auditLogger.LogEvent("endpoint", clientIP, "action", "resource", "failed", map[string]interface{}{"error": err.Error()})
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
        return
    }

    // Record success metrics
    h.metricsCollector.IncrementCounter("endpoint_success", map[string]string{})
    h.metricsCollector.RecordTimer("endpoint_latency", time.Since(startTime), map[string]string{})
    h.auditLogger.LogEvent("endpoint", clientIP, "action", "resource", "success", nil)

    // Cache result (optional)
    h.cache.Set(cacheKey, result)

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(result)
}
```

## Handler-Specific Configuration

### Auth Handler

**Location**: `pkg/plugins/auth/handler.go`

**Rate Limiting**:
- Capacity: 50 requests
- Refill Rate: 5 requests/second
- Reason: Authentication is security-sensitive

**Caching**: Not recommended (security-sensitive)

**Metrics to Collect**:
- `auth_login_success/failed`
- `auth_logout_success`
- `auth_verify_success/failed`
- `auth_rate_limit_exceeded`

**Audit Events**:
- Login attempts (success/failed)
- Logout events
- Verification attempts
- Rate limit violations

**Endpoints to Integrate**:
- Login handler
- Logout handler
- Verify handler
- Refresh handler

### Worker Handler

**Location**: `pkg/plugins/worker/handler.go`

**Rate Limiting**:
- Capacity: 100 requests
- Refill Rate: 10 requests/second
- Reason: Background job submission

**Caching**: Optional (job status)

**Metrics to Collect**:
- `job_submit_success/failed`
- `job_status_success`
- `job_cancel_success/failed`
- `job_list_success`

**Audit Events**:
- Job submissions
- Job cancellations
- Job status checks
- Rate limit violations

**Endpoints to Integrate**:
- Submit job handler
- Get job status handler
- Cancel job handler
- List jobs handler

### Transcoder Handler

**Location**: `pkg/plugins/transcoder/handler.go`

**Rate Limiting**:
- Capacity: 50 requests
- Refill Rate: 5 requests/second
- Reason: Resource-intensive operations

**Caching**: Optional (transcoding profiles)

**Metrics to Collect**:
- `transcode_start_success/failed`
- `transcode_status_success`
- `transcode_cancel_success/failed`
- `transcode_queue_size`

**Audit Events**:
- Transcoding jobs started
- Transcoding jobs cancelled
- Status checks
- Rate limit violations

**Endpoints to Integrate**:
- Start transcoding handler
- Get transcoding status handler
- Cancel transcoding handler
- Get queue status handler

### Monitor Handler

**Location**: `pkg/plugins/monitor/handler.go`

**Rate Limiting**:
- Capacity: 1000 requests
- Refill Rate: 100 requests/second
- Reason: Monitoring is read-heavy

**Caching**: Recommended (metrics, health status)

**Metrics to Collect**:
- `monitor_metrics_success`
- `monitor_health_success`
- `monitor_alerts_success`
- `monitor_logs_success`

**Audit Events**:
- Metrics queries
- Health checks
- Alert queries
- Log queries

**Endpoints to Integrate**:
- Get metrics handler
- Get health handler
- Get alerts handler
- Get logs handler

## Quick Integration Checklist

For each handler:

- [ ] Add imports (monitoring, security, optimization)
- [ ] Add fields to struct
- [ ] Initialize in constructor
- [ ] Add rate limit check to each endpoint
- [ ] Add metrics collection to each endpoint
- [ ] Add audit logging to sensitive operations
- [ ] Add caching (if applicable)
- [ ] Test with diagnostics
- [ ] Verify no errors

## Testing Each Handler

```bash
# Test Auth handler
go test ./pkg/plugins/auth -run TestAuth

# Test Worker handler
go test ./pkg/plugins/worker -run TestWorker

# Test Transcoder handler
go test ./pkg/plugins/transcoder -run TestTranscoder

# Test Monitor handler
go test ./pkg/plugins/monitor -run TestMonitor
```

## Performance Targets

### Auth Handler
- Response time: < 100ms
- Cache hit rate: N/A (no caching)
- Rate limit: 50 req/sec

### Worker Handler
- Response time: < 500ms
- Cache hit rate: > 80% (for status checks)
- Rate limit: 100 req/sec

### Transcoder Handler
- Response time: < 1000ms
- Cache hit rate: > 90% (for profiles)
- Rate limit: 50 req/sec

### Monitor Handler
- Response time: < 200ms
- Cache hit rate: > 80% (for metrics)
- Rate limit: 1000 req/sec

## Estimated Time

- Auth handler: 30-45 minutes
- Worker handler: 30-45 minutes
- Transcoder handler: 30-45 minutes
- Monitor handler: 30-45 minutes
- **Total**: 2-3 hours

## Next Steps After Integration

1. Run diagnostics on all handlers
2. Run unit tests
3. Run integration tests
4. Create Prometheus exporter
5. Create Grafana dashboard
6. Performance testing
7. Security audit
8. Production deployment

---

**Status**: Ready for integration
**Estimated Completion**: 2-3 hours
**Quality Target**: 100% diagnostics pass rate


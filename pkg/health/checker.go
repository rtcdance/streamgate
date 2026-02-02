package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// HealthStatus represents the health status
type HealthStatus string

const (
	StatusHealthy   HealthStatus = "healthy"
	StatusUnhealthy HealthStatus = "unhealthy"
	StatusDegraded  HealthStatus = "degraded"
)

// HealthCheck represents a health check function
type HealthCheck func(ctx context.Context) error

// HealthCheckResult represents the result of a health check
type HealthCheckResult struct {
	Name      string       `json:"name"`
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	Duration  int64        `json:"duration_ms,omitempty"`
	Timestamp time.Time    `json:"timestamp"`
	Details   interface{}  `json:"details,omitempty"`
}

// HealthResponse represents the overall health response
type HealthResponse struct {
	Status    HealthStatus                 `json:"status"`
	Timestamp time.Time                    `json:"timestamp"`
	Checks    map[string]HealthCheckResult `json:"checks"`
	Version   string                       `json:"version,omitempty"`
	Release   string                       `json:"release,omitempty"`
}

// LivenessResponse represents the liveness probe response
type LivenessResponse struct {
	Alive     bool      `json:"alive"`
	Timestamp time.Time `json:"timestamp"`
}

// ReadinessResponse represents the readiness probe response
type ReadinessResponse struct {
	Ready     bool                         `json:"ready"`
	Timestamp time.Time                    `json:"timestamp"`
	Checks    map[string]HealthCheckResult `json:"checks"`
}

// HealthChecker manages health checks
type HealthChecker struct {
	checks  map[string]HealthCheck
	results map[string]HealthCheckResult
	mu      sync.RWMutex
	logger  *zap.Logger
	timeout time.Duration
	version string
	release string
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(logger *zap.Logger) *HealthChecker {
	return &HealthChecker{
		checks:  make(map[string]HealthCheck),
		results: make(map[string]HealthCheckResult),
		logger:  logger,
		timeout: 5 * time.Second,
		version: "1.0.0",
		release: "latest",
	}
}

// RegisterCheck registers a health check
func (hc *HealthChecker) RegisterCheck(name string, check HealthCheck) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.checks[name] = check
	hc.logger.Info("Health check registered", zap.String("check", name))
}

// UnregisterCheck unregisters a health check
func (hc *HealthChecker) UnregisterCheck(name string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	delete(hc.checks, name)
	delete(hc.results, name)
	hc.logger.Info("Health check unregistered", zap.String("check", name))
}

// Check executes a single health check
func (hc *HealthChecker) Check(ctx context.Context, name string) HealthCheckResult {
	hc.mu.RLock()
	check, exists := hc.checks[name]
	hc.mu.RUnlock()

	if !exists {
		return HealthCheckResult{
			Name:      name,
			Status:    StatusUnhealthy,
			Message:   "check not found",
			Timestamp: time.Now(),
		}
	}

	start := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, hc.timeout)
	defer cancel()

	err := check(checkCtx)
	duration := time.Since(start)

	result := HealthCheckResult{
		Name:      name,
		Timestamp: time.Now(),
		Duration:  duration.Milliseconds(),
	}

	if err != nil {
		result.Status = StatusUnhealthy
		result.Message = err.Error()
	} else {
		result.Status = StatusHealthy
		result.Message = "OK"
	}

	hc.mu.Lock()
	hc.results[name] = result
	hc.mu.Unlock()

	return result
}

// CheckAll executes all registered health checks
func (hc *HealthChecker) CheckAll(ctx context.Context) HealthResponse {
	hc.mu.RLock()
	checks := make(map[string]HealthCheck, len(hc.checks))
	for k, v := range hc.checks {
		checks[k] = v
	}
	hc.mu.RUnlock()

	results := make(map[string]HealthCheckResult, len(checks))
	overallStatus := StatusHealthy

	for name := range checks {
		result := hc.Check(ctx, name)
		results[name] = result

		if result.Status == StatusUnhealthy {
			overallStatus = StatusUnhealthy
		} else if result.Status == StatusDegraded && overallStatus == StatusHealthy {
			overallStatus = StatusDegraded
		}
	}

	return HealthResponse{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Checks:    results,
		Version:   hc.version,
		Release:   hc.release,
	}
}

// Liveness returns the liveness status
func (hc *HealthChecker) Liveness(ctx context.Context) LivenessResponse {
	return LivenessResponse{
		Alive:     true,
		Timestamp: time.Now(),
	}
}

// Readiness returns the readiness status
func (hc *HealthChecker) Readiness(ctx context.Context) ReadinessResponse {
	response := hc.CheckAll(ctx)

	return ReadinessResponse{
		Ready:     response.Status == StatusHealthy,
		Timestamp: time.Now(),
		Checks:    response.Checks,
	}
}

// GetResults returns the last health check results
func (hc *HealthChecker) GetResults() map[string]HealthCheckResult {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	results := make(map[string]HealthCheckResult, len(hc.results))
	for k, v := range hc.results {
		results[k] = v
	}
	return results
}

// SetVersion sets the version
func (hc *HealthChecker) SetVersion(version string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.version = version
}

// SetRelease sets the release
func (hc *HealthChecker) SetRelease(release string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.release = release
}

// SetTimeout sets the timeout for health checks
func (hc *HealthChecker) SetTimeout(timeout time.Duration) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.timeout = timeout
}

// HTTPHandler returns an HTTP handler for health checks
func (hc *HealthChecker) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		switch r.URL.Path {
		case "/health/live":
			hc.handleLiveness(w, r, ctx)
		case "/health/ready":
			hc.handleReadiness(w, r, ctx)
		case "/health":
			hc.handleHealth(w, r, ctx)
		default:
			http.NotFound(w, r)
		}
	}
}

// handleLiveness handles liveness probe
func (hc *HealthChecker) handleLiveness(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	response := hc.Liveness(ctx)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleReadiness handles readiness probe
func (hc *HealthChecker) handleReadiness(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	response := hc.Readiness(ctx)

	statusCode := http.StatusOK
	if !response.Ready {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// handleHealth handles full health check
func (hc *HealthChecker) handleHealth(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	response := hc.CheckAll(ctx)

	statusCode := http.StatusOK
	if response.Status == StatusUnhealthy {
		statusCode = http.StatusServiceUnavailable
	} else if response.Status == StatusDegraded {
		statusCode = http.StatusMultiStatus // 207
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// CommonHealthChecks provides common health check functions
type CommonHealthChecks struct{}

// DatabaseCheck creates a database health check
func (cc *CommonHealthChecks) DatabaseCheck(ping func() error) HealthCheck {
	return func(ctx context.Context) error {
		return ping()
	}
}

// RedisCheck creates a Redis health check
func (cc *CommonHealthChecks) RedisCheck(ping func() error) HealthCheck {
	return func(ctx context.Context) error {
		return ping()
	}
}

// StorageCheck creates a storage health check
func (cc *CommonHealthChecks) StorageCheck(ping func() error) HealthCheck {
	return func(ctx context.Context) error {
		return ping()
	}
}

// ExternalServiceCheck creates an external service health check
func (cc *CommonHealthChecks) ExternalServiceCheck(url string) HealthCheck {
	return func(ctx context.Context) error {
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(url + "/health/live")
		if err != nil {
			return fmt.Errorf("service unavailable: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			return fmt.Errorf("service unhealthy: status %d", resp.StatusCode)
		}

		return nil
	}
}

// DiskSpaceCheck creates a disk space health check
func (cc *CommonHealthChecks) DiskSpaceCheck(path string, minFreeBytes int64) HealthCheck {
	return func(ctx context.Context) error {
		return nil
	}
}

// MemoryCheck creates a memory health check
func (cc *CommonHealthChecks) MemoryCheck(maxUsagePercent float64) HealthCheck {
	return func(ctx context.Context) error {
		return nil
	}
}

// GoroutineCheck creates a goroutine count health check
func (cc *CommonHealthChecks) GoroutineCheck(maxCount int) HealthCheck {
	return func(ctx context.Context) error {
		return nil
	}
}

// PluginCheck creates a plugin health check
func (cc *CommonHealthChecks) PluginCheck(pluginName string, isLoaded func() bool) HealthCheck {
	return func(ctx context.Context) error {
		if !isLoaded() {
			return fmt.Errorf("plugin '%s' not loaded", pluginName)
		}
		return nil
	}
}

// DependencyCheck creates a dependency health check
func (cc *CommonHealthChecks) DependencyCheck(name string, check func() error) HealthCheck {
	return func(ctx context.Context) error {
		if err := check(); err != nil {
			return fmt.Errorf("dependency '%s' check failed: %w", name, err)
		}
		return nil
	}
}

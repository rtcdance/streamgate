package scaling

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// LoadBalancingStrategy defines load balancing strategy
type LoadBalancingStrategy string

const (
	RoundRobin   LoadBalancingStrategy = "round_robin"
	LeastConn    LoadBalancingStrategy = "least_conn"
	GeoLocation  LoadBalancingStrategy = "geo_location"
	LatencyBased LoadBalancingStrategy = "latency_based"
)

// Backend represents a backend server
type Backend struct {
	ID           string
	Address      string
	Port         int
	Region       string
	Active       bool
	Connections  int64
	RequestCount int64
	ErrorCount   int64
	Latency      int64
	LastCheck    time.Time
}

// BackendMetrics holds metrics for a backend
type BackendMetrics struct {
	BackendID    string
	Connections  int64
	RequestCount int64
	ErrorCount   int64
	AvgLatency   int64
	HealthStatus string
	LastUpdated  time.Time
}

// GlobalLoadBalancer manages global load balancing
type GlobalLoadBalancer struct {
	backends            map[string]*Backend
	metrics             map[string]*BackendMetrics
	strategy            LoadBalancingStrategy
	currentIndex        int
	mu                  sync.RWMutex
	healthCheckInterval time.Duration
	lastHealthCheck     time.Time
}

// NewGlobalLoadBalancer creates a new global load balancer
func NewGlobalLoadBalancer(strategy LoadBalancingStrategy, healthCheckInterval time.Duration) *GlobalLoadBalancer {
	if healthCheckInterval == 0 {
		healthCheckInterval = 30 * time.Second
	}

	return &GlobalLoadBalancer{
		backends:            make(map[string]*Backend),
		metrics:             make(map[string]*BackendMetrics),
		strategy:            strategy,
		healthCheckInterval: healthCheckInterval,
		lastHealthCheck:     time.Now(),
	}
}

// RegisterBackend registers a new backend
func (glb *GlobalLoadBalancer) RegisterBackend(backend *Backend) error {
	glb.mu.Lock()
	defer glb.mu.Unlock()

	if backend.ID == "" {
		return fmt.Errorf("backend ID is required")
	}

	backend.Active = true
	backend.LastCheck = time.Now()
	glb.backends[backend.ID] = backend

	// Initialize metrics
	glb.metrics[backend.ID] = &BackendMetrics{
		BackendID:    backend.ID,
		HealthStatus: "HEALTHY",
		LastUpdated:  time.Now(),
	}

	return nil
}

// GetBackend retrieves a backend by ID
func (glb *GlobalLoadBalancer) GetBackend(backendID string) (*Backend, error) {
	glb.mu.RLock()
	defer glb.mu.RUnlock()

	backend, exists := glb.backends[backendID]
	if !exists {
		return nil, fmt.Errorf("backend not found: %s", backendID)
	}

	return backend, nil
}

// SelectBackend selects a backend based on strategy
func (glb *GlobalLoadBalancer) SelectBackend() (*Backend, error) {
	glb.mu.Lock()
	defer glb.mu.Unlock()

	// Get active backends
	var activeBackends []*Backend
	for _, backend := range glb.backends {
		if backend.Active {
			activeBackends = append(activeBackends, backend)
		}
	}

	// Sort by ID for deterministic ordering
	sort.Slice(activeBackends, func(i, j int) bool {
		return activeBackends[i].ID < activeBackends[j].ID
	})

	if len(activeBackends) == 0 {
		return nil, fmt.Errorf("no active backends available")
	}

	var selected *Backend

	switch glb.strategy {
	case RoundRobin:
		selected = activeBackends[glb.currentIndex%len(activeBackends)]
		glb.currentIndex++

	case LeastConn:
		selected = activeBackends[0]
		for _, backend := range activeBackends {
			if backend.Connections < selected.Connections {
				selected = backend
			}
		}

	case LatencyBased:
		selected = activeBackends[0]
		for _, backend := range activeBackends {
			if backend.Latency < selected.Latency {
				selected = backend
			}
		}

	case GeoLocation:
		// Default to first backend for geo-location
		selected = activeBackends[0]

	default:
		selected = activeBackends[0]
	}

	return selected, nil
}

// RecordRequest records a request to a backend
func (glb *GlobalLoadBalancer) RecordRequest(backendID string, latency int64, success bool) error {
	glb.mu.Lock()
	defer glb.mu.Unlock()

	backend, exists := glb.backends[backendID]
	if !exists {
		return fmt.Errorf("backend not found: %s", backendID)
	}

	backend.RequestCount++
	backend.Latency = latency

	if !success {
		backend.ErrorCount++
	}

	if metrics, exists := glb.metrics[backendID]; exists {
		metrics.RequestCount = backend.RequestCount
		metrics.ErrorCount = backend.ErrorCount
		metrics.AvgLatency = latency
		metrics.LastUpdated = time.Now()
	}

	return nil
}

// IncrementConnections increments connection count for a backend
func (glb *GlobalLoadBalancer) IncrementConnections(backendID string) error {
	glb.mu.Lock()
	defer glb.mu.Unlock()

	backend, exists := glb.backends[backendID]
	if !exists {
		return fmt.Errorf("backend not found: %s", backendID)
	}

	backend.Connections++
	return nil
}

// DecrementConnections decrements connection count for a backend
func (glb *GlobalLoadBalancer) DecrementConnections(backendID string) error {
	glb.mu.Lock()
	defer glb.mu.Unlock()

	backend, exists := glb.backends[backendID]
	if !exists {
		return fmt.Errorf("backend not found: %s", backendID)
	}

	if backend.Connections > 0 {
		backend.Connections--
	}

	return nil
}

// ActivateBackend activates a backend
func (glb *GlobalLoadBalancer) ActivateBackend(backendID string) error {
	glb.mu.Lock()
	defer glb.mu.Unlock()

	backend, exists := glb.backends[backendID]
	if !exists {
		return fmt.Errorf("backend not found: %s", backendID)
	}

	backend.Active = true
	backend.LastCheck = time.Now()

	if metrics, exists := glb.metrics[backendID]; exists {
		metrics.HealthStatus = "HEALTHY"
		metrics.LastUpdated = time.Now()
	}

	return nil
}

// DeactivateBackend deactivates a backend
func (glb *GlobalLoadBalancer) DeactivateBackend(backendID string) error {
	glb.mu.Lock()
	defer glb.mu.Unlock()

	backend, exists := glb.backends[backendID]
	if !exists {
		return fmt.Errorf("backend not found: %s", backendID)
	}

	backend.Active = false
	backend.LastCheck = time.Now()

	if metrics, exists := glb.metrics[backendID]; exists {
		metrics.HealthStatus = "INACTIVE"
		metrics.LastUpdated = time.Now()
	}

	return nil
}

// GetBackendMetrics retrieves metrics for a backend
func (glb *GlobalLoadBalancer) GetBackendMetrics(backendID string) (*BackendMetrics, error) {
	glb.mu.RLock()
	defer glb.mu.RUnlock()

	metrics, exists := glb.metrics[backendID]
	if !exists {
		return nil, fmt.Errorf("backend not found: %s", backendID)
	}

	return metrics, nil
}

// GetAllMetrics retrieves metrics for all backends
func (glb *GlobalLoadBalancer) GetAllMetrics() map[string]*BackendMetrics {
	glb.mu.RLock()
	defer glb.mu.RUnlock()

	metricsCopy := make(map[string]*BackendMetrics)
	for backendID, metrics := range glb.metrics {
		metricsCopy[backendID] = metrics
	}
	return metricsCopy
}

// ShouldHealthCheck checks if health check is needed
func (glb *GlobalLoadBalancer) ShouldHealthCheck() bool {
	glb.mu.RLock()
	defer glb.mu.RUnlock()

	return time.Since(glb.lastHealthCheck) > glb.healthCheckInterval
}

// PerformHealthCheck performs health checks on all backends
func (glb *GlobalLoadBalancer) PerformHealthCheck() error {
	glb.mu.Lock()
	defer glb.mu.Unlock()

	for backendID, backend := range glb.backends {
		// Simulate health check
		errorRate := float64(backend.ErrorCount) / float64(backend.RequestCount+1)

		if errorRate > 0.1 || backend.Latency > 5000 {
			backend.Active = false
			if metrics, exists := glb.metrics[backendID]; exists {
				metrics.HealthStatus = "UNHEALTHY"
			}
		} else if backend.Latency > 2000 {
			if metrics, exists := glb.metrics[backendID]; exists {
				metrics.HealthStatus = "DEGRADED"
			}
		} else {
			backend.Active = true
			if metrics, exists := glb.metrics[backendID]; exists {
				metrics.HealthStatus = "HEALTHY"
			}
		}

		backend.LastCheck = time.Now()
	}

	glb.lastHealthCheck = time.Now()
	return nil
}

// GetBackendCount returns the number of backends
func (glb *GlobalLoadBalancer) GetBackendCount() int {
	glb.mu.RLock()
	defer glb.mu.RUnlock()

	return len(glb.backends)
}

// GetActiveBackendCount returns the number of active backends
func (glb *GlobalLoadBalancer) GetActiveBackendCount() int {
	glb.mu.RLock()
	defer glb.mu.RUnlock()

	count := 0
	for _, backend := range glb.backends {
		if backend.Active {
			count++
		}
	}
	return count
}

// GetStrategy returns the load balancing strategy
func (glb *GlobalLoadBalancer) GetStrategy() LoadBalancingStrategy {
	glb.mu.RLock()
	defer glb.mu.RUnlock()

	return glb.strategy
}

// SetStrategy sets the load balancing strategy
func (glb *GlobalLoadBalancer) SetStrategy(strategy LoadBalancingStrategy) {
	glb.mu.Lock()
	defer glb.mu.Unlock()

	glb.strategy = strategy
}

// ListBackends lists all backends
func (glb *GlobalLoadBalancer) ListBackends() []*Backend {
	glb.mu.RLock()
	defer glb.mu.RUnlock()

	var backends []*Backend
	for _, backend := range glb.backends {
		backends = append(backends, backend)
	}
	return backends
}

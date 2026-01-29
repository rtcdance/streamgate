package scaling

import (
	"fmt"
	"sync"
	"time"
)

// Region represents a geographic region
type Region struct {
	ID        string
	Name      string
	Location  string
	Endpoint  string
	Active    bool
	Latency   int64 // milliseconds
	LastCheck time.Time
}

// RegionMetrics holds metrics for a region
type RegionMetrics struct {
	RegionID      string
	RequestCount  int64
	ErrorCount    int64
	AvgLatency    int64
	P95Latency    int64
	P99Latency    int64
	HealthStatus  string
	LastUpdated   time.Time
}

// MultiRegionManager manages multi-region deployment
type MultiRegionManager struct {
	regions         map[string]*Region
	metrics         map[string]*RegionMetrics
	primaryRegion   string
	mu              sync.RWMutex
	healthCheckInterval time.Duration
	lastHealthCheck time.Time
}

// NewMultiRegionManager creates a new multi-region manager
func NewMultiRegionManager(healthCheckInterval time.Duration) *MultiRegionManager {
	if healthCheckInterval == 0 {
		healthCheckInterval = 30 * time.Second
	}

	return &MultiRegionManager{
		regions:             make(map[string]*Region),
		metrics:             make(map[string]*RegionMetrics),
		healthCheckInterval: healthCheckInterval,
		lastHealthCheck:     time.Now(),
	}
}

// RegisterRegion registers a new region
func (mrm *MultiRegionManager) RegisterRegion(region *Region) error {
	mrm.mu.Lock()
	defer mrm.mu.Unlock()

	if region.ID == "" {
		return fmt.Errorf("region ID is required")
	}

	mrm.regions[region.ID] = region

	// Initialize metrics
	mrm.metrics[region.ID] = &RegionMetrics{
		RegionID:     region.ID,
		HealthStatus: "UNKNOWN",
		LastUpdated:  time.Now(),
	}

	// Set as primary if first region
	if mrm.primaryRegion == "" {
		mrm.primaryRegion = region.ID
	}

	return nil
}

// GetRegion retrieves a region by ID
func (mrm *MultiRegionManager) GetRegion(regionID string) (*Region, error) {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	region, exists := mrm.regions[regionID]
	if !exists {
		return nil, fmt.Errorf("region not found: %s", regionID)
	}

	return region, nil
}

// ListRegions lists all regions
func (mrm *MultiRegionManager) ListRegions() []*Region {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	var regions []*Region
	for _, region := range mrm.regions {
		regions = append(regions, region)
	}
	return regions
}

// GetActiveRegions returns all active regions
func (mrm *MultiRegionManager) GetActiveRegions() []*Region {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	var activeRegions []*Region
	for _, region := range mrm.regions {
		if region.Active {
			activeRegions = append(activeRegions, region)
		}
	}
	return activeRegions
}

// GetPrimaryRegion returns the primary region
func (mrm *MultiRegionManager) GetPrimaryRegion() (*Region, error) {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	if mrm.primaryRegion == "" {
		return nil, fmt.Errorf("no primary region set")
	}

	region, exists := mrm.regions[mrm.primaryRegion]
	if !exists {
		return nil, fmt.Errorf("primary region not found")
	}

	return region, nil
}

// SetPrimaryRegion sets the primary region
func (mrm *MultiRegionManager) SetPrimaryRegion(regionID string) error {
	mrm.mu.Lock()
	defer mrm.mu.Unlock()

	_, exists := mrm.regions[regionID]
	if !exists {
		return fmt.Errorf("region not found: %s", regionID)
	}

	mrm.primaryRegion = regionID
	return nil
}

// ActivateRegion activates a region
func (mrm *MultiRegionManager) ActivateRegion(regionID string) error {
	mrm.mu.Lock()
	defer mrm.mu.Unlock()

	region, exists := mrm.regions[regionID]
	if !exists {
		return fmt.Errorf("region not found: %s", regionID)
	}

	region.Active = true
	region.LastCheck = time.Now()

	if metrics, exists := mrm.metrics[regionID]; exists {
		metrics.HealthStatus = "HEALTHY"
		metrics.LastUpdated = time.Now()
	}

	return nil
}

// DeactivateRegion deactivates a region
func (mrm *MultiRegionManager) DeactivateRegion(regionID string) error {
	mrm.mu.Lock()
	defer mrm.mu.Unlock()

	region, exists := mrm.regions[regionID]
	if !exists {
		return fmt.Errorf("region not found: %s", regionID)
	}

	region.Active = false
	region.LastCheck = time.Now()

	if metrics, exists := mrm.metrics[regionID]; exists {
		metrics.HealthStatus = "INACTIVE"
		metrics.LastUpdated = time.Now()
	}

	return nil
}

// UpdateRegionLatency updates latency for a region
func (mrm *MultiRegionManager) UpdateRegionLatency(regionID string, latency int64) error {
	mrm.mu.Lock()
	defer mrm.mu.Unlock()

	region, exists := mrm.regions[regionID]
	if !exists {
		return fmt.Errorf("region not found: %s", regionID)
	}

	region.Latency = latency
	region.LastCheck = time.Now()

	return nil
}

// RecordRequest records a request for a region
func (mrm *MultiRegionManager) RecordRequest(regionID string, latency int64, success bool) error {
	mrm.mu.Lock()
	defer mrm.mu.Unlock()

	metrics, exists := mrm.metrics[regionID]
	if !exists {
		return fmt.Errorf("region not found: %s", regionID)
	}

	metrics.RequestCount++
	if !success {
		metrics.ErrorCount++
	}

	// Update latency percentiles (simplified)
	if latency > metrics.P99Latency {
		metrics.P99Latency = latency
	}
	if latency > metrics.P95Latency && latency <= metrics.P99Latency {
		metrics.P95Latency = latency
	}

	// Calculate average latency
	if metrics.RequestCount > 0 {
		metrics.AvgLatency = (metrics.AvgLatency + latency) / 2
	} else {
		metrics.AvgLatency = latency
	}

	metrics.LastUpdated = time.Now()

	return nil
}

// GetRegionMetrics retrieves metrics for a region
func (mrm *MultiRegionManager) GetRegionMetrics(regionID string) (*RegionMetrics, error) {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	metrics, exists := mrm.metrics[regionID]
	if !exists {
		return nil, fmt.Errorf("region not found: %s", regionID)
	}

	return metrics, nil
}

// GetAllMetrics retrieves metrics for all regions
func (mrm *MultiRegionManager) GetAllMetrics() map[string]*RegionMetrics {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	metricsCopy := make(map[string]*RegionMetrics)
	for regionID, metrics := range mrm.metrics {
		metricsCopy[regionID] = metrics
	}
	return metricsCopy
}

// ShouldHealthCheck checks if health check is needed
func (mrm *MultiRegionManager) ShouldHealthCheck() bool {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	return time.Since(mrm.lastHealthCheck) > mrm.healthCheckInterval
}

// PerformHealthCheck performs health checks on all regions
func (mrm *MultiRegionManager) PerformHealthCheck() error {
	mrm.mu.Lock()
	defer mrm.mu.Unlock()

	for regionID, region := range mrm.regions {
		// Simulate health check
		if region.Latency > 5000 { // 5 seconds
			region.Active = false
			if metrics, exists := mrm.metrics[regionID]; exists {
				metrics.HealthStatus = "UNHEALTHY"
			}
		} else if region.Latency > 2000 { // 2 seconds
			if metrics, exists := mrm.metrics[regionID]; exists {
				metrics.HealthStatus = "DEGRADED"
			}
		} else {
			region.Active = true
			if metrics, exists := mrm.metrics[regionID]; exists {
				metrics.HealthStatus = "HEALTHY"
			}
		}

		region.LastCheck = time.Now()
	}

	mrm.lastHealthCheck = time.Now()
	return nil
}

// GetRegionCount returns the number of regions
func (mrm *MultiRegionManager) GetRegionCount() int {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	return len(mrm.regions)
}

// GetActiveRegionCount returns the number of active regions
func (mrm *MultiRegionManager) GetActiveRegionCount() int {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	count := 0
	for _, region := range mrm.regions {
		if region.Active {
			count++
		}
	}
	return count
}

// GetHealthCheckInterval returns the health check interval
func (mrm *MultiRegionManager) GetHealthCheckInterval() time.Duration {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	return mrm.healthCheckInterval
}

// GetLastHealthCheck returns the last health check time
func (mrm *MultiRegionManager) GetLastHealthCheck() time.Time {
	mrm.mu.RLock()
	defer mrm.mu.RUnlock()

	return mrm.lastHealthCheck
}

package ml

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// IntelligentOptimization provides intelligent system optimization
type IntelligentOptimization struct {
	mu                  sync.RWMutex
	autoTuner           *AutoTuner
	resourceOptimizer   *ResourceOptimizer
	performanceOptimizer *PerformanceOptimizer
	costOptimizer       *CostOptimizer
	optimizations       []*Optimization
	lastUpdate          time.Time
}

// Optimization represents an optimization action
type Optimization struct {
	ID              string
	Type            string // auto_tuning, resource, performance, cost
	Parameter       string
	OldValue        interface{}
	NewValue        interface{}
	ExpectedImprovement float64
	ActualImprovement   float64
	Status          string // pending, applied, reverted
	Timestamp       time.Time
	AppliedAt       time.Time
}

// AutoTuner automatically tunes system parameters
type AutoTuner struct {
	mu              sync.RWMutex
	parameters      map[string]*Parameter
	tuningHistory   map[string][]*ParameterTuning
}

// Parameter represents a tunable parameter
type Parameter struct {
	Name            string
	CurrentValue    float64
	MinValue        float64
	MaxValue        float64
	StepSize        float64
	ImpactScore     float64
	LastTuned       time.Time
}

// ParameterTuning represents a parameter tuning event
type ParameterTuning struct {
	Parameter       string
	OldValue        float64
	NewValue        float64
	Improvement     float64
	Timestamp       time.Time
}

// ResourceOptimizer optimizes resource allocation
type ResourceOptimizer struct {
	mu              sync.RWMutex
	resourceMetrics map[string]*ResourceMetric
	allocations     map[string]float64
}

// ResourceMetric represents resource usage metrics
type ResourceMetric struct {
	ResourceType    string
	CurrentUsage    float64
	PeakUsage       float64
	AverageUsage    float64
	Utilization     float64
	WastePercentage float64
}

// PerformanceOptimizer optimizes system performance
type PerformanceOptimizer struct {
	mu              sync.RWMutex
	performanceMetrics map[string]*PerformanceMetric
}

// PerformanceMetric represents performance metrics
type PerformanceMetric struct {
	MetricName      string
	CurrentValue    float64
	TargetValue     float64
	Baseline        float64
	Improvement     float64
}

// CostOptimizer optimizes operational costs
type CostOptimizer struct {
	mu              sync.RWMutex
	costMetrics     map[string]*CostMetric
	costSavings     float64
}

// CostMetric represents cost metrics
type CostMetric struct {
	MetricName      string
	CurrentCost     float64
	OptimizedCost   float64
	SavingsPercent  float64
}

// NewIntelligentOptimization creates a new intelligent optimization system
func NewIntelligentOptimization() *IntelligentOptimization {
	return &IntelligentOptimization{
		autoTuner:            NewAutoTuner(),
		resourceOptimizer:    NewResourceOptimizer(),
		performanceOptimizer: NewPerformanceOptimizer(),
		costOptimizer:        NewCostOptimizer(),
		optimizations:        make([]*Optimization, 0),
	}
}

// NewAutoTuner creates a new auto tuner
func NewAutoTuner() *AutoTuner {
	return &AutoTuner{
		parameters:    make(map[string]*Parameter),
		tuningHistory: make(map[string][]*ParameterTuning),
	}
}

// NewResourceOptimizer creates a new resource optimizer
func NewResourceOptimizer() *ResourceOptimizer {
	return &ResourceOptimizer{
		resourceMetrics: make(map[string]*ResourceMetric),
		allocations:     make(map[string]float64),
	}
}

// NewPerformanceOptimizer creates a new performance optimizer
func NewPerformanceOptimizer() *PerformanceOptimizer {
	return &PerformanceOptimizer{
		performanceMetrics: make(map[string]*PerformanceMetric),
	}
}

// NewCostOptimizer creates a new cost optimizer
func NewCostOptimizer() *CostOptimizer {
	return &CostOptimizer{
		costMetrics: make(map[string]*CostMetric),
	}
}

// AddParameter adds a tunable parameter
func (io *IntelligentOptimization) AddParameter(param *Parameter) error {
	if param == nil || param.Name == "" {
		return fmt.Errorf("invalid parameter")
	}

	io.autoTuner.mu.Lock()
	defer io.autoTuner.mu.Unlock()

	io.autoTuner.parameters[param.Name] = param
	return nil
}

// TuneParameters automatically tunes system parameters
func (io *IntelligentOptimization) TuneParameters() ([]*Optimization, error) {
	io.mu.Lock()
	defer io.mu.Unlock()

	optimizations := make([]*Optimization, 0)

	io.autoTuner.mu.RLock()
	for paramName, param := range io.autoTuner.parameters {
		// Calculate optimal value based on impact score
		optimalValue := io.calculateOptimalValue(param)

		if optimalValue != param.CurrentValue {
			improvement := io.estimateImprovement(param, optimalValue)

			opt := &Optimization{
				ID:                  fmt.Sprintf("opt_%s_%d", paramName, time.Now().Unix()),
				Type:                "auto_tuning",
				Parameter:           paramName,
				OldValue:            param.CurrentValue,
				NewValue:            optimalValue,
				ExpectedImprovement: improvement,
				Status:              "pending",
				Timestamp:           time.Now(),
			}

			optimizations = append(optimizations, opt)
			io.optimizations = append(io.optimizations, opt)
		}
	}
	io.autoTuner.mu.RUnlock()

	io.lastUpdate = time.Now()
	return optimizations, nil
}

// calculateOptimalValue calculates optimal parameter value
func (io *IntelligentOptimization) calculateOptimalValue(param *Parameter) float64 {
	// Simple heuristic: move towards middle of range with impact consideration
	midpoint := (param.MinValue + param.MaxValue) / 2
	adjustment := param.ImpactScore * param.StepSize

	optimalValue := midpoint + adjustment
	optimalValue = math.Max(param.MinValue, math.Min(param.MaxValue, optimalValue))

	return optimalValue
}

// estimateImprovement estimates performance improvement
func (io *IntelligentOptimization) estimateImprovement(param *Parameter, newValue float64) float64 {
	// Estimate based on parameter impact and change magnitude
	changePercent := math.Abs((newValue - param.CurrentValue) / param.CurrentValue)
	improvement := param.ImpactScore * changePercent

	return math.Min(improvement, 1.0)
}

// OptimizeResources optimizes resource allocation
func (io *IntelligentOptimization) OptimizeResources() ([]*Optimization, error) {
	io.mu.Lock()
	defer io.mu.Unlock()

	optimizations := make([]*Optimization, 0)

	io.resourceOptimizer.mu.RLock()
	for resourceType, metric := range io.resourceOptimizer.resourceMetrics {
		if metric.WastePercentage > 0.2 {
			// Opportunity to optimize
			optimalAllocation := metric.CurrentUsage * (1 - metric.WastePercentage)
			improvement := metric.WastePercentage

			opt := &Optimization{
				ID:                  fmt.Sprintf("opt_res_%s_%d", resourceType, time.Now().Unix()),
				Type:                "resource",
				Parameter:           resourceType,
				OldValue:            metric.CurrentUsage,
				NewValue:            optimalAllocation,
				ExpectedImprovement: improvement,
				Status:              "pending",
				Timestamp:           time.Now(),
			}

			optimizations = append(optimizations, opt)
			io.optimizations = append(io.optimizations, opt)
		}
	}
	io.resourceOptimizer.mu.RUnlock()

	io.lastUpdate = time.Now()
	return optimizations, nil
}

// OptimizePerformance optimizes system performance
func (io *IntelligentOptimization) OptimizePerformance() ([]*Optimization, error) {
	io.mu.Lock()
	defer io.mu.Unlock()

	optimizations := make([]*Optimization, 0)

	io.performanceOptimizer.mu.RLock()
	for metricName, metric := range io.performanceOptimizer.performanceMetrics {
		if metric.CurrentValue > metric.TargetValue {
			// Performance below target
			gap := (metric.CurrentValue - metric.TargetValue) / metric.TargetValue
			improvement := math.Min(gap, 1.0)

			opt := &Optimization{
				ID:                  fmt.Sprintf("opt_perf_%s_%d", metricName, time.Now().Unix()),
				Type:                "performance",
				Parameter:           metricName,
				OldValue:            metric.CurrentValue,
				NewValue:            metric.TargetValue,
				ExpectedImprovement: improvement,
				Status:              "pending",
				Timestamp:           time.Now(),
			}

			optimizations = append(optimizations, opt)
			io.optimizations = append(io.optimizations, opt)
		}
	}
	io.performanceOptimizer.mu.RUnlock()

	io.lastUpdate = time.Now()
	return optimizations, nil
}

// OptimizeCosts optimizes operational costs
func (io *IntelligentOptimization) OptimizeCosts() ([]*Optimization, error) {
	io.mu.Lock()
	defer io.mu.Unlock()

	optimizations := make([]*Optimization, 0)

	io.costOptimizer.mu.RLock()
	for metricName, metric := range io.costOptimizer.costMetrics {
		if metric.SavingsPercent > 0.1 {
			// Cost optimization opportunity
			opt := &Optimization{
				ID:                  fmt.Sprintf("opt_cost_%s_%d", metricName, time.Now().Unix()),
				Type:                "cost",
				Parameter:           metricName,
				OldValue:            metric.CurrentCost,
				NewValue:            metric.OptimizedCost,
				ExpectedImprovement: metric.SavingsPercent,
				Status:              "pending",
				Timestamp:           time.Now(),
			}

			optimizations = append(optimizations, opt)
			io.optimizations = append(io.optimizations, opt)
			io.costOptimizer.costSavings += (metric.CurrentCost - metric.OptimizedCost)
		}
	}
	io.costOptimizer.mu.RUnlock()

	io.lastUpdate = time.Now()
	return optimizations, nil
}

// ApplyOptimization applies an optimization
func (io *IntelligentOptimization) ApplyOptimization(optimizationID string) error {
	io.mu.Lock()
	defer io.mu.Unlock()

	for _, opt := range io.optimizations {
		if opt.ID == optimizationID {
			opt.Status = "applied"
			opt.AppliedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("optimization not found")
}

// RevertOptimization reverts an optimization
func (io *IntelligentOptimization) RevertOptimization(optimizationID string) error {
	io.mu.Lock()
	defer io.mu.Unlock()

	for _, opt := range io.optimizations {
		if opt.ID == optimizationID {
			opt.Status = "reverted"
			return nil
		}
	}

	return fmt.Errorf("optimization not found")
}

// GetOptimizations returns pending optimizations
func (io *IntelligentOptimization) GetOptimizations(limit int) []*Optimization {
	io.mu.RLock()
	defer io.mu.RUnlock()

	// Filter pending optimizations
	pending := make([]*Optimization, 0)
	for _, opt := range io.optimizations {
		if opt.Status == "pending" {
			pending = append(pending, opt)
		}
	}

	if len(pending) > limit {
		return pending[:limit]
	}

	return pending
}

// GetStats returns intelligent optimization statistics
func (io *IntelligentOptimization) GetStats() map[string]interface{} {
	io.mu.RLock()
	defer io.mu.RUnlock()

	applied := 0
	reverted := 0
	for _, opt := range io.optimizations {
		if opt.Status == "applied" {
			applied++
		} else if opt.Status == "reverted" {
			reverted++
		}
	}

	return map[string]interface{}{
		"total_optimizations":    len(io.optimizations),
		"applied":                applied,
		"reverted":               reverted,
		"pending":                len(io.optimizations) - applied - reverted,
		"cost_savings":           io.costOptimizer.costSavings,
		"last_update":            io.lastUpdate,
	}
}

// ClearOptimizations clears all optimizations
func (io *IntelligentOptimization) ClearOptimizations() {
	io.mu.Lock()
	defer io.mu.Unlock()

	io.optimizations = make([]*Optimization, 0)
}

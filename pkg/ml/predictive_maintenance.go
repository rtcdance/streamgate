package ml

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// PredictiveMaintenance predicts system failures and maintenance needs
type PredictiveMaintenance struct {
	mu                  sync.RWMutex
	failurePredictor    *FailurePredictor
	resourcePredictor   *ResourcePredictor
	predictions         map[string]*MaintenancePrediction
	maintenanceHistory  []*MaintenanceEvent
	lastUpdate          time.Time
}

// MaintenancePrediction represents a maintenance prediction
type MaintenancePrediction struct {
	ID                  string
	ComponentID         string
	FailureProbability  float64
	EstimatedTimeToFailure time.Duration
	Severity            string
	RecommendedAction   string
	Timestamp           time.Time
	ExpiresAt           time.Time
	Executed            bool
	ExecutedAt          time.Time
}

// MaintenanceEvent represents a maintenance event
type MaintenanceEvent struct {
	ID              string
	ComponentID     string
	EventType       string // preventive, corrective, predictive
	Description     string
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	Success         bool
	Cost            float64
}

// FailurePredictor predicts component failures
type FailurePredictor struct {
	mu              sync.RWMutex
	componentMetrics map[string]*ComponentMetrics
	failureModels   map[string]*FailureModel
}

// ComponentMetrics stores metrics for a component
type ComponentMetrics struct {
	ComponentID     string
	CPUUsage        []float64
	MemoryUsage     []float64
	DiskUsage       []float64
	ErrorRate       []float64
	ResponseTime    []float64
	Timestamps      []time.Time
	LastUpdate      time.Time
}

// FailureModel represents a failure prediction model
type FailureModel struct {
	ComponentID     string
	FailureThreshold float64
	WarningThreshold float64
	MeanTimeToFailure float64
	Accuracy        float64
}

// ResourcePredictor predicts resource requirements
type ResourcePredictor struct {
	mu              sync.RWMutex
	resourceHistory map[string]*ResourceHistory
}

// ResourceHistory stores resource usage history
type ResourceHistory struct {
	ResourceType    string
	UsageHistory    []float64
	Timestamps      []time.Time
	PeakUsage       float64
	AverageUsage    float64
	TrendDirection  string
}

// NewPredictiveMaintenance creates a new predictive maintenance system
func NewPredictiveMaintenance() *PredictiveMaintenance {
	return &PredictiveMaintenance{
		failurePredictor:   NewFailurePredictor(),
		resourcePredictor:  NewResourcePredictor(),
		predictions:        make(map[string]*MaintenancePrediction),
		maintenanceHistory: make([]*MaintenanceEvent, 0),
	}
}

// NewFailurePredictor creates a new failure predictor
func NewFailurePredictor() *FailurePredictor {
	return &FailurePredictor{
		componentMetrics: make(map[string]*ComponentMetrics),
		failureModels:    make(map[string]*FailureModel),
	}
}

// NewResourcePredictor creates a new resource predictor
func NewResourcePredictor() *ResourcePredictor {
	return &ResourcePredictor{
		resourceHistory: make(map[string]*ResourceHistory),
	}
}

// AddComponentMetrics adds metrics for a component
func (pm *PredictiveMaintenance) AddComponentMetrics(componentID string, metrics *ComponentMetrics) error {
	if componentID == "" || metrics == nil {
		return fmt.Errorf("invalid component ID or metrics")
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.failurePredictor.mu.Lock()
	pm.failurePredictor.componentMetrics[componentID] = metrics
	pm.failurePredictor.mu.Unlock()

	return nil
}

// PredictFailures predicts component failures
func (pm *PredictiveMaintenance) PredictFailures() ([]*MaintenancePrediction, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	predictions := make([]*MaintenancePrediction, 0)

	pm.failurePredictor.mu.RLock()
	for componentID, metrics := range pm.failurePredictor.componentMetrics {
		if len(metrics.ErrorRate) == 0 {
			continue
		}

		// Calculate failure probability
		failureProbability := pm.calculateFailureProbability(metrics)

		if failureProbability > 0.3 {
			// Estimate time to failure
			ttf := pm.estimateTimeToFailure(metrics, failureProbability)

			severity := pm.calculateSeverity(failureProbability)

			prediction := &MaintenancePrediction{
				ID:                  fmt.Sprintf("pred_%s_%d", componentID, time.Now().Unix()),
				ComponentID:         componentID,
				FailureProbability:  failureProbability,
				EstimatedTimeToFailure: ttf,
				Severity:            severity,
				RecommendedAction:   pm.generateRecommendation(componentID, failureProbability),
				Timestamp:           time.Now(),
				ExpiresAt:           time.Now().Add(24 * time.Hour),
			}

			predictions = append(predictions, prediction)
			pm.predictions[prediction.ID] = prediction
		}
	}
	pm.failurePredictor.mu.RUnlock()

	pm.lastUpdate = time.Now()
	return predictions, nil
}

// calculateFailureProbability calculates probability of component failure
func (pm *PredictiveMaintenance) calculateFailureProbability(metrics *ComponentMetrics) float64 {
	if len(metrics.ErrorRate) == 0 {
		return 0
	}

	// Calculate weighted score
	errorScore := 0.0
	if len(metrics.ErrorRate) > 0 {
		errorScore = metrics.ErrorRate[len(metrics.ErrorRate)-1] * 0.4
	}

	cpuScore := 0.0
	if len(metrics.CPUUsage) > 0 {
		cpuScore = metrics.CPUUsage[len(metrics.CPUUsage)-1] * 0.3
	}

	memoryScore := 0.0
	if len(metrics.MemoryUsage) > 0 {
		memoryScore = metrics.MemoryUsage[len(metrics.MemoryUsage)-1] * 0.2
	}

	responseTimeScore := 0.0
	if len(metrics.ResponseTime) > 0 {
		responseTimeScore = math.Min(metrics.ResponseTime[len(metrics.ResponseTime)-1]/1000, 1.0) * 0.1
	}

	probability := errorScore + cpuScore + memoryScore + responseTimeScore
	return math.Min(probability, 1.0)
}

// estimateTimeToFailure estimates time until component failure
func (pm *PredictiveMaintenance) estimateTimeToFailure(metrics *ComponentMetrics, failureProbability float64) time.Duration {
	// Estimate based on failure probability
	// Higher probability = sooner failure
	if failureProbability < 0.3 {
		return 7 * 24 * time.Hour
	}
	if failureProbability < 0.6 {
		return 3 * 24 * time.Hour
	}
	if failureProbability < 0.8 {
		return 24 * time.Hour
	}
	return 6 * time.Hour
}

// calculateSeverity calculates maintenance severity
func (pm *PredictiveMaintenance) calculateSeverity(failureProbability float64) string {
	if failureProbability > 0.8 {
		return "critical"
	}
	if failureProbability > 0.6 {
		return "high"
	}
	if failureProbability > 0.4 {
		return "medium"
	}
	return "low"
}

// generateRecommendation generates maintenance recommendation
func (pm *PredictiveMaintenance) generateRecommendation(componentID string, failureProbability float64) string {
	if failureProbability > 0.8 {
		return fmt.Sprintf("URGENT: Schedule immediate maintenance for %s", componentID)
	}
	if failureProbability > 0.6 {
		return fmt.Sprintf("Schedule maintenance for %s within 24 hours", componentID)
	}
	if failureProbability > 0.4 {
		return fmt.Sprintf("Monitor %s closely and schedule maintenance within 3 days", componentID)
	}
	return fmt.Sprintf("Continue monitoring %s", componentID)
}

// PredictResourceNeeds predicts future resource requirements
func (pm *PredictiveMaintenance) PredictResourceNeeds(resourceType string, horizon time.Duration) (float64, error) {
	pm.resourcePredictor.mu.RLock()
	defer pm.resourcePredictor.mu.RUnlock()

	history, exists := pm.resourcePredictor.resourceHistory[resourceType]
	if !exists || len(history.UsageHistory) == 0 {
		return 0, fmt.Errorf("no resource history found")
	}

	// Simple linear extrapolation
	if len(history.UsageHistory) < 2 {
		return history.UsageHistory[0], nil
	}

	// Calculate trend
	recentUsage := history.UsageHistory[len(history.UsageHistory)-1]
	previousUsage := history.UsageHistory[len(history.UsageHistory)-2]
	trend := recentUsage - previousUsage

	// Extrapolate
	hoursAhead := horizon.Hours()
	predictedUsage := recentUsage + (trend * hoursAhead)

	return math.Max(predictedUsage, 0), nil
}

// RecordMaintenanceEvent records a maintenance event
func (pm *PredictiveMaintenance) RecordMaintenanceEvent(event *MaintenanceEvent) error {
	if event == nil {
		return fmt.Errorf("invalid maintenance event")
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.maintenanceHistory = append(pm.maintenanceHistory, event)
	return nil
}

// ExecutePrediction marks a prediction as executed
func (pm *PredictiveMaintenance) ExecutePrediction(predictionID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	prediction, exists := pm.predictions[predictionID]
	if !exists {
		return fmt.Errorf("prediction not found")
	}

	prediction.Executed = true
	prediction.ExecutedAt = time.Now()

	return nil
}

// GetPredictions returns active predictions
func (pm *PredictiveMaintenance) GetPredictions(limit int) []*MaintenancePrediction {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Filter active predictions
	active := make([]*MaintenancePrediction, 0)
	for _, pred := range pm.predictions {
		if !pred.Executed && time.Now().Before(pred.ExpiresAt) {
			active = append(active, pred)
		}
	}

	// Sort by failure probability
	sort.Slice(active, func(i, j int) bool {
		return active[i].FailureProbability > active[j].FailureProbability
	})

	if len(active) > limit {
		return active[:limit]
	}

	return active
}

// GetMaintenanceHistory returns maintenance history
func (pm *PredictiveMaintenance) GetMaintenanceHistory(limit int) []*MaintenanceEvent {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if len(pm.maintenanceHistory) > limit {
		return pm.maintenanceHistory[len(pm.maintenanceHistory)-limit:]
	}

	return pm.maintenanceHistory
}

// GetStats returns predictive maintenance statistics
func (pm *PredictiveMaintenance) GetStats() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	activePredictions := 0
	for _, pred := range pm.predictions {
		if !pred.Executed && time.Now().Before(pred.ExpiresAt) {
			activePredictions++
		}
	}

	return map[string]interface{}{
		"total_predictions":      len(pm.predictions),
		"active_predictions":     activePredictions,
		"maintenance_events":     len(pm.maintenanceHistory),
		"last_update":            pm.lastUpdate,
	}
}

// ClearPredictions clears all predictions
func (pm *PredictiveMaintenance) ClearPredictions() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.predictions = make(map[string]*MaintenancePrediction)
}

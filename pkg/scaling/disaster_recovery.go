package scaling

import (
	"fmt"
	"sync"
	"time"
)

// BackupStrategy defines backup strategy
type BackupStrategy string

const (
	FullBackup       BackupStrategy = "full"
	IncrementalBackup BackupStrategy = "incremental"
	DifferentialBackup BackupStrategy = "differential"
)

// RecoveryPoint represents a recovery point
type RecoveryPoint struct {
	ID        string
	Timestamp time.Time
	Strategy  BackupStrategy
	Size      int64
	Status    string // "COMPLETED", "IN_PROGRESS", "FAILED"
	Location  string
	Metadata  map[string]string
}

// DisasterRecoveryPlan represents a disaster recovery plan
type DisasterRecoveryPlan struct {
	ID                  string
	Name                string
	BackupStrategy      BackupStrategy
	BackupInterval      time.Duration
	RetentionDays       int
	RPOMinutes          int // Recovery Point Objective
	RTOMinutes          int // Recovery Time Objective
	PrimaryRegion       string
	SecondaryRegion     string
	Active              bool
	LastBackupTime      time.Time
	LastRecoveryTest    time.Time
}

// DisasterRecoveryManager manages disaster recovery
type DisasterRecoveryManager struct {
	plans           map[string]*DisasterRecoveryPlan
	recoveryPoints  map[string]*RecoveryPoint
	mu              sync.RWMutex
	maxRecoveryPoints int
}

// NewDisasterRecoveryManager creates a new disaster recovery manager
func NewDisasterRecoveryManager(maxRecoveryPoints int) *DisasterRecoveryManager {
	if maxRecoveryPoints == 0 {
		maxRecoveryPoints = 100
	}

	return &DisasterRecoveryManager{
		plans:             make(map[string]*DisasterRecoveryPlan),
		recoveryPoints:    make(map[string]*RecoveryPoint),
		maxRecoveryPoints: maxRecoveryPoints,
	}
}

// CreatePlan creates a new disaster recovery plan
func (drm *DisasterRecoveryManager) CreatePlan(plan *DisasterRecoveryPlan) error {
	drm.mu.Lock()
	defer drm.mu.Unlock()

	if plan.ID == "" {
		return fmt.Errorf("plan ID is required")
	}

	if plan.BackupInterval == 0 {
		plan.BackupInterval = 24 * time.Hour
	}

	if plan.RetentionDays == 0 {
		plan.RetentionDays = 30
	}

	if plan.RPOMinutes == 0 {
		plan.RPOMinutes = 60
	}

	if plan.RTOMinutes == 0 {
		plan.RTOMinutes = 120
	}

	plan.Active = true
	plan.LastBackupTime = time.Now()

	drm.plans[plan.ID] = plan
	return nil
}

// GetPlan retrieves a disaster recovery plan
func (drm *DisasterRecoveryManager) GetPlan(planID string) (*DisasterRecoveryPlan, error) {
	drm.mu.RLock()
	defer drm.mu.RUnlock()

	plan, exists := drm.plans[planID]
	if !exists {
		return nil, fmt.Errorf("plan not found: %s", planID)
	}

	return plan, nil
}

// ListPlans lists all disaster recovery plans
func (drm *DisasterRecoveryManager) ListPlans() []*DisasterRecoveryPlan {
	drm.mu.RLock()
	defer drm.mu.RUnlock()

	var plans []*DisasterRecoveryPlan
	for _, plan := range drm.plans {
		plans = append(plans, plan)
	}
	return plans
}

// CreateRecoveryPoint creates a new recovery point
func (drm *DisasterRecoveryManager) CreateRecoveryPoint(planID string, size int64, location string) (*RecoveryPoint, error) {
	drm.mu.Lock()
	defer drm.mu.Unlock()

	plan, exists := drm.plans[planID]
	if !exists {
		return nil, fmt.Errorf("plan not found: %s", planID)
	}

	// Check if max recovery points reached
	if len(drm.recoveryPoints) >= drm.maxRecoveryPoints {
		drm.evictOldestRecoveryPoint()
	}

	recoveryPoint := &RecoveryPoint{
		ID:        fmt.Sprintf("rp-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Strategy:  plan.BackupStrategy,
		Size:      size,
		Status:    "COMPLETED",
		Location:  location,
		Metadata: map[string]string{
			"plan_id": planID,
		},
	}

	drm.recoveryPoints[recoveryPoint.ID] = recoveryPoint
	plan.LastBackupTime = time.Now()

	return recoveryPoint, nil
}

// GetRecoveryPoint retrieves a recovery point
func (drm *DisasterRecoveryManager) GetRecoveryPoint(rpID string) (*RecoveryPoint, error) {
	drm.mu.RLock()
	defer drm.mu.RUnlock()

	rp, exists := drm.recoveryPoints[rpID]
	if !exists {
		return nil, fmt.Errorf("recovery point not found: %s", rpID)
	}

	return rp, nil
}

// ListRecoveryPoints lists all recovery points
func (drm *DisasterRecoveryManager) ListRecoveryPoints(planID string) []*RecoveryPoint {
	drm.mu.RLock()
	defer drm.mu.RUnlock()

	var rps []*RecoveryPoint
	for _, rp := range drm.recoveryPoints {
		if rp.Metadata["plan_id"] == planID {
			rps = append(rps, rp)
		}
	}
	return rps
}

// InitiateRecovery initiates recovery from a recovery point
func (drm *DisasterRecoveryManager) InitiateRecovery(rpID string) error {
	drm.mu.Lock()
	defer drm.mu.Unlock()

	rp, exists := drm.recoveryPoints[rpID]
	if !exists {
		return fmt.Errorf("recovery point not found: %s", rpID)
	}

	// Simulate recovery
	rp.Status = "IN_PROGRESS"

	// Update recovery status
	rp.Status = "COMPLETED"

	return nil
}

// TestRecovery tests recovery from a recovery point
func (drm *DisasterRecoveryManager) TestRecovery(planID string) error {
	drm.mu.Lock()
	defer drm.mu.Unlock()

	plan, exists := drm.plans[planID]
	if !exists {
		return fmt.Errorf("plan not found: %s", planID)
	}

	// Simulate recovery test
	plan.LastRecoveryTest = time.Now()

	return nil
}

// ShouldBackup checks if backup is needed
func (drm *DisasterRecoveryManager) ShouldBackup(planID string) bool {
	drm.mu.RLock()
	defer drm.mu.RUnlock()

	plan, exists := drm.plans[planID]
	if !exists {
		return false
	}

	return time.Since(plan.LastBackupTime) > plan.BackupInterval
}

// DeleteRecoveryPoint deletes a recovery point
func (drm *DisasterRecoveryManager) DeleteRecoveryPoint(rpID string) error {
	drm.mu.Lock()
	defer drm.mu.Unlock()

	_, exists := drm.recoveryPoints[rpID]
	if !exists {
		return fmt.Errorf("recovery point not found: %s", rpID)
	}

	delete(drm.recoveryPoints, rpID)
	return nil
}

// GetRecoveryPointCount returns the number of recovery points
func (drm *DisasterRecoveryManager) GetRecoveryPointCount() int {
	drm.mu.RLock()
	defer drm.mu.RUnlock()

	return len(drm.recoveryPoints)
}

// GetPlanCount returns the number of plans
func (drm *DisasterRecoveryManager) GetPlanCount() int {
	drm.mu.RLock()
	defer drm.mu.RUnlock()

	return len(drm.plans)
}

// GetTotalBackupSize returns the total backup size
func (drm *DisasterRecoveryManager) GetTotalBackupSize() int64 {
	drm.mu.RLock()
	defer drm.mu.RUnlock()

	totalSize := int64(0)
	for _, rp := range drm.recoveryPoints {
		totalSize += rp.Size
	}
	return totalSize
}

// evictOldestRecoveryPoint evicts the oldest recovery point (internal, must hold lock)
func (drm *DisasterRecoveryManager) evictOldestRecoveryPoint() {
	var oldestID string
	var oldestTime time.Time

	for id, rp := range drm.recoveryPoints {
		if oldestTime.IsZero() || rp.Timestamp.Before(oldestTime) {
			oldestID = id
			oldestTime = rp.Timestamp
		}
	}

	if oldestID != "" {
		delete(drm.recoveryPoints, oldestID)
	}
}

// GetRecoveryStats returns recovery statistics
func (drm *DisasterRecoveryManager) GetRecoveryStats() map[string]interface{} {
	drm.mu.RLock()
	defer drm.mu.RUnlock()

	return map[string]interface{}{
		"plan_count":           len(drm.plans),
		"recovery_point_count": len(drm.recoveryPoints),
		"total_backup_size":    drm.getTotalBackupSize(),
		"max_recovery_points":  drm.maxRecoveryPoints,
	}
}

// getTotalBackupSize returns the total backup size (internal, must hold lock)
func (drm *DisasterRecoveryManager) getTotalBackupSize() int64 {
	totalSize := int64(0)
	for _, rp := range drm.recoveryPoints {
		totalSize += rp.Size
	}
	return totalSize
}

// ActivatePlan activates a disaster recovery plan
func (drm *DisasterRecoveryManager) ActivatePlan(planID string) error {
	drm.mu.Lock()
	defer drm.mu.Unlock()

	plan, exists := drm.plans[planID]
	if !exists {
		return fmt.Errorf("plan not found: %s", planID)
	}

	plan.Active = true
	return nil
}

// DeactivatePlan deactivates a disaster recovery plan
func (drm *DisasterRecoveryManager) DeactivatePlan(planID string) error {
	drm.mu.Lock()
	defer drm.mu.Unlock()

	plan, exists := drm.plans[planID]
	if !exists {
		return fmt.Errorf("plan not found: %s", planID)
	}

	plan.Active = false
	return nil
}

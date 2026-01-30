package scaling_test

import (
	"strconv"
	"testing"
	"time"

	"streamgate/pkg/scaling"
)

func TestDisasterRecoveryManager_CreatePlan(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:              "plan-1",
		Name:            "Primary Backup",
		BackupStrategy:  scaling.FullBackup,
		BackupInterval:  24 * time.Hour,
		RetentionDays:   30,
		RPOMinutes:      60,
		RTOMinutes:      120,
		PrimaryRegion:   "us-east-1",
		SecondaryRegion: "eu-west-1",
	}

	err := drm.CreatePlan(plan)
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	if drm.GetPlanCount() != 1 {
		t.Errorf("Expected 1 plan, got %d", drm.GetPlanCount())
	}
}

func TestDisasterRecoveryManager_GetPlan(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	retrieved, err := drm.GetPlan("plan-1")
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}

	if retrieved.ID != "plan-1" {
		t.Errorf("Plan ID doesn't match")
	}
}

func TestDisasterRecoveryManager_ListPlans(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	for i := 1; i <= 3; i++ {
		plan := &scaling.DisasterRecoveryPlan{
			ID:             "plan-" + string(rune(i)),
			Name:           "Plan " + string(rune(i)),
			BackupStrategy: scaling.FullBackup,
		}
		drm.CreatePlan(plan)
	}

	plans := drm.ListPlans()
	if len(plans) != 3 {
		t.Errorf("Expected 3 plans, got %d", len(plans))
	}
}

func TestDisasterRecoveryManager_CreateRecoveryPoint(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	rp, err := drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-1")
	if err != nil {
		t.Fatalf("CreateRecoveryPoint failed: %v", err)
	}

	if rp.Status != "COMPLETED" {
		t.Errorf("Expected COMPLETED status, got %s", rp.Status)
	}

	if drm.GetRecoveryPointCount() != 1 {
		t.Errorf("Expected 1 recovery point, got %d", drm.GetRecoveryPointCount())
	}
}

func TestDisasterRecoveryManager_GetRecoveryPoint(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	rp, _ := drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-1")

	retrieved, err := drm.GetRecoveryPoint(rp.ID)
	if err != nil {
		t.Fatalf("GetRecoveryPoint failed: %v", err)
	}

	if retrieved.ID != rp.ID {
		t.Errorf("Recovery point ID doesn't match")
	}
}

func TestDisasterRecoveryManager_ListRecoveryPoints(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	for i := 1; i <= 3; i++ {
		drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-"+strconv.Itoa(i))
		time.Sleep(1 * time.Millisecond)
	}

	rps := drm.ListRecoveryPoints("plan-1")
	if len(rps) != 3 {
		t.Errorf("Expected 3 recovery points, got %d", len(rps))
	}
}

func TestDisasterRecoveryManager_InitiateRecovery(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	rp, _ := drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-1")

	err := drm.InitiateRecovery(rp.ID)
	if err != nil {
		t.Fatalf("InitiateRecovery failed: %v", err)
	}

	retrieved, _ := drm.GetRecoveryPoint(rp.ID)
	if retrieved.Status != "COMPLETED" {
		t.Errorf("Expected COMPLETED status after recovery, got %s", retrieved.Status)
	}
}

func TestDisasterRecoveryManager_TestRecovery(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	err := drm.TestRecovery("plan-1")
	if err != nil {
		t.Fatalf("TestRecovery failed: %v", err)
	}

	retrieved, _ := drm.GetPlan("plan-1")
	if retrieved.LastRecoveryTest.IsZero() {
		t.Error("Last recovery test time not updated")
	}
}

func TestDisasterRecoveryManager_ShouldBackup(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
		BackupInterval: 1 * time.Millisecond,
	}

	drm.CreatePlan(plan)

	if drm.ShouldBackup("plan-1") {
		t.Error("Should not backup immediately")
	}

	time.Sleep(2 * time.Millisecond)

	if !drm.ShouldBackup("plan-1") {
		t.Error("Should backup after interval")
	}
}

func TestDisasterRecoveryManager_DeleteRecoveryPoint(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	rp, _ := drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-1")

	err := drm.DeleteRecoveryPoint(rp.ID)
	if err != nil {
		t.Fatalf("DeleteRecoveryPoint failed: %v", err)
	}

	if drm.GetRecoveryPointCount() != 0 {
		t.Errorf("Expected 0 recovery points, got %d", drm.GetRecoveryPointCount())
	}
}

func TestDisasterRecoveryManager_GetTotalBackupSize(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-1")
	drm.CreateRecoveryPoint("plan-1", 2048*1024, "s3://backups/rp-2")

	totalSize := drm.GetTotalBackupSize()
	if totalSize != 3072*1024 {
		t.Errorf("Expected 3072KB, got %d bytes", totalSize)
	}
}

func TestDisasterRecoveryManager_ActivateDeactivatePlan(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	err := drm.DeactivatePlan("plan-1")
	if err != nil {
		t.Fatalf("DeactivatePlan failed: %v", err)
	}

	retrieved, _ := drm.GetPlan("plan-1")
	if retrieved.Active {
		t.Error("Plan should be inactive")
	}

	err = drm.ActivatePlan("plan-1")
	if err != nil {
		t.Fatalf("ActivatePlan failed: %v", err)
	}

	retrieved, _ = drm.GetPlan("plan-1")
	if !retrieved.Active {
		t.Error("Plan should be active")
	}
}

func TestDisasterRecoveryManager_GetRecoveryStats(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)
	drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-1")

	stats := drm.GetRecoveryStats()
	if stats["plan_count"] != 1 {
		t.Errorf("Expected 1 plan in stats")
	}

	if stats["recovery_point_count"] != 1 {
		t.Errorf("Expected 1 recovery point in stats")
	}
}

func TestDisasterRecoveryManager_RecoveryPointEviction(t *testing.T) {
	drm := scaling.NewDisasterRecoveryManager(2) // Max 2 recovery points

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	// Create 3 recovery points
	drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-1")
	drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-2")
	drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-3")

	// Should have evicted oldest (rp-1)
	if drm.GetRecoveryPointCount() != 2 {
		t.Errorf("Expected 2 recovery points after eviction, got %d", drm.GetRecoveryPointCount())
	}
}

func BenchmarkDisasterRecoveryManager_CreateRecoveryPoint(b *testing.B) {
	drm := scaling.NewDisasterRecoveryManager(1000)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-"+string(rune(i)))
	}
}

func BenchmarkDisasterRecoveryManager_InitiateRecovery(b *testing.B) {
	drm := scaling.NewDisasterRecoveryManager(100)

	plan := &scaling.DisasterRecoveryPlan{
		ID:             "plan-1",
		Name:           "Primary Backup",
		BackupStrategy: scaling.FullBackup,
	}

	drm.CreatePlan(plan)

	rp, _ := drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		drm.InitiateRecovery(rp.ID)
	}
}

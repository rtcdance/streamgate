package scaling_test

import (
	"testing"
	"time"

	"streamgate/pkg/scaling"
)

func TestMultiRegionManager_RegisterRegion(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region := &scaling.Region{
		ID:       "us-east-1",
		Name:     "US East",
		Location: "Virginia",
		Endpoint: "https://us-east-1.example.com",
		Active:   true,
	}

	err := mrm.RegisterRegion(region)
	if err != nil {
		t.Fatalf("RegisterRegion failed: %v", err)
	}

	if mrm.GetRegionCount() != 1 {
		t.Errorf("Expected 1 region, got %d", mrm.GetRegionCount())
	}
}

func TestMultiRegionManager_GetRegion(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region := &scaling.Region{
		ID:       "us-east-1",
		Name:     "US East",
		Location: "Virginia",
		Endpoint: "https://us-east-1.example.com",
		Active:   true,
	}

	mrm.RegisterRegion(region)

	retrieved, err := mrm.GetRegion("us-east-1")
	if err != nil {
		t.Fatalf("GetRegion failed: %v", err)
	}

	if retrieved.ID != "us-east-1" {
		t.Errorf("Region ID doesn't match")
	}
}

func TestMultiRegionManager_ListRegions(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	regions := []string{"us-east-1", "eu-west-1", "ap-southeast-1"}
	for _, regionID := range regions {
		region := &scaling.Region{
			ID:       regionID,
			Name:     regionID,
			Location: regionID,
			Endpoint: "https://" + regionID + ".example.com",
			Active:   true,
		}
		mrm.RegisterRegion(region)
	}

	listed := mrm.ListRegions()
	if len(listed) != 3 {
		t.Errorf("Expected 3 regions, got %d", len(listed))
	}
}

func TestMultiRegionManager_GetActiveRegions(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region1 := &scaling.Region{
		ID:     "us-east-1",
		Name:   "US East",
		Active: true,
	}
	region2 := &scaling.Region{
		ID:     "eu-west-1",
		Name:   "EU West",
		Active: false,
	}

	mrm.RegisterRegion(region1)
	mrm.RegisterRegion(region2)

	active := mrm.GetActiveRegions()
	if len(active) != 1 {
		t.Errorf("Expected 1 active region, got %d", len(active))
	}
}

func TestMultiRegionManager_GetPrimaryRegion(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region := &scaling.Region{
		ID:     "us-east-1",
		Name:   "US East",
		Active: true,
	}

	mrm.RegisterRegion(region)

	primary, err := mrm.GetPrimaryRegion()
	if err != nil {
		t.Fatalf("GetPrimaryRegion failed: %v", err)
	}

	if primary.ID != "us-east-1" {
		t.Errorf("Primary region ID doesn't match")
	}
}

func TestMultiRegionManager_SetPrimaryRegion(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region1 := &scaling.Region{ID: "us-east-1", Name: "US East", Active: true}
	region2 := &scaling.Region{ID: "eu-west-1", Name: "EU West", Active: true}

	mrm.RegisterRegion(region1)
	mrm.RegisterRegion(region2)

	err := mrm.SetPrimaryRegion("eu-west-1")
	if err != nil {
		t.Fatalf("SetPrimaryRegion failed: %v", err)
	}

	primary, _ := mrm.GetPrimaryRegion()
	if primary.ID != "eu-west-1" {
		t.Errorf("Primary region not updated")
	}
}

func TestMultiRegionManager_ActivateDeactivateRegion(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region := &scaling.Region{
		ID:     "us-east-1",
		Name:   "US East",
		Active: true,
	}

	mrm.RegisterRegion(region)

	err := mrm.DeactivateRegion("us-east-1")
	if err != nil {
		t.Fatalf("DeactivateRegion failed: %v", err)
	}

	if mrm.GetActiveRegionCount() != 0 {
		t.Errorf("Expected 0 active regions, got %d", mrm.GetActiveRegionCount())
	}

	err = mrm.ActivateRegion("us-east-1")
	if err != nil {
		t.Fatalf("ActivateRegion failed: %v", err)
	}

	if mrm.GetActiveRegionCount() != 1 {
		t.Errorf("Expected 1 active region, got %d", mrm.GetActiveRegionCount())
	}
}

func TestMultiRegionManager_UpdateRegionLatency(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region := &scaling.Region{
		ID:     "us-east-1",
		Name:   "US East",
		Active: true,
	}

	mrm.RegisterRegion(region)

	err := mrm.UpdateRegionLatency("us-east-1", 50)
	if err != nil {
		t.Fatalf("UpdateRegionLatency failed: %v", err)
	}

	retrieved, _ := mrm.GetRegion("us-east-1")
	if retrieved.Latency != 50 {
		t.Errorf("Latency not updated")
	}
}

func TestMultiRegionManager_RecordRequest(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region := &scaling.Region{
		ID:     "us-east-1",
		Name:   "US East",
		Active: true,
	}

	mrm.RegisterRegion(region)

	err := mrm.RecordRequest("us-east-1", 50, true)
	if err != nil {
		t.Fatalf("RecordRequest failed: %v", err)
	}

	metrics, _ := mrm.GetRegionMetrics("us-east-1")
	if metrics.RequestCount != 1 {
		t.Errorf("Expected 1 request, got %d", metrics.RequestCount)
	}
}

func TestMultiRegionManager_GetRegionMetrics(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region := &scaling.Region{
		ID:     "us-east-1",
		Name:   "US East",
		Active: true,
	}

	mrm.RegisterRegion(region)

	metrics, err := mrm.GetRegionMetrics("us-east-1")
	if err != nil {
		t.Fatalf("GetRegionMetrics failed: %v", err)
	}

	if metrics.RegionID != "us-east-1" {
		t.Errorf("Metrics region ID doesn't match")
	}
}

func TestMultiRegionManager_PerformHealthCheck(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region := &scaling.Region{
		ID:      "us-east-1",
		Name:    "US East",
		Active:  true,
		Latency: 100,
	}

	mrm.RegisterRegion(region)

	err := mrm.PerformHealthCheck()
	if err != nil {
		t.Fatalf("PerformHealthCheck failed: %v", err)
	}

	metrics, _ := mrm.GetRegionMetrics("us-east-1")
	if metrics.HealthStatus != "HEALTHY" {
		t.Errorf("Expected HEALTHY status, got %s", metrics.HealthStatus)
	}
}

func TestMultiRegionManager_ShouldHealthCheck(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(1 * time.Millisecond)

	if mrm.ShouldHealthCheck() {
		t.Error("Should not health check immediately")
	}

	time.Sleep(2 * time.Millisecond)

	if !mrm.ShouldHealthCheck() {
		t.Error("Should health check after interval")
	}
}

func TestMultiRegionManager_GetAllMetrics(t *testing.T) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	for i := 1; i <= 3; i++ {
		region := &scaling.Region{
			ID:     "region-" + string(rune(i)),
			Name:   "Region " + string(rune(i)),
			Active: true,
		}
		mrm.RegisterRegion(region)
	}

	metrics := mrm.GetAllMetrics()
	if len(metrics) != 3 {
		t.Errorf("Expected 3 metrics, got %d", len(metrics))
	}
}

func BenchmarkMultiRegionManager_RegisterRegion(b *testing.B) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		region := &scaling.Region{
			ID:     "region-" + string(rune(i)),
			Name:   "Region " + string(rune(i)),
			Active: true,
		}
		mrm.RegisterRegion(region)
	}
}

func BenchmarkMultiRegionManager_RecordRequest(b *testing.B) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	region := &scaling.Region{
		ID:     "us-east-1",
		Name:   "US East",
		Active: true,
	}
	mrm.RegisterRegion(region)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mrm.RecordRequest("us-east-1", 50, true)
	}
}

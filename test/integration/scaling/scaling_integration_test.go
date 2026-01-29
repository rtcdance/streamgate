package scaling_test

import (
	"testing"
	"time"

	"streamgate/pkg/scaling"
)

func TestScalingIntegration_MultiRegionWithCDN(t *testing.T) {
	// Setup multi-region manager
	mrm := scaling.NewMultiRegionManager(30 * time.Second)

	// Setup CDN manager
	cdnConfig := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(cdnConfig, 1024*1024)

	// Register regions
	for i := 1; i <= 3; i++ {
		region := &scaling.Region{
			ID:       "region-" + string(rune(i)),
			Name:     "Region " + string(rune(i)),
			Location: "Location " + string(rune(i)),
			Endpoint: "https://region-" + string(rune(i)) + ".example.com",
			Active:   true,
		}
		mrm.RegisterRegion(region)
	}

	// Cache content
	cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 1024)

	// Record requests across regions
	for i := 1; i <= 3; i++ {
		regionID := "region-" + string(rune(i))
		mrm.RecordRequest(regionID, 50, true)
	}

	// Verify multi-region metrics
	if mrm.GetRegionCount() != 3 {
		t.Errorf("Expected 3 regions, got %d", mrm.GetRegionCount())
	}

	// Verify CDN metrics
	metrics := cm.GetMetrics()
	if metrics.CacheCount != 1 {
		t.Errorf("Expected 1 cached item, got %d", metrics.CacheCount)
	}
}

func TestScalingIntegration_CDNWithLoadBalancing(t *testing.T) {
	// Setup CDN manager
	cdnConfig := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(cdnConfig, 1024*1024)

	// Setup load balancer
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	// Register backends
	for i := 1; i <= 3; i++ {
		backend := &scaling.Backend{
			ID:      "backend-" + string(rune(i)),
			Address: "192.168.1." + string(rune(i)),
			Port:    8080,
			Region:  "region-" + string(rune(i)),
		}
		glb.RegisterBackend(backend)
	}

	// Cache content
	cm.CacheContent("key-1", "https://example.com/file.mp4", 3600, 1024)

	// Select backends and record requests
	for i := 0; i < 5; i++ {
		selected, _ := glb.SelectBackend()
		glb.RecordRequest(selected.ID, 50, true)
		cm.GetCachedContent("key-1")
	}

	// Verify load balancing
	if glb.GetBackendCount() != 3 {
		t.Errorf("Expected 3 backends, got %d", glb.GetBackendCount())
	}

	// Verify CDN hit rate
	cdnMetrics := cm.GetMetrics()
	if cdnMetrics.HitRate != 100 {
		t.Errorf("Expected 100%% hit rate, got %.2f%%", cdnMetrics.HitRate)
	}
}

func TestScalingIntegration_LoadBalancingWithDisasterRecovery(t *testing.T) {
	// Setup load balancer
	glb := scaling.NewGlobalLoadBalancer(scaling.LeastConn, 30*time.Second)

	// Setup disaster recovery
	drm := scaling.NewDisasterRecoveryManager(100)

	// Register backends
	for i := 1; i <= 3; i++ {
		backend := &scaling.Backend{
			ID:          "backend-" + string(rune(i)),
			Address:     "192.168.1." + string(rune(i)),
			Port:        8080,
			Region:      "region-" + string(rune(i)),
			Connections: int64(i),
		}
		glb.RegisterBackend(backend)
	}

	// Create disaster recovery plan
	plan := &scaling.DisasterRecoveryPlan{
		ID:              "plan-1",
		Name:            "Primary Backup",
		BackupStrategy:  scaling.FullBackup,
		BackupInterval:  24 * time.Hour,
		RetentionDays:   30,
		RPOMinutes:      60,
		RTOMinutes:      120,
		PrimaryRegion:   "region-1",
		SecondaryRegion: "region-2",
	}

	drm.CreatePlan(plan)

	// Create recovery points
	for i := 1; i <= 3; i++ {
		drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-"+string(rune(i)))
	}

	// Select backend with least connections
	selected, _ := glb.SelectBackend()
	if selected.ID != "backend-1" {
		t.Error("Should select backend with least connections")
	}

	// Verify disaster recovery
	if drm.GetRecoveryPointCount() != 3 {
		t.Errorf("Expected 3 recovery points, got %d", drm.GetRecoveryPointCount())
	}
}

func TestScalingIntegration_FullGlobalScalingStack(t *testing.T) {
	// Setup all components
	mrm := scaling.NewMultiRegionManager(30 * time.Second)
	cdnConfig := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(cdnConfig, 1024*1024)
	glb := scaling.NewGlobalLoadBalancer(scaling.LatencyBased, 30*time.Second)
	drm := scaling.NewDisasterRecoveryManager(100)

	// Step 1: Register regions
	for i := 1; i <= 3; i++ {
		region := &scaling.Region{
			ID:       "region-" + string(rune(i)),
			Name:     "Region " + string(rune(i)),
			Location: "Location " + string(rune(i)),
			Endpoint: "https://region-" + string(rune(i)) + ".example.com",
			Active:   true,
			Latency:  int64(50 * i),
		}
		mrm.RegisterRegion(region)
	}

	// Step 2: Register backends
	for i := 1; i <= 3; i++ {
		backend := &scaling.Backend{
			ID:      "backend-" + string(rune(i)),
			Address: "192.168.1." + string(rune(i)),
			Port:    8080,
			Region:  "region-" + string(rune(i)),
			Latency: int64(50 * i),
		}
		glb.RegisterBackend(backend)
	}

	// Step 3: Cache content
	for i := 1; i <= 5; i++ {
		cm.CacheContent("key-"+string(rune(i)), "https://example.com/file"+string(rune(i))+".mp4", 3600, 1024)
	}

	// Step 4: Create disaster recovery plan
	plan := &scaling.DisasterRecoveryPlan{
		ID:              "plan-1",
		Name:            "Primary Backup",
		BackupStrategy:  scaling.FullBackup,
		PrimaryRegion:   "region-1",
		SecondaryRegion: "region-2",
	}
	drm.CreatePlan(plan)

	// Step 5: Simulate traffic
	for i := 0; i < 10; i++ {
		// Select backend
		selected, _ := glb.SelectBackend()
		glb.IncrementConnections(selected.ID)

		// Record request
		glb.RecordRequest(selected.ID, int64(50*i), true)

		// Access cache
		cm.GetCachedContent("key-1")

		// Record region request
		mrm.RecordRequest("region-1", int64(50*i), true)

		glb.DecrementConnections(selected.ID)
	}

	// Step 6: Create recovery points
	for i := 1; i <= 3; i++ {
		drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-"+string(rune(i)))
	}

	// Verify all components
	if mrm.GetRegionCount() != 3 {
		t.Errorf("Expected 3 regions, got %d", mrm.GetRegionCount())
	}

	if glb.GetBackendCount() != 3 {
		t.Errorf("Expected 3 backends, got %d", glb.GetBackendCount())
	}

	if cm.GetCacheCount() != 5 {
		t.Errorf("Expected 5 cached items, got %d", cm.GetCacheCount())
	}

	if drm.GetRecoveryPointCount() != 3 {
		t.Errorf("Expected 3 recovery points, got %d", drm.GetRecoveryPointCount())
	}

	// Verify metrics
	cdnMetrics := cm.GetMetrics()
	if cdnMetrics.TotalRequests < 10 {
		t.Errorf("Expected at least 10 total requests, got %d", cdnMetrics.TotalRequests)
	}
}

func TestScalingIntegration_FailoverScenario(t *testing.T) {
	// Setup components
	mrm := scaling.NewMultiRegionManager(30 * time.Second)
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	// Register regions
	region1 := &scaling.Region{
		ID:       "region-1",
		Name:     "Primary",
		Location: "US East",
		Endpoint: "https://region-1.example.com",
		Active:   true,
		Latency:  100,
	}
	region2 := &scaling.Region{
		ID:       "region-2",
		Name:     "Secondary",
		Location: "EU West",
		Endpoint: "https://region-2.example.com",
		Active:   true,
		Latency:  200,
	}

	mrm.RegisterRegion(region1)
	mrm.RegisterRegion(region2)

	// Register backends
	backend1 := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "region-1",
		Active:  true,
		Latency: 100,
	}
	backend2 := &scaling.Backend{
		ID:      "backend-2",
		Address: "192.168.1.2",
		Port:    8080,
		Region:  "region-2",
		Active:  true,
		Latency: 200,
	}

	glb.RegisterBackend(backend1)
	glb.RegisterBackend(backend2)

	// Simulate primary region failure
	mrm.UpdateRegionLatency("region-1", 6000) // 6 seconds - unhealthy
	glb.RecordRequest("backend-1", 6000, false)

	// Perform health checks
	mrm.PerformHealthCheck()
	glb.PerformHealthCheck()

	// Verify failover
	if mrm.GetActiveRegionCount() != 1 {
		t.Errorf("Expected 1 active region after failover, got %d", mrm.GetActiveRegionCount())
	}

	if glb.GetActiveBackendCount() != 1 {
		t.Errorf("Expected 1 active backend after failover, got %d", glb.GetActiveBackendCount())
	}

	// Verify secondary region is active
	activeRegions := mrm.GetActiveRegions()
	if len(activeRegions) > 0 && activeRegions[0].ID != "region-2" {
		t.Error("Secondary region should be active")
	}
}

func TestScalingIntegration_DisasterRecoveryFlow(t *testing.T) {
	// Setup disaster recovery
	drm := scaling.NewDisasterRecoveryManager(100)

	// Create backup plan
	plan := &scaling.DisasterRecoveryPlan{
		ID:              "plan-1",
		Name:            "Primary Backup",
		BackupStrategy:  scaling.FullBackup,
		BackupInterval:  1 * time.Millisecond,
		RetentionDays:   30,
		RPOMinutes:      60,
		RTOMinutes:      120,
		PrimaryRegion:   "region-1",
		SecondaryRegion: "region-2",
	}

	drm.CreatePlan(plan)

	// Create initial recovery point
	rp1, _ := drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-1")

	// Wait for backup interval
	time.Sleep(2 * time.Millisecond)

	// Check if backup is needed
	if !drm.ShouldBackup("plan-1") {
		t.Error("Should backup after interval")
	}

	// Create new recovery point
	rp2, _ := drm.CreateRecoveryPoint("plan-1", 2048*1024, "s3://backups/rp-2")

	// Test recovery
	err := drm.InitiateRecovery(rp2.ID)
	if err != nil {
		t.Fatalf("InitiateRecovery failed: %v", err)
	}

	// Verify recovery points
	rps := drm.ListRecoveryPoints("plan-1")
	if len(rps) != 2 {
		t.Errorf("Expected 2 recovery points, got %d", len(rps))
	}

	// Verify total backup size
	totalSize := drm.GetTotalBackupSize()
	if totalSize != 3072*1024 {
		t.Errorf("Expected 3072KB total, got %d bytes", totalSize)
	}
}

func BenchmarkScalingIntegration_MultiRegionWithCDN(b *testing.B) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)
	cdnConfig := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(cdnConfig, 1024*1024*1024)

	for i := 1; i <= 3; i++ {
		region := &scaling.Region{
			ID:     "region-" + string(rune(i)),
			Name:   "Region " + string(rune(i)),
			Active: true,
		}
		mrm.RegisterRegion(region)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.CacheContent("key-"+string(rune(i)), "https://example.com/file.mp4", 3600, 1024)
		mrm.RecordRequest("region-1", 50, true)
	}
}

func BenchmarkScalingIntegration_FullStack(b *testing.B) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)
	cdnConfig := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(cdnConfig, 1024*1024*1024)
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)
	drm := scaling.NewDisasterRecoveryManager(1000)

	for i := 1; i <= 3; i++ {
		region := &scaling.Region{ID: "region-" + string(rune(i)), Name: "Region " + string(rune(i)), Active: true}
		mrm.RegisterRegion(region)

		backend := &scaling.Backend{ID: "backend-" + string(rune(i)), Address: "192.168.1." + string(rune(i)), Port: 8080}
		glb.RegisterBackend(backend)
	}

	plan := &scaling.DisasterRecoveryPlan{ID: "plan-1", Name: "Backup", BackupStrategy: scaling.FullBackup}
	drm.CreatePlan(plan)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.CacheContent("key-"+string(rune(i)), "https://example.com/file.mp4", 3600, 1024)
		glb.SelectBackend()
		mrm.RecordRequest("region-1", 50, true)
		drm.CreateRecoveryPoint("plan-1", 1024*1024, "s3://backups/rp-"+string(rune(i)))
	}
}

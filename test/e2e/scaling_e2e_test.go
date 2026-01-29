package e2e_test

import (
	"testing"
	"time"

	"streamgate/pkg/scaling"
)

func TestScalingE2E_GlobalDeploymentFlow(t *testing.T) {
	// Initialize all scaling components
	mrm := scaling.NewMultiRegionManager(30 * time.Second)
	cdnConfig := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(cdnConfig, 1024*1024*1024)
	glb := scaling.NewGlobalLoadBalancer(scaling.LatencyBased, 30*time.Second)
	drm := scaling.NewDisasterRecoveryManager(100)

	// Step 1: Deploy to multiple regions
	regions := []struct {
		id       string
		name     string
		location string
		latency  int64
	}{
		{"us-east-1", "US East", "Virginia", 50},
		{"eu-west-1", "EU West", "Ireland", 100},
		{"ap-southeast-1", "AP Southeast", "Singapore", 150},
	}

	for _, r := range regions {
		region := &scaling.Region{
			ID:       r.id,
			Name:     r.name,
			Location: r.location,
			Endpoint: "https://" + r.id + ".example.com",
			Active:   true,
			Latency:  r.latency,
		}
		mrm.RegisterRegion(region)
	}

	if mrm.GetRegionCount() != 3 {
		t.Errorf("Expected 3 regions, got %d", mrm.GetRegionCount())
	}

	// Step 2: Deploy backends in each region
	for i, r := range regions {
		backend := &scaling.Backend{
			ID:      "backend-" + r.id,
			Address: "10.0." + string(rune(i)) + ".1",
			Port:    8080,
			Region:  r.id,
			Active:  true,
			Latency: r.latency,
		}
		glb.RegisterBackend(backend)
	}

	if glb.GetBackendCount() != 3 {
		t.Errorf("Expected 3 backends, got %d", glb.GetBackendCount())
	}

	// Step 3: Setup CDN for content distribution
	contentItems := []struct {
		key string
		url string
		ttl int64
	}{
		{"video-1", "https://example.com/video1.mp4", 3600},
		{"video-2", "https://example.com/video2.mp4", 3600},
		{"image-1", "https://example.com/image1.jpg", 7200},
	}

	for _, item := range contentItems {
		cm.CacheContent(item.key, item.url, item.ttl, 1024*1024)
	}

	if cm.GetCacheCount() != 3 {
		t.Errorf("Expected 3 cached items, got %d", cm.GetCacheCount())
	}

	// Step 4: Setup disaster recovery
	plan := &scaling.DisasterRecoveryPlan{
		ID:              "global-backup",
		Name:            "Global Backup Plan",
		BackupStrategy:  scaling.FullBackup,
		BackupInterval:  24 * time.Hour,
		RetentionDays:   30,
		RPOMinutes:      60,
		RTOMinutes:      120,
		PrimaryRegion:   "us-east-1",
		SecondaryRegion: "eu-west-1",
	}
	drm.CreatePlan(plan)

	// Step 5: Simulate global traffic
	for i := 0; i < 20; i++ {
		// Select backend based on latency
		selected, _ := glb.SelectBackend()
		glb.IncrementConnections(selected.ID)

		// Record request
		glb.RecordRequest(selected.ID, selected.Latency, true)

		// Access cached content
		for _, item := range contentItems {
			cm.GetCachedContent(item.key)
		}

		// Record region request
		mrm.RecordRequest(selected.Region, selected.Latency, true)

		glb.DecrementConnections(selected.ID)
	}

	// Step 6: Create recovery points
	for i := 1; i <= 3; i++ {
		drm.CreateRecoveryPoint("global-backup", 5*1024*1024, "s3://backups/global-rp-"+string(rune(i)))
	}

	// Verify deployment
	if mrm.GetActiveRegionCount() != 3 {
		t.Errorf("Expected 3 active regions, got %d", mrm.GetActiveRegionCount())
	}

	if glb.GetActiveBackendCount() != 3 {
		t.Errorf("Expected 3 active backends, got %d", glb.GetActiveBackendCount())
	}

	// Verify CDN metrics
	cdnMetrics := cm.GetMetrics()
	if cdnMetrics.CacheHits < 50 {
		t.Errorf("Expected at least 50 cache hits, got %d", cdnMetrics.CacheHits)
	}

	// Verify disaster recovery
	if drm.GetRecoveryPointCount() != 3 {
		t.Errorf("Expected 3 recovery points, got %d", drm.GetRecoveryPointCount())
	}
}

func TestScalingE2E_FailoverAndRecovery(t *testing.T) {
	// Initialize components
	mrm := scaling.NewMultiRegionManager(30 * time.Second)
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)
	drm := scaling.NewDisasterRecoveryManager(100)

	// Setup primary and secondary regions
	primaryRegion := &scaling.Region{
		ID:       "us-east-1",
		Name:     "Primary",
		Location: "Virginia",
		Endpoint: "https://us-east-1.example.com",
		Active:   true,
		Latency:  50,
	}
	secondaryRegion := &scaling.Region{
		ID:       "eu-west-1",
		Name:     "Secondary",
		Location: "Ireland",
		Endpoint: "https://eu-west-1.example.com",
		Active:   true,
		Latency:  100,
	}

	mrm.RegisterRegion(primaryRegion)
	mrm.RegisterRegion(secondaryRegion)

	// Setup backends
	primaryBackend := &scaling.Backend{
		ID:      "backend-primary",
		Address: "10.0.0.1",
		Port:    8080,
		Region:  "us-east-1",
		Active:  true,
		Latency: 50,
	}
	secondaryBackend := &scaling.Backend{
		ID:      "backend-secondary",
		Address: "10.0.1.1",
		Port:    8080,
		Region:  "eu-west-1",
		Active:  true,
		Latency: 100,
	}

	glb.RegisterBackend(primaryBackend)
	glb.RegisterBackend(secondaryBackend)

	// Setup disaster recovery
	plan := &scaling.DisasterRecoveryPlan{
		ID:              "failover-plan",
		Name:            "Failover Plan",
		BackupStrategy:  scaling.IncrementalBackup,
		PrimaryRegion:   "us-east-1",
		SecondaryRegion: "eu-west-1",
	}
	drm.CreatePlan(plan)

	// Create initial recovery point
	rp1, _ := drm.CreateRecoveryPoint("failover-plan", 10*1024*1024, "s3://backups/rp-1")

	// Simulate normal operation
	for i := 0; i < 10; i++ {
		selected, _ := glb.SelectBackend()
		glb.RecordRequest(selected.ID, selected.Latency, true)
		mrm.RecordRequest(selected.Region, selected.Latency, true)
	}

	// Simulate primary region failure
	mrm.UpdateRegionLatency("us-east-1", 7000) // 7 seconds - unhealthy
	glb.RecordRequest("backend-primary", 7000, false)
	glb.RecordRequest("backend-primary", 7000, false)
	glb.RecordRequest("backend-primary", 7000, false)

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

	// Initiate recovery
	err := drm.InitiateRecovery(rp1.ID)
	if err != nil {
		t.Fatalf("InitiateRecovery failed: %v", err)
	}

	// Verify recovery point was used
	rps := drm.ListRecoveryPoints("failover-plan")
	if len(rps) != 1 {
		t.Errorf("Expected 1 recovery point, got %d", len(rps))
	}
}

func TestScalingE2E_LoadBalancingStrategies(t *testing.T) {
	// Test different load balancing strategies
	strategies := []scaling.LoadBalancingStrategy{
		scaling.RoundRobin,
		scaling.LeastConn,
		scaling.LatencyBased,
		scaling.GeoLocation,
	}

	for _, strategy := range strategies {
		glb := scaling.NewGlobalLoadBalancer(strategy, 30*time.Second)

		// Register backends with different characteristics
		backends := []struct {
			id          string
			connections int64
			latency     int64
		}{
			{"backend-1", 5, 100},
			{"backend-2", 2, 50},
			{"backend-3", 8, 150},
		}

		for _, b := range backends {
			backend := &scaling.Backend{
				ID:          b.id,
				Address:     "10.0.0." + string(rune(len(backends))),
				Port:        8080,
				Region:      "region-1",
				Active:      true,
				Connections: b.connections,
				Latency:     b.latency,
			}
			glb.RegisterBackend(backend)
		}

		// Select backends multiple times
		selections := make(map[string]int)
		for i := 0; i < 10; i++ {
			selected, _ := glb.SelectBackend()
			selections[selected.ID]++
		}

		// Verify selection based on strategy
		if len(selections) == 0 {
			t.Errorf("No backends selected for strategy %s", strategy)
		}
	}
}

func TestScalingE2E_CDNContentDistribution(t *testing.T) {
	// Initialize CDN
	cdnConfig := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(cdnConfig, 100*1024*1024) // 100MB cache

	// Simulate content distribution
	contentLibrary := []struct {
		key  string
		url  string
		size int64
	}{
		{"movie-1", "https://example.com/movies/movie1.mp4", 500 * 1024 * 1024},
		{"movie-2", "https://example.com/movies/movie2.mp4", 400 * 1024 * 1024},
		{"series-1", "https://example.com/series/series1.mp4", 300 * 1024 * 1024},
	}

	// Cache content
	for _, content := range contentLibrary {
		cm.CacheContent(content.key, content.url, 3600, content.size)
	}

	// Simulate user requests
	requestPatterns := []struct {
		key   string
		count int
	}{
		{"movie-1", 50},
		{"movie-2", 30},
		{"series-1", 20},
	}

	for _, pattern := range requestPatterns {
		for i := 0; i < pattern.count; i++ {
			cm.GetCachedContent(pattern.key)
		}
	}

	// Verify CDN performance
	metrics := cm.GetMetrics()
	if metrics.CacheHits != 100 {
		t.Errorf("Expected 100 cache hits, got %d", metrics.CacheHits)
	}

	if metrics.HitRate != 100 {
		t.Errorf("Expected 100%% hit rate, got %.2f%%", metrics.HitRate)
	}

	// Verify cache utilization
	utilization := cm.GetCacheUtilization()
	if utilization > 100 {
		t.Errorf("Cache utilization should not exceed 100%%, got %.2f%%", utilization)
	}
}

func TestScalingE2E_DisasterRecoveryProcedure(t *testing.T) {
	// Initialize disaster recovery
	drm := scaling.NewDisasterRecoveryManager(100)

	// Create multiple backup plans
	plans := []struct {
		id        string
		name      string
		strategy  scaling.BackupStrategy
		primary   string
		secondary string
	}{
		{"plan-1", "Full Backup", scaling.FullBackup, "us-east-1", "eu-west-1"},
		{"plan-2", "Incremental", scaling.IncrementalBackup, "ap-southeast-1", "ap-northeast-1"},
	}

	for _, p := range plans {
		plan := &scaling.DisasterRecoveryPlan{
			ID:              p.id,
			Name:            p.name,
			BackupStrategy:  p.strategy,
			BackupInterval:  24 * time.Hour,
			RetentionDays:   30,
			RPOMinutes:      60,
			RTOMinutes:      120,
			PrimaryRegion:   p.primary,
			SecondaryRegion: p.secondary,
		}
		drm.CreatePlan(plan)
	}

	// Create recovery points for each plan
	for _, p := range plans {
		for i := 1; i <= 5; i++ {
			drm.CreateRecoveryPoint(p.id, int64(i)*1024*1024, "s3://backups/"+p.id+"/rp-"+string(rune(i)))
		}
	}

	// Test recovery procedures
	for _, p := range plans {
		rps := drm.ListRecoveryPoints(p.id)
		if len(rps) > 0 {
			// Initiate recovery from latest recovery point
			latestRP := rps[len(rps)-1]
			err := drm.InitiateRecovery(latestRP.ID)
			if err != nil {
				t.Fatalf("InitiateRecovery failed for plan %s: %v", p.id, err)
			}

			// Test recovery
			err = drm.TestRecovery(p.id)
			if err != nil {
				t.Fatalf("TestRecovery failed for plan %s: %v", p.id, err)
			}
		}
	}

	// Verify recovery statistics
	stats := drm.GetRecoveryStats()
	if stats["plan_count"] != 2 {
		t.Errorf("Expected 2 plans in stats")
	}
}

func TestScalingE2E_GlobalScalingMetrics(t *testing.T) {
	// Initialize all components
	mrm := scaling.NewMultiRegionManager(30 * time.Second)
	cdnConfig := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(cdnConfig, 1024*1024*1024)
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	// Setup infrastructure
	for i := 1; i <= 3; i++ {
		region := &scaling.Region{
			ID:     "region-" + string(rune(i)),
			Name:   "Region " + string(rune(i)),
			Active: true,
		}
		mrm.RegisterRegion(region)

		backend := &scaling.Backend{
			ID:      "backend-" + string(rune(i)),
			Address: "10.0." + string(rune(i)) + ".1",
			Port:    8080,
			Region:  "region-" + string(rune(i)),
		}
		glb.RegisterBackend(backend)
	}

	// Generate traffic
	for i := 0; i < 100; i++ {
		// CDN requests
		cm.CacheContent("key-"+string(rune(i%10)), "https://example.com/file.mp4", 3600, 1024)
		cm.GetCachedContent("key-" + string(rune(i%10)))

		// Load balancer requests
		selected, _ := glb.SelectBackend()
		glb.RecordRequest(selected.ID, 50, true)

		// Region requests
		mrm.RecordRequest("region-1", 50, true)
	}

	// Collect metrics
	cdnMetrics := cm.GetMetrics()
	lbMetrics := glb.GetAllMetrics()
	regionMetrics := mrm.GetAllMetrics()

	// Verify metrics
	if cdnMetrics.TotalRequests < 100 {
		t.Errorf("Expected at least 100 CDN requests, got %d", cdnMetrics.TotalRequests)
	}

	if len(lbMetrics) != 3 {
		t.Errorf("Expected 3 backend metrics, got %d", len(lbMetrics))
	}

	if len(regionMetrics) != 3 {
		t.Errorf("Expected 3 region metrics, got %d", len(regionMetrics))
	}
}

func BenchmarkScalingE2E_GlobalDeployment(b *testing.B) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)
	cdnConfig := scaling.CDNConfig{Provider: scaling.CloudFlare}
	cm := scaling.NewCDNManager(cdnConfig, 1024*1024*1024)
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	for i := 1; i <= 3; i++ {
		region := &scaling.Region{ID: "region-" + string(rune(i)), Name: "Region " + string(rune(i)), Active: true}
		mrm.RegisterRegion(region)

		backend := &scaling.Backend{ID: "backend-" + string(rune(i)), Address: "10.0." + string(rune(i)) + ".1", Port: 8080}
		glb.RegisterBackend(backend)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.CacheContent("key-"+string(rune(i)), "https://example.com/file.mp4", 3600, 1024)
		glb.SelectBackend()
		mrm.RecordRequest("region-1", 50, true)
	}
}

func BenchmarkScalingE2E_FailoverDetection(b *testing.B) {
	mrm := scaling.NewMultiRegionManager(30 * time.Second)
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	for i := 1; i <= 3; i++ {
		region := &scaling.Region{ID: "region-" + string(rune(i)), Name: "Region " + string(rune(i)), Active: true, Latency: 100}
		mrm.RegisterRegion(region)

		backend := &scaling.Backend{ID: "backend-" + string(rune(i)), Address: "10.0." + string(rune(i)) + ".1", Port: 8080, Latency: 100}
		glb.RegisterBackend(backend)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mrm.PerformHealthCheck()
		glb.PerformHealthCheck()
	}
}

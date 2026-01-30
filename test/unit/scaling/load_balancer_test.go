package scaling_test

import (
	"strconv"
	"testing"
	"time"

	"streamgate/pkg/scaling"
)

func TestGlobalLoadBalancer_RegisterBackend(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	backend := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "us-east-1",
	}

	err := glb.RegisterBackend(backend)
	if err != nil {
		t.Fatalf("RegisterBackend failed: %v", err)
	}

	if glb.GetBackendCount() != 1 {
		t.Errorf("Expected 1 backend, got %d", glb.GetBackendCount())
	}
}

func TestGlobalLoadBalancer_GetBackend(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	backend := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "us-east-1",
	}

	glb.RegisterBackend(backend)

	retrieved, err := glb.GetBackend("backend-1")
	if err != nil {
		t.Fatalf("GetBackend failed: %v", err)
	}

	if retrieved.ID != "backend-1" {
		t.Errorf("Backend ID doesn't match")
	}
}

func TestGlobalLoadBalancer_SelectBackend_RoundRobin(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	for i := 1; i <= 3; i++ {
		backend := &scaling.Backend{
			ID:      "backend-" + strconv.Itoa(i),
			Address: "192.168.1." + strconv.Itoa(i),
			Port:    8080,
			Region:  "us-east-1",
		}
		glb.RegisterBackend(backend)
	}

	// Select backends in round-robin order
	selections := make([]*scaling.Backend, 6)
	for i := 0; i < 6; i++ {
		selections[i], _ = glb.SelectBackend()
	}

	// Check that consecutive selections are different
	for i := 0; i < len(selections)-1; i++ {
		if selections[i].ID == selections[i+1].ID {
			t.Error("Round-robin selection not working correctly - consecutive selections should be different")
		}
	}

	// Check that after cycling through all backends, we wrap around
	// Since we have 3 backends, selections[3] should equal selections[0]
	if selections[3].ID != selections[0].ID {
		t.Error("Round-robin should wrap around after cycling through all backends")
	}

	// selections[4] should equal selections[1]
	if selections[4].ID != selections[1].ID {
		t.Error("Round-robin should maintain consistent order after wrap-around")
	}

	// selections[5] should equal selections[2]
	if selections[5].ID != selections[2].ID {
		t.Error("Round-robin should maintain consistent order after wrap-around")
	}
}

func TestGlobalLoadBalancer_SelectBackend_LeastConn(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.LeastConn, 30*time.Second)

	backend1 := &scaling.Backend{
		ID:          "backend-1",
		Address:     "192.168.1.1",
		Port:        8080,
		Region:      "us-east-1",
		Connections: 5,
	}
	backend2 := &scaling.Backend{
		ID:          "backend-2",
		Address:     "192.168.1.2",
		Port:        8080,
		Region:      "us-east-1",
		Connections: 2,
	}

	glb.RegisterBackend(backend1)
	glb.RegisterBackend(backend2)

	selected, _ := glb.SelectBackend()
	if selected.ID != "backend-2" {
		t.Error("Should select backend with least connections")
	}
}

func TestGlobalLoadBalancer_SelectBackend_LatencyBased(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.LatencyBased, 30*time.Second)

	backend1 := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "us-east-1",
		Latency: 100,
	}
	backend2 := &scaling.Backend{
		ID:      "backend-2",
		Address: "192.168.1.2",
		Port:    8080,
		Region:  "us-east-1",
		Latency: 50,
	}

	glb.RegisterBackend(backend1)
	glb.RegisterBackend(backend2)

	selected, _ := glb.SelectBackend()
	if selected.ID != "backend-2" {
		t.Error("Should select backend with lowest latency")
	}
}

func TestGlobalLoadBalancer_RecordRequest(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	backend := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "us-east-1",
	}

	glb.RegisterBackend(backend)

	err := glb.RecordRequest("backend-1", 50, true)
	if err != nil {
		t.Fatalf("RecordRequest failed: %v", err)
	}

	metrics, _ := glb.GetBackendMetrics("backend-1")
	if metrics.RequestCount != 1 {
		t.Errorf("Expected 1 request, got %d", metrics.RequestCount)
	}
}

func TestGlobalLoadBalancer_IncrementDecrementConnections(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	backend := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "us-east-1",
	}

	glb.RegisterBackend(backend)

	glb.IncrementConnections("backend-1")
	glb.IncrementConnections("backend-1")

	retrieved, _ := glb.GetBackend("backend-1")
	if retrieved.Connections != 2 {
		t.Errorf("Expected 2 connections, got %d", retrieved.Connections)
	}

	glb.DecrementConnections("backend-1")

	retrieved, _ = glb.GetBackend("backend-1")
	if retrieved.Connections != 1 {
		t.Errorf("Expected 1 connection, got %d", retrieved.Connections)
	}
}

func TestGlobalLoadBalancer_ActivateDeactivateBackend(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	backend := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "us-east-1",
	}

	glb.RegisterBackend(backend)

	err := glb.DeactivateBackend("backend-1")
	if err != nil {
		t.Fatalf("DeactivateBackend failed: %v", err)
	}

	if glb.GetActiveBackendCount() != 0 {
		t.Errorf("Expected 0 active backends, got %d", glb.GetActiveBackendCount())
	}

	err = glb.ActivateBackend("backend-1")
	if err != nil {
		t.Fatalf("ActivateBackend failed: %v", err)
	}

	if glb.GetActiveBackendCount() != 1 {
		t.Errorf("Expected 1 active backend, got %d", glb.GetActiveBackendCount())
	}
}

func TestGlobalLoadBalancer_GetBackendMetrics(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	backend := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "us-east-1",
	}

	glb.RegisterBackend(backend)

	metrics, err := glb.GetBackendMetrics("backend-1")
	if err != nil {
		t.Fatalf("GetBackendMetrics failed: %v", err)
	}

	if metrics.BackendID != "backend-1" {
		t.Errorf("Metrics backend ID doesn't match")
	}
}

func TestGlobalLoadBalancer_PerformHealthCheck(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	backend := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "us-east-1",
		Latency: 100,
	}

	glb.RegisterBackend(backend)

	err := glb.PerformHealthCheck()
	if err != nil {
		t.Fatalf("PerformHealthCheck failed: %v", err)
	}

	metrics, _ := glb.GetBackendMetrics("backend-1")
	if metrics.HealthStatus != "HEALTHY" {
		t.Errorf("Expected HEALTHY status, got %s", metrics.HealthStatus)
	}
}

func TestGlobalLoadBalancer_ShouldHealthCheck(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 1*time.Millisecond)

	if glb.ShouldHealthCheck() {
		t.Error("Should not health check immediately")
	}

	time.Sleep(2 * time.Millisecond)

	if !glb.ShouldHealthCheck() {
		t.Error("Should health check after interval")
	}
}

func TestGlobalLoadBalancer_ListBackends(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	for i := 1; i <= 3; i++ {
		backend := &scaling.Backend{
			ID:      "backend-" + string(rune(i)),
			Address: "192.168.1." + string(rune(i)),
			Port:    8080,
			Region:  "us-east-1",
		}
		glb.RegisterBackend(backend)
	}

	backends := glb.ListBackends()
	if len(backends) != 3 {
		t.Errorf("Expected 3 backends, got %d", len(backends))
	}
}

func TestGlobalLoadBalancer_SetStrategy(t *testing.T) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	glb.SetStrategy(scaling.LeastConn)

	if glb.GetStrategy() != scaling.LeastConn {
		t.Error("Strategy not updated")
	}
}

func BenchmarkGlobalLoadBalancer_SelectBackend(b *testing.B) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	for i := 1; i <= 10; i++ {
		backend := &scaling.Backend{
			ID:      "backend-" + string(rune(i)),
			Address: "192.168.1." + string(rune(i)),
			Port:    8080,
			Region:  "us-east-1",
		}
		glb.RegisterBackend(backend)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		glb.SelectBackend()
	}
}

func BenchmarkGlobalLoadBalancer_RecordRequest(b *testing.B) {
	glb := scaling.NewGlobalLoadBalancer(scaling.RoundRobin, 30*time.Second)

	backend := &scaling.Backend{
		ID:      "backend-1",
		Address: "192.168.1.1",
		Port:    8080,
		Region:  "us-east-1",
	}
	glb.RegisterBackend(backend)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		glb.RecordRequest("backend-1", 50, true)
	}
}

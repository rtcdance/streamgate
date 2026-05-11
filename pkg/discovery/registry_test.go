package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestRegistry(t *testing.T) *MemoryRegistry {
	t.Helper()
	return NewMemoryRegistry(zap.NewNop())
}

func TestMemoryRegistry_Register(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := ServiceInfo{
		ID:      "svc-1",
		Name:    "api-gateway",
		Address: "localhost",
		Port:    8080,
	}

	err := r.Register(ctx, svc)
	require.NoError(t, err)

	services, err := r.Discover(ctx, "api-gateway")
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "svc-1", services[0].ID)
	assert.Equal(t, StatusHealthy, services[0].Status)
	cancel()
}

func TestMemoryRegistry_RegisterAutoID(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := ServiceInfo{Name: "transcoder", Address: "localhost", Port: 9090}
	err := r.Register(ctx, svc)
	require.NoError(t, err)

	services, err := r.Discover(ctx, "transcoder")
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Contains(t, services[0].ID, "transcoder-")
	cancel()
}

func TestMemoryRegistry_Deregister(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = r.Register(ctx, ServiceInfo{ID: "s1", Name: "auth", Address: "localhost", Port: 8081})
	_ = r.Register(ctx, ServiceInfo{ID: "s2", Name: "auth", Address: "localhost", Port: 8082})

	err := r.Deregister(ctx, "s1")
	require.NoError(t, err)

	services, err := r.Discover(ctx, "auth")
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "s2", services[0].ID)
}

func TestMemoryRegistry_DeregisterNotFound(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	err := r.Deregister(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMemoryRegistry_DiscoverNotFound(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	_, err := r.Discover(ctx, "missing")
	assert.Error(t, err)
}

func TestMemoryRegistry_DiscoverFiltersUnhealthy(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	svc1 := ServiceInfo{ID: "h1", Name: "svc", Address: "a1", Port: 1, Status: StatusHealthy}
	svc2 := ServiceInfo{ID: "h2", Name: "svc", Address: "a2", Port: 2, Status: StatusUnhealthy}

	r.mu.Lock()
	r.services["svc"] = []ServiceInfo{svc1, svc2}
	r.mu.Unlock()

	services, err := r.Discover(ctx, "svc")
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "h1", services[0].ID)
}

func TestMemoryRegistry_HealthCheck(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Empty registry should fail health check
	err := r.HealthCheck(ctx)
	assert.Error(t, err)

	// Register a service
	_ = r.Register(ctx, ServiceInfo{ID: "h1", Name: "svc", Address: "a", Port: 1})
	err = r.HealthCheck(ctx)
	assert.NoError(t, err)
	cancel()
}

func TestMemoryRegistry_Watch(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = r.Register(ctx, ServiceInfo{ID: "w1", Name: "watched", Address: "a", Port: 1})

	ch, err := r.Watch(ctx, "watched")
	require.NoError(t, err)

	// Should receive initial state
	select {
	case services := <-ch:
		require.Len(t, services, 1)
		assert.Equal(t, "w1", services[0].ID)
	case <-time.After(time.Second):
		t.Fatal("watch did not receive initial state")
	}

	// Cancel context should clean up watcher
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestLoadBalancer_RoundRobin(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = r.Register(ctx, ServiceInfo{ID: "lb1", Name: "lb-svc", Address: "a1", Port: 1})
	_ = r.Register(ctx, ServiceInfo{ID: "lb2", Name: "lb-svc", Address: "a2", Port: 2})
	_ = r.Register(ctx, ServiceInfo{ID: "lb3", Name: "lb-svc", Address: "a3", Port: 3})

	lb := NewLoadBalancer(r, RoundRobin, zap.NewNop())

	// Round-robin should cycle through instances
	ids := make(map[string]int)
	for i := 0; i < 6; i++ {
		svc, err := lb.GetInstance(ctx, "lb-svc")
		require.NoError(t, err)
		ids[svc.ID]++
	}

	// Each instance should have been selected approximately 2 times
	for _, count := range ids {
		assert.Equal(t, 2, count)
	}
}

func TestLoadBalancer_NoInstances(t *testing.T) {
	r := newTestRegistry(t)
	lb := NewLoadBalancer(r, RoundRobin, zap.NewNop())

	_, err := lb.GetInstance(context.Background(), "missing")
	assert.Error(t, err)
}

func TestServiceDiscovery(t *testing.T) {
	r := newTestRegistry(t)
	sd := NewServiceDiscovery(r, RoundRobin, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = r.Register(ctx, ServiceInfo{ID: "sd1", Name: "api-gateway", Address: "10.0.0.1", Port: 8080})
	_ = r.Register(ctx, ServiceInfo{ID: "sd2", Name: "transcoder", Address: "10.0.0.2", Port: 9090})

	svc, err := sd.GetService(ctx, "api-gateway")
	require.NoError(t, err)
	assert.Equal(t, "sd1", svc.ID)

	all, err := sd.GetAllServices(ctx, "api-gateway")
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestServiceStatus_String(t *testing.T) {
	tests := []struct {
		status ServiceStatus
		want   string
	}{
		{StatusUnknown, "unknown"},
		{StatusHealthy, "healthy"},
		{StatusUnhealthy, "unhealthy"},
		{StatusDraining, "draining"},
		{ServiceStatus(99), "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.status.String())
	}
}

package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type testMockRegistry struct {
	mu         sync.RWMutex
	services   map[string][]*ServiceInfo
	discoverFn func(ctx context.Context, serviceName string) ([]*ServiceInfo, error)
}

func newTestMockRegistry() *testMockRegistry {
	return &testMockRegistry{
		services: make(map[string][]*ServiceInfo),
	}
}

func (m *testMockRegistry) Register(_ context.Context, svc *ServiceInfo) error {
	m.mu.Lock()
	m.services[svc.Name] = append(m.services[svc.Name], svc)
	m.mu.Unlock()
	return nil
}

func (m *testMockRegistry) Deregister(_ context.Context, serviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, svcs := range m.services {
		for i, s := range svcs {
			if s.ID == serviceID {
				m.services[name] = append(svcs[:i], svcs[i+1:]...)
				return nil
			}
		}
	}
	return fmt.Errorf("service not found: %s", serviceID)
}

func (m *testMockRegistry) Discover(ctx context.Context, serviceName string) ([]*ServiceInfo, error) {
	if m.discoverFn != nil {
		return m.discoverFn(ctx, serviceName)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	svcs := m.services[serviceName]
	result := make([]*ServiceInfo, len(svcs))
	copy(result, svcs)
	return result, nil
}

func (m *testMockRegistry) Watch(_ context.Context, _ string) (<-chan []*ServiceInfo, error) {
	ch := make(chan []*ServiceInfo)
	close(ch)
	return ch, nil
}

func (m *testMockRegistry) Health(_ context.Context) error {
	return nil
}

func TestSvcClient_GetServiceAddress_RoundRobinDistribution(t *testing.T) {
	reg := newTestMockRegistry()
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s1", Name: "svc", Address: "10.0.0.1", Port: 8080})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s2", Name: "svc", Address: "10.0.0.2", Port: 8080})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s3", Name: "svc", Address: "10.0.0.3", Port: 8080})

	client := NewServiceClient(reg, zap.NewNop())
	addrs := make(map[string]bool)
	for i := 0; i < 6; i++ {
		addr, err := client.GetServiceAddress(context.Background(), "svc")
		require.NoError(t, err)
		addrs[addr] = true
	}
	assert.Len(t, addrs, 3, "round-robin should distribute across all services")
}

func TestSvcClient_GetServiceAddress_NoInstancesFound(t *testing.T) {
	reg := newTestMockRegistry()
	client := NewServiceClient(reg, zap.NewNop())

	_, err := client.GetServiceAddress(context.Background(), ServiceUpload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")
}

func TestSvcClient_GetServiceAddress_RegistryError(t *testing.T) {
	reg := newTestMockRegistry()
	reg.discoverFn = func(_ context.Context, _ string) ([]*ServiceInfo, error) {
		return nil, errors.New("registry unavailable")
	}
	client := NewServiceClient(reg, zap.NewNop())

	_, err := client.GetServiceAddress(context.Background(), ServiceUpload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to discover service")
}

func TestSvcClient_GetAllServiceAddresses_MultipleInstances(t *testing.T) {
	reg := newTestMockRegistry()
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s1", Name: "svc", Address: "10.0.0.1", Port: 8080})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s2", Name: "svc", Address: "10.0.0.2", Port: 8081})

	client := NewServiceClient(reg, zap.NewNop())
	addrs, err := client.GetAllServiceAddresses(context.Background(), "svc")
	require.NoError(t, err)
	assert.Len(t, addrs, 2)
	assert.Contains(t, addrs, "10.0.0.1:8080")
	assert.Contains(t, addrs, "10.0.0.2:8081")
}

func TestSvcClient_GetAllServiceAddresses_RegistryDown(t *testing.T) {
	reg := newTestMockRegistry()
	reg.discoverFn = func(_ context.Context, _ string) ([]*ServiceInfo, error) {
		return nil, errors.New("registry down")
	}
	client := NewServiceClient(reg, zap.NewNop())

	_, err := client.GetAllServiceAddresses(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to discover services")
}

func TestSvcClient_GetAllServiceAddresses_NoInstances(t *testing.T) {
	reg := newTestMockRegistry()
	client := NewServiceClient(reg, zap.NewNop())

	addrs, err := client.GetAllServiceAddresses(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, addrs)
}

func TestGetServicePort_AllKnownServices(t *testing.T) {
	tests := []struct {
		name     string
		expected int
	}{
		{ServiceAPIGateway, 9090},
		{ServiceUpload, 9091},
		{ServiceStreaming, 9093},
		{ServiceMetadata, 9005},
		{ServiceAuth, 9007},
		{ServiceCache, 9006},
		{ServiceTranscoder, 9092},
		{ServiceWorker, 9008},
		{ServiceMonitor, 9009},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, GetServicePort(tt.name))
		})
	}
}

func TestGetServicePort_UnknownService(t *testing.T) {
	assert.Equal(t, 8080, GetServicePort("unknown-service"))
	assert.Equal(t, 8080, GetServicePort(""))
}

func TestCB_NegativeTimeout(t *testing.T) {
	cb := NewCircuitBreaker(3, -5, zap.NewNop())
	assert.Equal(t, 30*time.Second, cb.timeout)
	assert.Equal(t, "closed", cb.GetState())
}

func TestCB_HalfOpenToClosed_ViaTimer(t *testing.T) {
	cb := NewCircuitBreaker(1, 1, zap.NewNop())

	err := cb.Call(func() error { return errors.New("fail") })
	require.Error(t, err)
	assert.Equal(t, "open", cb.GetState())

	time.Sleep(1500 * time.Millisecond)
	assert.Equal(t, "half-open", cb.GetState())

	err = cb.Call(func() error { return nil })
	require.NoError(t, err)
	assert.Equal(t, "closed", cb.GetState())
}

func TestCB_HalfOpenToOpen_ViaTimer(t *testing.T) {
	cb := NewCircuitBreaker(1, 1, zap.NewNop())

	err := cb.Call(func() error { return errors.New("fail") })
	require.Error(t, err)

	time.Sleep(1500 * time.Millisecond)
	assert.Equal(t, "half-open", cb.GetState())

	err = cb.Call(func() error { return errors.New("still failing") })
	require.Error(t, err)
	assert.Equal(t, "open", cb.GetState())
}

func TestCB_HalfOpenConcurrentTrial(t *testing.T) {
	cb := NewCircuitBreaker(1, 1, zap.NewNop())

	err := cb.Call(func() error { return errors.New("fail") })
	require.Error(t, err)

	time.Sleep(1500 * time.Millisecond)
	assert.Equal(t, "half-open", cb.GetState())

	var rejectedCount atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := cb.Call(func() error {
				time.Sleep(100 * time.Millisecond)
				return nil
			})
			if err != nil && err.Error() == "circuit breaker is half-open (trial in progress)" {
				rejectedCount.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, "closed", cb.GetState())
	assert.True(t, rejectedCount.Load() > 0, "some callers should be rejected during trial")
}

func TestCB_ResetFailuresOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker(5, 1, zap.NewNop())

	for i := 0; i < 3; i++ {
		_ = cb.Call(func() error { return errors.New("fail") })
	}
	assert.Equal(t, 3, cb.failures)

	err := cb.Call(func() error { return nil })
	require.NoError(t, err)
	assert.Equal(t, 0, cb.failures)
	assert.Equal(t, "closed", cb.GetState())
}

func TestCB_ExactlyMaxFailuresOpens(t *testing.T) {
	cb := NewCircuitBreaker(3, 1, zap.NewNop())

	for i := 0; i < 2; i++ {
		_ = cb.Call(func() error { return errors.New("fail") })
	}
	assert.Equal(t, "closed", cb.GetState())

	_ = cb.Call(func() error { return errors.New("fail") })
	assert.Equal(t, "open", cb.GetState())
}

func TestSvcLocator_AllServiceMethods(t *testing.T) {
	reg := newTestMockRegistry()
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "u1", Name: ServiceUpload, Address: "10.0.0.1", Port: 9091})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s1", Name: ServiceStreaming, Address: "10.0.0.2", Port: 9093})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "m1", Name: ServiceMetadata, Address: "10.0.0.3", Port: 9005})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "a1", Name: ServiceAuth, Address: "10.0.0.4", Port: 9007})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "c1", Name: ServiceCache, Address: "10.0.0.5", Port: 9006})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "t1", Name: ServiceTranscoder, Address: "10.0.0.6", Port: 9092})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "w1", Name: ServiceWorker, Address: "10.0.0.7", Port: 9008})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "mo1", Name: ServiceMonitor, Address: "10.0.0.8", Port: 9009})

	loc := NewServiceLocator(reg, zap.NewNop())

	tests := []struct {
		method   string
		getAddr  func() (string, error)
		expected string
	}{
		{"Upload", func() (string, error) { return loc.GetUploadService(context.Background()) }, "10.0.0.1:9091"},
		{"Streaming", func() (string, error) { return loc.GetStreamingService(context.Background()) }, "10.0.0.2:9093"},
		{"Metadata", func() (string, error) { return loc.GetMetadataService(context.Background()) }, "10.0.0.3:9005"},
		{"Auth", func() (string, error) { return loc.GetAuthService(context.Background()) }, "10.0.0.4:9007"},
		{"Cache", func() (string, error) { return loc.GetCacheService(context.Background()) }, "10.0.0.5:9006"},
		{"Transcoder", func() (string, error) { return loc.GetTranscoderService(context.Background()) }, "10.0.0.6:9092"},
		{"Worker", func() (string, error) { return loc.GetWorkerService(context.Background()) }, "10.0.0.7:9008"},
		{"Monitor", func() (string, error) { return loc.GetMonitorService(context.Background()) }, "10.0.0.8:9009"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			addr, err := tt.getAddr()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, addr)
		})
	}
}

func TestSvcLocator_RegistryUnavailable(t *testing.T) {
	reg := newTestMockRegistry()
	reg.discoverFn = func(_ context.Context, _ string) ([]*ServiceInfo, error) {
		return nil, errors.New("registry unavailable")
	}
	loc := NewServiceLocator(reg, zap.NewNop())

	_, err := loc.GetUploadService(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to discover service")
}

func TestSvcLocator_RoundRobinDistribution(t *testing.T) {
	reg := newTestMockRegistry()
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s1", Name: ServiceUpload, Address: "10.0.0.1", Port: 9091})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s2", Name: ServiceUpload, Address: "10.0.0.2", Port: 9091})

	loc := NewServiceLocator(reg, zap.NewNop())
	addrs := make(map[string]bool)
	for i := 0; i < 4; i++ {
		addr, err := loc.GetUploadService(context.Background())
		require.NoError(t, err)
		addrs[addr] = true
	}
	assert.Len(t, addrs, 2, "round-robin should distribute across all instances")
}

func TestClientPool_NewWithTLS_NilConfig(t *testing.T) {
	pool := NewClientPoolWithTLS(nil, zap.NewNop(), nil)
	assert.NotNil(t, pool)
	assert.Nil(t, pool.tlsConfig)
}

func TestClientPool_Close_EmptyPool(t *testing.T) {
	pool := NewClientPool(nil, zap.NewNop())
	err := pool.Close()
	require.NoError(t, err)
}

func TestClientPool_GetConnection_RegistryError(t *testing.T) {
	reg := newTestMockRegistry()
	reg.discoverFn = func(_ context.Context, _ string) ([]*ServiceInfo, error) {
		return nil, errors.New("registry unavailable")
	}
	pool := NewClientPool(reg, zap.NewNop())

	_, err := pool.GetConnection(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get service address")
}

func TestClientPool_GetConnection_NoInstancesFound(t *testing.T) {
	reg := newTestMockRegistry()
	pool := NewClientPool(reg, zap.NewNop())

	_, err := pool.GetConnection(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")
}

func TestSvcInfo_StructFieldAccess(t *testing.T) {
	si := &ServiceInfo{
		ID:       "svc-1",
		Name:     "test-service",
		Address:  "10.0.0.1",
		Port:     8080,
		Tags:     []string{"v1", "prod"},
		Metadata: map[string]string{"version": "1.0"},
		Check: &HealthCheck{
			HTTP:     "http://10.0.0.1:8080/health",
			Interval: "10s",
			Timeout:  "5s",
		},
	}
	assert.Equal(t, "svc-1", si.ID)
	assert.Equal(t, "test-service", si.Name)
	assert.Equal(t, "10.0.0.1", si.Address)
	assert.Equal(t, 8080, si.Port)
	assert.Equal(t, []string{"v1", "prod"}, si.Tags)
	assert.Equal(t, "1.0", si.Metadata["version"])
	assert.NotNil(t, si.Check)
	assert.Equal(t, "http://10.0.0.1:8080/health", si.Check.HTTP)
	assert.Equal(t, "10s", si.Check.Interval)
	assert.Equal(t, "5s", si.Check.Timeout)
}

func TestSvcInfo_NilHealthCheck(t *testing.T) {
	si := &ServiceInfo{ID: "svc-1", Name: "test"}
	assert.Nil(t, si.Check)
}

func TestServiceNameConstants_AllValues(t *testing.T) {
	assert.Equal(t, "api-gateway", ServiceAPIGateway)
	assert.Equal(t, "upload", ServiceUpload)
	assert.Equal(t, "streaming", ServiceStreaming)
	assert.Equal(t, "metadata", ServiceMetadata)
	assert.Equal(t, "auth", ServiceAuth)
	assert.Equal(t, "cache", ServiceCache)
	assert.Equal(t, "transcoder", ServiceTranscoder)
	assert.Equal(t, "worker", ServiceWorker)
	assert.Equal(t, "monitor", ServiceMonitor)
}

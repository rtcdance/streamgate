package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type threadSafeMockRegistry struct {
	mu       sync.RWMutex
	services map[string][]*ServiceInfo
}

func newThreadSafeMockRegistry() *threadSafeMockRegistry {
	return &threadSafeMockRegistry{
		services: make(map[string][]*ServiceInfo),
	}
}

func (m *threadSafeMockRegistry) Register(_ context.Context, svc *ServiceInfo) error {
	m.mu.Lock()
	m.services[svc.Name] = append(m.services[svc.Name], svc)
	m.mu.Unlock()
	return nil
}

func (m *threadSafeMockRegistry) Deregister(_ context.Context, serviceID string) error {
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
	return errors.New("not found")
}

func (m *threadSafeMockRegistry) Discover(_ context.Context, serviceName string) ([]*ServiceInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	svcs := m.services[serviceName]
	result := make([]*ServiceInfo, len(svcs))
	copy(result, svcs)
	return result, nil
}

func (m *threadSafeMockRegistry) Watch(_ context.Context, _ string) (<-chan []*ServiceInfo, error) {
	ch := make(chan []*ServiceInfo)
	close(ch)
	return ch, nil
}

func (m *threadSafeMockRegistry) Health(_ context.Context) error {
	return nil
}

func TestServiceClient_GetServiceAddress_RoundRobin(t *testing.T) {
	reg := newThreadSafeMockRegistry()
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s1", Name: "svc", Address: "10.0.0.1", Port: 8080})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s2", Name: "svc", Address: "10.0.0.2", Port: 8080})

	client := NewServiceClient(reg, zap.NewNop())
	addr1, err := client.GetServiceAddress(context.Background(), "svc")
	require.NoError(t, err)
	addr2, err := client.GetServiceAddress(context.Background(), "svc")
	require.NoError(t, err)
	assert.NotEqual(t, addr1, addr2)
}

func TestServiceClient_GetServiceAddress_DiscoverError(t *testing.T) {
	reg := &errorDiscoverRegistry{}
	client := NewServiceClient(reg, zap.NewNop())
	_, err := client.GetServiceAddress(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to discover service")
}

func TestServiceClient_GetServiceAddress_NoInstances(t *testing.T) {
	reg := newThreadSafeMockRegistry()
	client := NewServiceClient(reg, zap.NewNop())
	_, err := client.GetServiceAddress(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")
}

func TestServiceClient_GetAllServiceAddresses_Multiple(t *testing.T) {
	reg := newThreadSafeMockRegistry()
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s1", Name: "svc", Address: "10.0.0.1", Port: 8080})
	_ = reg.Register(context.Background(), &ServiceInfo{ID: "s2", Name: "svc", Address: "10.0.0.2", Port: 8081})

	client := NewServiceClient(reg, zap.NewNop())
	addrs, err := client.GetAllServiceAddresses(context.Background(), "svc")
	require.NoError(t, err)
	assert.Len(t, addrs, 2)
}

func TestServiceClient_GetAllServiceAddresses_DiscoverError(t *testing.T) {
	reg := &errorDiscoverRegistry{}
	client := NewServiceClient(reg, zap.NewNop())
	_, err := client.GetAllServiceAddresses(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to discover services")
}

type errorDiscoverRegistry struct{}

func (e *errorDiscoverRegistry) Register(_ context.Context, _ *ServiceInfo) error { return nil }
func (e *errorDiscoverRegistry) Deregister(_ context.Context, _ string) error     { return nil }
func (e *errorDiscoverRegistry) Discover(_ context.Context, _ string) ([]*ServiceInfo, error) {
	return nil, errors.New("registry unavailable")
}
func (e *errorDiscoverRegistry) Watch(_ context.Context, _ string) (<-chan []*ServiceInfo, error) {
	return nil, nil
}
func (e *errorDiscoverRegistry) Health(_ context.Context) error { return nil }

func TestClientPool_GetServiceAddress_NoInstances(t *testing.T) {
	reg := newThreadSafeMockRegistry()
	pool := NewClientPool(reg, zap.NewNop())
	_, err := pool.GetConnection(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")
}

func TestClientPool_GetServiceAddress_DiscoverError(t *testing.T) {
	reg := &errorDiscoverRegistry{}
	pool := NewClientPool(reg, zap.NewNop())
	_, err := pool.GetConnection(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get service address")
}

func TestCircuitBreaker_OpenRejects(t *testing.T) {
	cb := NewCircuitBreaker(1, 1, zap.NewNop())
	err := cb.Call(func() error { return errors.New("fail") })
	require.Error(t, err)
	assert.Equal(t, "open", cb.GetState())

	err = cb.Call(func() error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestCircuitBreaker_HalfOpenSuccess(t *testing.T) {
	cb := NewCircuitBreaker(1, 0, zap.NewNop())
	_ = cb.Call(func() error { return errors.New("fail") })

	cb.mu.Lock()
	cb.state = "half-open"
	cb.mu.Unlock()

	err := cb.Call(func() error { return nil })
	require.NoError(t, err)
	assert.Equal(t, "closed", cb.GetState())
	assert.Equal(t, 0, cb.failures)
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(1, 0, zap.NewNop())
	_ = cb.Call(func() error { return errors.New("fail") })

	cb.mu.Lock()
	cb.state = "half-open"
	cb.mu.Unlock()

	err := cb.Call(func() error { return errors.New("still failing") })
	require.Error(t, err)
	assert.Equal(t, "open", cb.GetState())
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cb := NewCircuitBreaker(5, 10, zap.NewNop())
	_ = cb.Call(func() error { return errors.New("fail") })
	_ = cb.Call(func() error { return errors.New("fail") })
	assert.Equal(t, 2, cb.failures)

	err := cb.Call(func() error { return nil })
	require.NoError(t, err)
	assert.Equal(t, 0, cb.failures)
	assert.Equal(t, "closed", cb.GetState())
}

func TestServiceLocator_NoInstances(t *testing.T) {
	reg := newThreadSafeMockRegistry()
	loc := NewServiceLocator(reg, zap.NewNop())
	_, err := loc.GetUploadService(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")
}

func TestServiceLocator_DiscoverError(t *testing.T) {
	reg := &errorDiscoverRegistry{}
	loc := NewServiceLocator(reg, zap.NewNop())
	_, err := loc.GetUploadService(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to discover service")
}

func TestGetServicePort_All(t *testing.T) {
	tests := []struct {
		name string
		port int
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
		assert.Equal(t, tt.port, GetServicePort(tt.name))
	}
	assert.Equal(t, 8080, GetServicePort("unknown"))
}

func TestNewServiceClient(t *testing.T) {
	client := NewServiceClient(nil, zap.NewNop())
	assert.NotNil(t, client)
}

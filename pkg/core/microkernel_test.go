package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/core/config"
	"streamgate/pkg/core/event"
	"streamgate/pkg/service"
)

type mockPlugin struct {
	name        string
	version     string
	initialized bool
	started     bool
}

func (m *mockPlugin) Name() string {
	return m.name
}

func (m *mockPlugin) Version() string {
	return m.version
}

func (m *mockPlugin) Init(ctx context.Context, kernel *Microkernel) error {
	m.initialized = true
	return nil
}

func (m *mockPlugin) Start(ctx context.Context) error {
	m.started = true
	return nil
}

func (m *mockPlugin) Stop(ctx context.Context) error {
	m.started = false
	return nil
}

func (m *mockPlugin) Health(ctx context.Context) error {
	return nil
}

type mockEventBus struct {
	published []event.Event
}

func (m *mockEventBus) Publish(ctx context.Context, e event.Event) error {
	m.published = append(m.published, e)
	return nil
}

func (m *mockEventBus) Subscribe(ctx context.Context, topic string, handler event.EventHandler) error {
	return nil
}

func (m *mockEventBus) Unsubscribe(ctx context.Context, topic string, handler event.EventHandler) error {
	return nil
}

type mockRegistry struct{}

func (m *mockRegistry) Register(ctx context.Context, info *service.ServiceInfo) error {
	return nil
}

func (m *mockRegistry) Deregister(ctx context.Context, serviceID string) error {
	return nil
}

func (m *mockRegistry) Discover(ctx context.Context, serviceName string) ([]*service.ServiceInfo, error) {
	return nil, nil
}

func (m *mockRegistry) Health(ctx context.Context, serviceID string) error {
	return nil
}

type mockClientPool struct{}

func (m *mockClientPool) GetClient(serviceName string) (interface{}, error) {
	return nil, nil
}

func (m *mockClientPool) Close() error {
	return nil
}

func TestNewMicrokernel(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Mode: "monolithic",
	}

	kernel, err := NewMicrokernel(cfg, logger)
	assert.NoError(t, err)
	assert.NotNil(t, kernel)
	assert.NotNil(t, kernel.logger)
	assert.NotNil(t, kernel.eventBus)
	assert.NotNil(t, kernel.plugins)
	assert.False(t, kernel.started)
}

func TestNewMicrokernel_MicroserviceMode(t *testing.T) {
	t.Skip("Skipping microservice mode test - requires external services")

	logger := zap.NewNop()
	cfg := &config.Config{
		Mode: "microservice",
		NATS: config.NATSConfig{
			URL: "nats://localhost:4222",
		},
		Consul: config.ConsulConfig{
			Address: "localhost:8500",
		},
	}

	kernel, err := NewMicrokernel(cfg, logger)
	assert.NoError(t, err)
	assert.NotNil(t, kernel)
	assert.NotNil(t, kernel.registry)
	assert.NotNil(t, kernel.clientPool)
}

func TestMicrokernel_RegisterPlugin(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	plugin := &mockPlugin{name: "test-plugin", version: "1.0.0"}

	err = kernel.RegisterPlugin(plugin)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(kernel.plugins))

	retrievedPlugin, err := kernel.GetPlugin("test-plugin")
	assert.NoError(t, err)
	assert.Equal(t, plugin, retrievedPlugin)
}

func TestMicrokernel_RegisterPlugin_Duplicate(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	plugin := &mockPlugin{name: "test-plugin", version: "1.0.0"}

	err = kernel.RegisterPlugin(plugin)
	require.NoError(t, err)

	err = kernel.RegisterPlugin(plugin)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestMicrokernel_GetPlugin_NotFound(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	_, err = kernel.GetPlugin("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMicrokernel_GetEventBus(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	eventBus := kernel.GetEventBus()
	assert.NotNil(t, eventBus)
}

func TestMicrokernel_GetRegistry(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	registry := kernel.GetRegistry()
	assert.Nil(t, registry)
}

func TestMicrokernel_GetRegistry_MicroserviceMode(t *testing.T) {
	t.Skip("Skipping microservice mode test - requires external services")

	logger := zap.NewNop()
	cfg := &config.Config{
		Mode: "microservice",
		Consul: config.ConsulConfig{
			Address: "localhost:8500",
		},
	}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	registry := kernel.GetRegistry()
	assert.NotNil(t, registry)
}

func TestMicrokernel_GetClientPool(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	clientPool := kernel.GetClientPool()
	assert.Nil(t, clientPool)
}

func TestMicrokernel_GetClientPool_MicroserviceMode(t *testing.T) {
	t.Skip("Skipping microservice mode test - requires external services")

	logger := zap.NewNop()
	cfg := &config.Config{
		Mode: "microservice",
		Consul: config.ConsulConfig{
			Address: "localhost:8500",
		},
	}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	clientPool := kernel.GetClientPool()
	assert.NotNil(t, clientPool)
}

func TestMicrokernel_GetConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	retrievedConfig := kernel.GetConfig()
	assert.Equal(t, cfg, retrievedConfig)
}

func TestMicrokernel_GetLogger(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	retrievedLogger := kernel.GetLogger()
	assert.Equal(t, logger, retrievedLogger)
}

func TestMicrokernel_Start_AlreadyStarted(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = kernel.Start(ctx)
	require.NoError(t, err)

	err = kernel.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")
}

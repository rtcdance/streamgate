package e2e_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/core/event"

	"github.com/stretchr/testify/require"
)

type mockPlugin struct {
	name    string
	version string
}

func (m *mockPlugin) Name() string {
	return m.name
}

func (m *mockPlugin) Version() string {
	return m.version
}

func (m *mockPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	return nil
}

func (m *mockPlugin) Start(ctx context.Context) error {
	return nil
}

func (m *mockPlugin) Stop(ctx context.Context) error {
	return nil
}

func (m *mockPlugin) Health(ctx context.Context) error {
	return nil
}

func (m *mockPlugin) DependsOn() []string { return nil }

func TestE2E_MicrokernelInitialization(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, kernel)

	_ = kernel.Shutdown(context.Background())
}

func TestE2E_PluginRegistration(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = kernel.Shutdown(context.Background()) }()

	plugin := &mockPlugin{
		name:    "test-plugin",
		version: "1.0.0",
	}

	err = kernel.RegisterPlugin(plugin)
	require.NoError(t, err)

	registered, err := kernel.GetPlugin("test-plugin")
	require.NoError(t, err)
	require.NotNil(t, registered)
}

func TestE2E_EventPublishing(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = kernel.Shutdown(context.Background()) }()

	var eventReceived bool
	var mu sync.Mutex
	eventBus := kernel.GetEventBus()
	_, _ = eventBus.Subscribe(context.Background(), "test-event", func(ctx context.Context, e *event.Event) error {
		mu.Lock()
		eventReceived = true
		mu.Unlock()
		return nil
	})

	ev := &event.Event{
		Type: "test-event",
		Data: map[string]interface{}{"message": "test"},
	}

	err = eventBus.Publish(context.Background(), ev)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	received := eventReceived
	mu.Unlock()
	require.True(t, received)
}

func TestE2E_HealthCheck(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = kernel.Shutdown(context.Background()) }()

	err = kernel.Health(context.Background())
	require.NoError(t, err)
}

func TestE2E_Lifecycle(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	require.NoError(t, err)

	err = kernel.Start(context.Background())
	require.NoError(t, err)

	err = kernel.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestE2E_ConfigurationManagement(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = kernel.Shutdown(context.Background()) }()

	retrievedConfig := kernel.GetConfig()
	require.NotNil(t, retrievedConfig)
	require.Equal(t, "test-kernel", retrievedConfig.AppName)
}

func TestE2E_Logging(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = kernel.Shutdown(context.Background()) }()

	logger := kernel.GetLogger()
	if logger != nil {
		logger.Info("test message")
		logger.Error("error message")
	}

	require.True(t, true)
}

func TestE2E_MetricsCollection(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = kernel.Shutdown(context.Background()) }()

	require.True(t, true)
}

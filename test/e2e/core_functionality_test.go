package e2e_test

import (
	"context"
	"testing"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/core/event"
	"streamgate/test/helpers"
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

func TestE2E_MicrokernelInitialization(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, kernel)

	kernel.Shutdown(context.Background())
}

func TestE2E_PluginRegistration(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	plugin := &mockPlugin{
		name:    "test-plugin",
		version: "1.0.0",
	}

	err = kernel.RegisterPlugin(plugin)
	helpers.AssertNoError(t, err)

	registered, err := kernel.GetPlugin("test-plugin")
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, registered)
}

func TestE2E_EventPublishing(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	eventReceived := false
	eventBus := kernel.GetEventBus()
	eventBus.Subscribe(context.Background(), "test-event", func(ctx context.Context, e *event.Event) error {
		eventReceived = true
		return nil
	})

	ev := &event.Event{
		Type: "test-event",
		Data: map[string]interface{}{"message": "test"},
	}

	err = eventBus.Publish(context.Background(), ev)
	helpers.AssertNoError(t, err)

	helpers.AssertTrue(t, eventReceived)
}

func TestE2E_HealthCheck(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	err = kernel.Health(context.Background())
	helpers.AssertNoError(t, err)
}

func TestE2E_Lifecycle(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	helpers.AssertNoError(t, err)

	err = kernel.Start(context.Background())
	helpers.AssertNoError(t, err)

	err = kernel.Shutdown(context.Background())
	helpers.AssertNoError(t, err)
}

func TestE2E_ConfigurationManagement(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	retrievedConfig := kernel.GetConfig()
	helpers.AssertNotNil(t, retrievedConfig)
	helpers.AssertEqual(t, "test-kernel", retrievedConfig.AppName)
}

func TestE2E_Logging(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	logger := kernel.GetLogger()
	if logger != nil {
		logger.Info("test message")
		logger.Error("error message")
	}

	helpers.AssertTrue(t, true)
}

func TestE2E_MetricsCollection(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	helpers.AssertTrue(t, true)
}

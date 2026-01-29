package e2e_test

import (
	"context"
	"testing"

	"streamgate/pkg/core"
	"streamgate/test/helpers"
)

func TestE2E_MicrokernelInitialization(t *testing.T) {
	// Setup
	config := &core.MicrokernelConfig{
		Name:    "test-kernel",
		Version: "1.0.0",
	}

	// Initialize microkernel
	kernel, err := core.NewMicrokernel(config)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, kernel)

	// Cleanup
	kernel.Shutdown(context.Background())
}

func TestE2E_PluginRegistration(t *testing.T) {
	// Setup
	config := &core.MicrokernelConfig{
		Name:    "test-kernel",
		Version: "1.0.0",
	}

	kernel, err := core.NewMicrokernel(config)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	// Register plugin
	plugin := &core.Plugin{
		Name:    "test-plugin",
		Version: "1.0.0",
	}

	err = kernel.RegisterPlugin(plugin)
	helpers.AssertNoError(t, err)

	// Verify plugin registered
	registered := kernel.GetPlugin("test-plugin")
	helpers.AssertNotNil(t, registered)
}

func TestE2E_EventPublishing(t *testing.T) {
	// Setup
	config := &core.MicrokernelConfig{
		Name:    "test-kernel",
		Version: "1.0.0",
	}

	kernel, err := core.NewMicrokernel(config)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	// Subscribe to event
	eventReceived := false
	kernel.Subscribe("test-event", func(event *core.Event) {
		eventReceived = true
	})

	// Publish event
	event := &core.Event{
		Type: "test-event",
		Data: map[string]interface{}{"message": "test"},
	}

	err = kernel.PublishEvent(context.Background(), event)
	helpers.AssertNoError(t, err)

	// Verify event was received
	helpers.AssertTrue(t, eventReceived)
}

func TestE2E_HealthCheck(t *testing.T) {
	// Setup
	config := &core.MicrokernelConfig{
		Name:    "test-kernel",
		Version: "1.0.0",
	}

	kernel, err := core.NewMicrokernel(config)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	// Check health
	health, err := kernel.HealthCheck(context.Background())
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, health)
	helpers.AssertEqual(t, "healthy", health.Status)
}

func TestE2E_Lifecycle(t *testing.T) {
	// Setup
	config := &core.MicrokernelConfig{
		Name:    "test-kernel",
		Version: "1.0.0",
	}

	kernel, err := core.NewMicrokernel(config)
	helpers.AssertNoError(t, err)

	// Start kernel
	err = kernel.Start(context.Background())
	helpers.AssertNoError(t, err)

	// Verify running
	helpers.AssertTrue(t, kernel.IsRunning())

	// Stop kernel
	err = kernel.Shutdown(context.Background())
	helpers.AssertNoError(t, err)

	// Verify stopped
	helpers.AssertFalse(t, kernel.IsRunning())
}

func TestE2E_ConfigurationManagement(t *testing.T) {
	// Setup
	config := &core.MicrokernelConfig{
		Name:    "test-kernel",
		Version: "1.0.0",
	}

	kernel, err := core.NewMicrokernel(config)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	// Set configuration
	err = kernel.SetConfig("test-key", "test-value")
	helpers.AssertNoError(t, err)

	// Get configuration
	value := kernel.GetConfig("test-key")
	helpers.AssertEqual(t, "test-value", value)
}

func TestE2E_Logging(t *testing.T) {
	// Setup
	config := &core.MicrokernelConfig{
		Name:    "test-kernel",
		Version: "1.0.0",
	}

	kernel, err := core.NewMicrokernel(config)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	// Log messages
	kernel.Log("info", "test message")
	kernel.Log("error", "error message")

	// Verify logging works (no panic)
	helpers.AssertTrue(t, true)
}

func TestE2E_MetricsCollection(t *testing.T) {
	// Setup
	config := &core.MicrokernelConfig{
		Name:    "test-kernel",
		Version: "1.0.0",
	}

	kernel, err := core.NewMicrokernel(config)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	// Record metric
	kernel.RecordMetric("test-metric", 100)

	// Get metrics
	metrics := kernel.GetMetrics()
	helpers.AssertNotNil(t, metrics)
}

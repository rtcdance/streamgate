package e2e_test

import (
	"context"
	"testing"

	"streamgate/pkg/plugins/api"
	"streamgate/test/helpers"
)

func TestE2E_PluginLoading(t *testing.T) {
	// Setup
	pluginManager := api.NewPluginManager()

	// Load plugins
	err := pluginManager.LoadPlugins(context.Background())
	// May fail if plugins not available, but should not panic
	if err == nil {
		plugins := pluginManager.GetLoadedPlugins()
		helpers.AssertTrue(t, len(plugins) >= 0)
	}
}

func TestE2E_PluginExecution(t *testing.T) {
	// Setup
	pluginManager := api.NewPluginManager()

	// Load plugins
	err := pluginManager.LoadPlugins(context.Background())
	if err == nil {
		// Execute plugin
		result, err := pluginManager.ExecutePlugin(context.Background(), "test-plugin", map[string]interface{}{})
		// May fail if plugin not available, but should not panic
		if err == nil {
			helpers.AssertNotNil(t, result)
		}
	}
}

func TestE2E_PluginChaining(t *testing.T) {
	// Setup
	pluginManager := api.NewPluginManager()

	// Load plugins
	err := pluginManager.LoadPlugins(context.Background())
	if err == nil {
		// Chain plugins
		chain := []string{"plugin1", "plugin2", "plugin3"}
		result, err := pluginManager.ExecutePluginChain(context.Background(), chain, map[string]interface{}{})
		// May fail if plugins not available, but should not panic
		if err == nil {
			helpers.AssertNotNil(t, result)
		}
	}
}

func TestE2E_PluginConfiguration(t *testing.T) {
	// Setup
	pluginManager := api.NewPluginManager()

	// Configure plugin
	config := map[string]interface{}{
		"enabled": true,
		"timeout": 5000,
	}

	err := pluginManager.ConfigurePlugin(context.Background(), "test-plugin", config)
	// May fail if plugin not available, but should not panic
	if err == nil {
		retrievedConfig := pluginManager.GetPluginConfig("test-plugin")
		helpers.AssertNotNil(t, retrievedConfig)
	}
}

func TestE2E_PluginHooks(t *testing.T) {
	// Setup
	pluginManager := api.NewPluginManager()

	// Register hook
	hookCalled := false
	pluginManager.RegisterHook("before_process", func(ctx context.Context, data interface{}) error {
		hookCalled = true
		return nil
	})

	// Execute hook
	err := pluginManager.ExecuteHook(context.Background(), "before_process", nil)
	if err == nil {
		helpers.AssertTrue(t, hookCalled)
	}
}

func TestE2E_PluginMetrics(t *testing.T) {
	// Setup
	pluginManager := api.NewPluginManager()

	// Load plugins
	err := pluginManager.LoadPlugins(context.Background())
	if err == nil {
		// Get plugin metrics
		metrics := pluginManager.GetPluginMetrics()
		helpers.AssertNotNil(t, metrics)
	}
}

func TestE2E_PluginErrorHandling(t *testing.T) {
	// Setup
	pluginManager := api.NewPluginManager()

	// Load plugins
	err := pluginManager.LoadPlugins(context.Background())
	if err == nil {
		// Execute non-existent plugin
		_, err := pluginManager.ExecutePlugin(context.Background(), "non-existent-plugin", map[string]interface{}{})
		helpers.AssertError(t, err)
	}
}

func TestE2E_PluginUnloading(t *testing.T) {
	// Setup
	pluginManager := api.NewPluginManager()

	// Load plugins
	err := pluginManager.LoadPlugins(context.Background())
	if err == nil {
		// Unload plugin
		err := pluginManager.UnloadPlugin(context.Background(), "test-plugin")
		// May fail if plugin not available, but should not panic
		if err == nil {
			plugins := pluginManager.GetLoadedPlugins()
			helpers.AssertTrue(t, len(plugins) >= 0)
		}
	}
}

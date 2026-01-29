package e2e_test

import (
	"context"
	"testing"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/plugins/api"
	"streamgate/test/helpers"
)

type testPlugin struct {
	name string
}

func (m *testPlugin) Name() string {
	return m.name
}

func (m *testPlugin) Version() string {
	return "1.0.0"
}

func (m *testPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	return nil
}

func (m *testPlugin) Start(ctx context.Context) error {
	return nil
}

func (m *testPlugin) Stop(ctx context.Context) error {
	return nil
}

func (m *testPlugin) Health(ctx context.Context) error {
	return nil
}

func TestE2E_PluginLoading(t *testing.T) {
	plugin := &testPlugin{
		name: "test-plugin",
	}

	helpers.AssertNotNil(t, plugin)
	helpers.AssertEqual(t, "test-plugin", plugin.Name())
	helpers.AssertEqual(t, "1.0.0", plugin.Version())
}

func TestE2E_PluginExecution(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	helpers.AssertNoError(t, err)
	defer kernel.Shutdown(context.Background())

	plugin := &testPlugin{
		name: "test-plugin",
	}

	err = plugin.Init(context.Background(), kernel)
	helpers.AssertNoError(t, err)

	err = plugin.Start(context.Background())
	helpers.AssertNoError(t, err)

	err = plugin.Stop(context.Background())
	helpers.AssertNoError(t, err)

	err = plugin.Health(context.Background())
	helpers.AssertNoError(t, err)
}

func TestE2E_GatewayPlugin(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
	}

	plugin := api.NewGatewayPlugin(cfg, nil)

	helpers.AssertNotNil(t, plugin)
	helpers.AssertEqual(t, "api-gateway", plugin.Name())
	helpers.AssertEqual(t, "1.0.0", plugin.Version())
}

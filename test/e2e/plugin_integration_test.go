package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/plugins/api"
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

	require.NotNil(t, plugin)
	require.Equal(t, "test-plugin", plugin.Name())
	require.Equal(t, "1.0.0", plugin.Version())
}

func TestE2E_PluginExecution(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	kernel, err := core.NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	defer kernel.Shutdown(context.Background())

	plugin := &testPlugin{
		name: "test-plugin",
	}

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	err = plugin.Start(context.Background())
	require.NoError(t, err)

	err = plugin.Stop(context.Background())
	require.NoError(t, err)

	err = plugin.Health(context.Background())
	require.NoError(t, err)
}

func TestE2E_GatewayPlugin(t *testing.T) {
	cfg := &config.Config{
		AppName: "test-kernel",
		Port:    8080,
		Mode:    "monolithic",
	}

	plugin := api.NewGatewayPlugin(cfg, nil)

	require.NotNil(t, plugin)
	require.Equal(t, "api-gateway", plugin.Name())
	require.Equal(t, "1.0.0", plugin.Version())
}

package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/core/event"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockPlugin struct {
	name        string
	version     string
	initialized bool
	started     bool
	healthErr   error
	initErr     error
	startErr    error
	stopErr     error
	deps        []string
}

func (m *mockPlugin) Name() string {
	return m.name
}

func (m *mockPlugin) Version() string {
	return m.version
}

func (m *mockPlugin) Init(ctx context.Context, kernel *Microkernel) error {
	if m.initErr != nil {
		return m.initErr
	}
	m.initialized = true
	return nil
}

func (m *mockPlugin) Start(ctx context.Context) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started = true
	return nil
}

func (m *mockPlugin) Stop(ctx context.Context) error {
	if m.stopErr != nil {
		return m.stopErr
	}
	m.started = false
	return nil
}

func (m *mockPlugin) Health(ctx context.Context) error {
	return m.healthErr
}

func (m *mockPlugin) DependsOn() []string {
	return m.deps
}

func newTestKernel(t *testing.T) *Microkernel {
	t.Helper()
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)
	return kernel
}

func TestNewMicrokernel(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolith"}

	kernel, err := NewMicrokernel(cfg, logger)
	assert.NoError(t, err)
	assert.NotNil(t, kernel)
	assert.NotNil(t, kernel.logger)
	assert.NotNil(t, kernel.eventBus)
	assert.NotNil(t, kernel.plugins)
	assert.False(t, kernel.started)
	assert.NotNil(t, kernel.ctx)
}

func TestNewMicrokernel_NilConfig(t *testing.T) {
	_, err := NewMicrokernel(nil, zap.NewNop())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config is required")
}

func TestNewMicrokernel_MonolithicMode(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{Mode: "monolithic"}

	kernel, err := NewMicrokernel(cfg, logger)
	require.NoError(t, err)
	assert.NotNil(t, kernel.eventBus)
}

func TestNewMicrokernel_NilLogger(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}

	kernel, err := NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	assert.NotNil(t, kernel)
	assert.NotNil(t, kernel.logger)
}

func TestNewMicrokernel_NilLoggerProduction(t *testing.T) {
	t.Skip("requires NATS integration build tag")
	cfg := &config.Config{Mode: "production"}

	kernel, err := NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	assert.NotNil(t, kernel)
	assert.NotNil(t, kernel.logger)
}

func TestNewMicrokernel_NilLoggerProd(t *testing.T) {
	t.Skip("requires NATS integration build tag")
	cfg := &config.Config{Mode: "prod"}

	kernel, err := NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	assert.NotNil(t, kernel)
}

func TestNewMicrokernel_NilLoggerMicroservices(t *testing.T) {
	t.Skip("requires NATS integration build tag")
	cfg := &config.Config{Mode: "microservices"}

	kernel, err := NewMicrokernel(cfg, nil)
	require.NoError(t, err)
	assert.NotNil(t, kernel)
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
	kernel := newTestKernel(t)

	plugin := &mockPlugin{name: "test-plugin", version: "1.0.0"}

	err := kernel.RegisterPlugin(plugin)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(kernel.plugins))

	retrievedPlugin, err := kernel.GetPlugin("test-plugin")
	assert.NoError(t, err)
	assert.Equal(t, plugin, retrievedPlugin)
}

func TestMicrokernel_RegisterPlugin_Duplicate(t *testing.T) {
	kernel := newTestKernel(t)

	plugin := &mockPlugin{name: "test-plugin", version: "1.0.0"}

	err := kernel.RegisterPlugin(plugin)
	require.NoError(t, err)

	err = kernel.RegisterPlugin(plugin)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestMicrokernel_RegisterPlugin_Multiple(t *testing.T) {
	kernel := newTestKernel(t)

	p1 := &mockPlugin{name: "plugin-a", version: "1.0.0"}
	p2 := &mockPlugin{name: "plugin-b", version: "2.0.0"}
	p3 := &mockPlugin{name: "plugin-c", version: "3.0.0"}

	require.NoError(t, kernel.RegisterPlugin(p1))
	require.NoError(t, kernel.RegisterPlugin(p2))
	require.NoError(t, kernel.RegisterPlugin(p3))

	assert.Equal(t, 3, len(kernel.plugins))

	got, err := kernel.GetPlugin("plugin-b")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", got.Version())
}

func TestMicrokernel_GetPlugin_NotFound(t *testing.T) {
	kernel := newTestKernel(t)

	_, err := kernel.GetPlugin("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMicrokernel_GetEventBus(t *testing.T) {
	kernel := newTestKernel(t)

	eventBus := kernel.GetEventBus()
	assert.NotNil(t, eventBus)
}

func TestMicrokernel_GetRegistry(t *testing.T) {
	kernel := newTestKernel(t)

	registry := kernel.GetRegistry()
	assert.Nil(t, registry)
}

func TestMicrokernel_GetClientPool(t *testing.T) {
	kernel := newTestKernel(t)

	clientPool := kernel.GetClientPool()
	assert.Nil(t, clientPool)
}

func TestMicrokernel_GetConfig(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	retrievedConfig := kernel.GetConfig()
	assert.Equal(t, cfg, retrievedConfig)
}

func TestMicrokernel_GetLogger(t *testing.T) {
	logger := zap.NewNop()
	kernel := newTestKernel(t)

	retrievedLogger := kernel.GetLogger()
	assert.Equal(t, logger, retrievedLogger)
}

func TestMicrokernel_Start_AlreadyStarted(t *testing.T) {
	kernel := newTestKernel(t)

	ctx := context.Background()
	err := kernel.Start(ctx)
	require.NoError(t, err)

	err = kernel.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")
}

func TestMicrokernel_Start_Success(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "api-gateway", version: "1.0.0"}
	require.NoError(t, kernel.RegisterPlugin(p))

	err := kernel.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, p.initialized)
	assert.True(t, p.started)
}

func TestMicrokernel_Start_MultiplePlugins(t *testing.T) {
	kernel := newTestKernel(t)
	p1 := &mockPlugin{name: "api-gateway", version: "1.0.0"}
	p2 := &mockPlugin{name: "auth", version: "1.0.0"}
	require.NoError(t, kernel.RegisterPlugin(p1))
	require.NoError(t, kernel.RegisterPlugin(p2))

	err := kernel.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, p1.initialized)
	assert.True(t, p1.started)
	assert.True(t, p2.initialized)
	assert.False(t, p2.started, "non-api-gateway plugins should not start in monolith mode")
}

func TestMicrokernel_Start_InitFailure_RollsBack(t *testing.T) {
	kernel := newTestKernel(t)
	p1 := &mockPlugin{name: "good", version: "1.0.0"}
	p2 := &mockPlugin{name: "bad", version: "1.0.0", initErr: fmt.Errorf("init failed"), deps: []string{"good"}}
	require.NoError(t, kernel.RegisterPlugin(p1))
	require.NoError(t, kernel.RegisterPlugin(p2))

	err := kernel.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize plugin bad")
	assert.True(t, p1.initialized)
	assert.False(t, p2.initialized)
}

func TestMicrokernel_Start_StartFailure_RollsBack(t *testing.T) {
	kernel := newTestKernel(t)
	p1 := &mockPlugin{name: "auth", version: "1.0.0"}
	p2 := &mockPlugin{name: "api-gateway", version: "1.0.0", startErr: fmt.Errorf("start failed")}
	require.NoError(t, kernel.RegisterPlugin(p1))
	require.NoError(t, kernel.RegisterPlugin(p2))

	err := kernel.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start plugin api-gateway")
}

func TestMicrokernel_Start_MissingDependency(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "orphan", version: "1.0.0", deps: []string{"nonexistent"}}
	require.NoError(t, kernel.RegisterPlugin(p))

	err := kernel.Start(context.Background())
	assert.Error(t, err)
}

func TestMicrokernel_Start_WithDependencies(t *testing.T) {
	kernel := newTestKernel(t)
	base := &mockPlugin{name: "base", version: "1.0.0"}
	dependent := &mockPlugin{name: "dependent", version: "1.0.0", deps: []string{"base"}}
	require.NoError(t, kernel.RegisterPlugin(base))
	require.NoError(t, kernel.RegisterPlugin(dependent))

	err := kernel.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, base.initialized)
	assert.True(t, dependent.initialized)
}

func TestMicrokernel_Shutdown(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "test", version: "1.0.0"}
	require.NoError(t, kernel.RegisterPlugin(p))
	require.NoError(t, kernel.Start(context.Background()))

	err := kernel.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.False(t, kernel.started)
	assert.False(t, p.started)
}

func TestMicrokernel_Shutdown_NotStarted(t *testing.T) {
	kernel := newTestKernel(t)

	err := kernel.Shutdown(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestMicrokernel_Shutdown_ReverseOrder(t *testing.T) {
	kernel := newTestKernel(t)
	p1 := &mockPlugin{name: "first", version: "1.0.0"}
	p2 := &mockPlugin{name: "second", version: "1.0.0", deps: []string{"first"}}
	require.NoError(t, kernel.RegisterPlugin(p1))
	require.NoError(t, kernel.RegisterPlugin(p2))
	require.NoError(t, kernel.Start(context.Background()))

	err := kernel.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.False(t, p1.started)
	assert.False(t, p2.started)
}

func TestMicrokernel_Shutdown_StopError(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "test", version: "1.0.0", stopErr: fmt.Errorf("stop error")}
	require.NoError(t, kernel.RegisterPlugin(p))
	require.NoError(t, kernel.Start(context.Background()))

	err := kernel.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestMicrokernel_Health_AllHealthy(t *testing.T) {
	kernel := newTestKernel(t)
	p1 := &mockPlugin{name: "p1", version: "1.0.0"}
	p2 := &mockPlugin{name: "p2", version: "1.0.0"}
	require.NoError(t, kernel.RegisterPlugin(p1))
	require.NoError(t, kernel.RegisterPlugin(p2))

	err := kernel.Health(context.Background())
	assert.NoError(t, err)
}

func TestMicrokernel_Health_UnhealthyPlugin(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "api-gateway", version: "1.0.0", healthErr: fmt.Errorf("unhealthy")}
	require.NoError(t, kernel.RegisterPlugin(p))

	err := kernel.Health(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api-gateway")
	assert.Contains(t, err.Error(), "health check failed")
}

func TestMicrokernel_Health_NoPlugins(t *testing.T) {
	kernel := newTestKernel(t)

	err := kernel.Health(context.Background())
	assert.NoError(t, err)
}

func TestTopoSort_Simple(t *testing.T) {
	plugins := map[string]Plugin{
		"a": &mockPlugin{name: "a"},
		"b": &mockPlugin{name: "b"},
		"c": &mockPlugin{name: "c"},
	}
	deps := map[string][]string{
		"a": nil,
		"b": {"a"},
		"c": {"b"},
	}

	order, err := topoSort(plugins, deps)
	require.NoError(t, err)
	assert.Equal(t, 3, len(order))

	idxA, idxB, idxC := -1, -1, -1
	for i, n := range order {
		switch n {
		case "a":
			idxA = i
		case "b":
			idxB = i
		case "c":
			idxC = i
		}
	}
	assert.True(t, idxA < idxB, "a should come before b")
	assert.True(t, idxB < idxC, "b should come before c")
}

func TestTopoSort_NoDeps(t *testing.T) {
	plugins := map[string]Plugin{
		"x": &mockPlugin{name: "x"},
		"y": &mockPlugin{name: "y"},
	}
	deps := map[string][]string{
		"x": nil,
		"y": nil,
	}

	order, err := topoSort(plugins, deps)
	require.NoError(t, err)
	assert.Equal(t, 2, len(order))
}

func TestTopoSort_CircularDependency(t *testing.T) {
	plugins := map[string]Plugin{
		"a": &mockPlugin{name: "a"},
		"b": &mockPlugin{name: "b"},
	}
	deps := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}

	_, err := topoSort(plugins, deps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestTopoSort_SelfDependency(t *testing.T) {
	plugins := map[string]Plugin{
		"a": &mockPlugin{name: "a"},
	}
	deps := map[string][]string{
		"a": {"a"},
	}

	order, err := topoSort(plugins, deps)
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, order)
}

func TestTopoSort_DiamondDependency(t *testing.T) {
	plugins := map[string]Plugin{
		"a": &mockPlugin{name: "a"},
		"b": &mockPlugin{name: "b"},
		"c": &mockPlugin{name: "c"},
		"d": &mockPlugin{name: "d"},
	}
	deps := map[string][]string{
		"a": nil,
		"b": {"a"},
		"c": {"a"},
		"d": {"b", "c"},
	}

	order, err := topoSort(plugins, deps)
	require.NoError(t, err)
	assert.Equal(t, 4, len(order))

	idx := make(map[string]int)
	for i, n := range order {
		idx[n] = i
	}
	assert.True(t, idx["a"] < idx["b"])
	assert.True(t, idx["a"] < idx["c"])
	assert.True(t, idx["b"] < idx["d"])
	assert.True(t, idx["c"] < idx["d"])
}

func TestRegisterPluginFactory(t *testing.T) {
	factoryMu.Lock()
	for k := range pluginFactories {
		delete(pluginFactories, k)
	}
	factoryMu.Unlock()

	factory := func(cfg *config.Config, logger *zap.Logger) Plugin {
		return &mockPlugin{name: "factory-test", version: "1.0.0"}
	}

	RegisterPluginFactory("factory-test", factory)
	got := GetPluginFactory("factory-test")
	assert.NotNil(t, got)

	names := RegisteredPluginNames()
	assert.Contains(t, names, "factory-test")

	factoryMu.Lock()
	delete(pluginFactories, "factory-test")
	factoryMu.Unlock()
}

func TestRegisterPluginFactory_DuplicatePanics(t *testing.T) {
	factoryMu.Lock()
	for k := range pluginFactories {
		delete(pluginFactories, k)
	}
	factoryMu.Unlock()

	factory := func(cfg *config.Config, logger *zap.Logger) Plugin {
		return &mockPlugin{name: "dup", version: "1.0.0"}
	}

	RegisterPluginFactory("dup", factory)
	assert.Panics(t, func() {
		RegisterPluginFactory("dup", factory)
	})

	factoryMu.Lock()
	delete(pluginFactories, "dup")
	factoryMu.Unlock()
}

func TestMustRegisterPluginFactory(t *testing.T) {
	factoryMu.Lock()
	for k := range pluginFactories {
		delete(pluginFactories, k)
	}
	factoryMu.Unlock()

	f1 := func(cfg *config.Config, logger *zap.Logger) Plugin {
		return &mockPlugin{name: "override", version: "1.0.0"}
	}
	f2 := func(cfg *config.Config, logger *zap.Logger) Plugin {
		return &mockPlugin{name: "override", version: "2.0.0"}
	}

	MustRegisterPluginFactory("override", f1)
	MustRegisterPluginFactory("override", f2)

	got := GetPluginFactory("override")
	assert.NotNil(t, got)

	factoryMu.Lock()
	delete(pluginFactories, "override")
	factoryMu.Unlock()
}

func TestGetPluginFactory_NotFound(t *testing.T) {
	got := GetPluginFactory("nonexistent-factory")
	assert.Nil(t, got)
}

func TestLoadRegisteredPlugins(t *testing.T) {
	factoryMu.Lock()
	for k := range pluginFactories {
		delete(pluginFactories, k)
	}
	factoryMu.Unlock()

	RegisterPluginFactory("auto-a", func(cfg *config.Config, logger *zap.Logger) Plugin {
		return &mockPlugin{name: "auto-a", version: "1.0.0"}
	})
	RegisterPluginFactory("auto-b", func(cfg *config.Config, logger *zap.Logger) Plugin {
		return &mockPlugin{name: "auto-b", version: "1.0.0"}
	})

	kernel := newTestKernel(t)
	err := kernel.LoadRegisteredPlugins()
	require.NoError(t, err)

	_, err = kernel.GetPlugin("auto-a")
	assert.NoError(t, err)
	_, err = kernel.GetPlugin("auto-b")
	assert.NoError(t, err)

	factoryMu.Lock()
	delete(pluginFactories, "auto-a")
	delete(pluginFactories, "auto-b")
	factoryMu.Unlock()
}

func TestLoadRegisteredPlugins_DuplicateInKernel(t *testing.T) {
	factoryMu.Lock()
	for k := range pluginFactories {
		delete(pluginFactories, k)
	}
	factoryMu.Unlock()

	RegisterPluginFactory("dup-load", func(cfg *config.Config, logger *zap.Logger) Plugin {
		return &mockPlugin{name: "dup-load", version: "1.0.0"}
	})

	kernel := newTestKernel(t)
	require.NoError(t, kernel.RegisterPlugin(&mockPlugin{name: "dup-load", version: "1.0.0"}))

	err := kernel.LoadRegisteredPlugins()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	factoryMu.Lock()
	delete(pluginFactories, "dup-load")
	factoryMu.Unlock()
}

type mockEventBus struct {
	event.EventBus
	closeErr error
}

func (m *mockEventBus) Close() error { return m.closeErr }

func TestMicrokernel_Start_InitFailureRollback_StopError(t *testing.T) {
	kernel := newTestKernel(t)
	p1 := &mockPlugin{name: "api-gateway", version: "1.0.0", stopErr: fmt.Errorf("stop during rollback")}
	p2 := &mockPlugin{name: "bad", version: "1.0.0", initErr: fmt.Errorf("init failed"), deps: []string{"api-gateway"}}
	require.NoError(t, kernel.RegisterPlugin(p1))
	require.NoError(t, kernel.RegisterPlugin(p2))

	err := kernel.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize plugin bad")
}

func TestMicrokernel_Start_StartFailureRollback_StopError(t *testing.T) {
	kernel := newTestKernel(t)
	p1 := &mockPlugin{name: "auth", version: "1.0.0", stopErr: fmt.Errorf("stop during start rollback")}
	p2 := &mockPlugin{name: "api-gateway", version: "1.0.0", startErr: fmt.Errorf("start failed")}
	require.NoError(t, kernel.RegisterPlugin(p1))
	require.NoError(t, kernel.RegisterPlugin(p2))

	err := kernel.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start plugin api-gateway")
}

func TestMicrokernel_Shutdown_MultiplePlugins_StopError(t *testing.T) {
	kernel := newTestKernel(t)
	p1 := &mockPlugin{name: "api-gateway", version: "1.0.0"}
	p2 := &mockPlugin{name: "auth", version: "1.0.0", stopErr: fmt.Errorf("stop error")}
	require.NoError(t, kernel.RegisterPlugin(p1))
	require.NoError(t, kernel.RegisterPlugin(p2))
	require.NoError(t, kernel.Start(context.Background()))

	err := kernel.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestMicrokernel_Shutdown_ContextCancelled(t *testing.T) {
	kernel := newTestKernel(t)
	p := &mockPlugin{name: "api-gateway", version: "1.0.0"}
	require.NoError(t, kernel.RegisterPlugin(p))
	require.NoError(t, kernel.Start(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := kernel.Shutdown(ctx)
	assert.NoError(t, err)
	assert.False(t, kernel.started)
}

func TestMicrokernel_Shutdown_EventBusCloseError(t *testing.T) {
	kernel := newTestKernel(t)
	kernel.eventBus = &mockEventBus{closeErr: fmt.Errorf("bus close failed")}
	p := &mockPlugin{name: "api-gateway", version: "1.0.0"}
	require.NoError(t, kernel.RegisterPlugin(p))
	require.NoError(t, kernel.Start(context.Background()))

	err := kernel.Shutdown(context.Background())
	assert.NoError(t, err)
}

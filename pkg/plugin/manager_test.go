package plugin

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockPlugin implements Plugin for testing
type mockPlugin struct {
	name       string
	version    string
	ptype      string
	config     map[string]interface{}
	metadata  map[string]interface{}
	healthErr error
	startErr  error
	stopErr   error
	initErr   error
}

func newMockPlugin(name, version, ptype string) *mockPlugin {
	return &mockPlugin{
		name:      name,
		version:   version,
		ptype:     ptype,
		config:    make(map[string]interface{}),
		metadata:  make(map[string]interface{}),
	}
}

func (m *mockPlugin) Name() string                      { return m.name }
func (m *mockPlugin) Version() string                   { return m.version }
func (m *mockPlugin) Type() string                      { return m.ptype }
func (m *mockPlugin) Config() map[string]interface{}    { return m.config }
func (m *mockPlugin) Metadata() map[string]interface{}  { return m.metadata }
func (m *mockPlugin) Initialize(ctx context.Context, config map[string]interface{}) error {
	m.config = config
	return m.initErr
}
func (m *mockPlugin) Start(ctx context.Context) error   { return m.startErr }
func (m *mockPlugin) Stop(ctx context.Context) error    { return m.stopErr }
func (m *mockPlugin) HealthCheck(ctx context.Context) error { return m.healthErr }

// helper to register a plugin directly into the manager's internal maps
func registerTestPlugin(pm *PluginManager, p Plugin, state PluginState) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.plugins[p.Name()] = p
	pm.pluginInfo[p.Name()] = &PluginInfo{
		Name:     p.Name(),
		Version:  p.Version(),
		Type:     p.Type(),
		State:    state,
		Config:   p.Config(),
		Metadata: p.Metadata(),
	}
}

func TestPluginState_String(t *testing.T) {
	tests := []struct {
		state PluginState
		want  string
	}{
		{StateUnloaded, "unloaded"},
		{StateLoading, "loading"},
		{StateLoaded, "loaded"},
		{StateRunning, "running"},
		{StateStopping, "stopping"},
		{StateError, "error"},
		{PluginState(99), "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.state.String())
	}
}

func TestNewPluginManager(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	assert.NotNil(t, pm)
	assert.Empty(t, pm.ListPlugins())
}

func TestPluginManager_GetPlugin_NotFound(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	_, err := pm.GetPlugin("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPluginManager_GetPluginInfo_NotFound(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	_, err := pm.GetPluginInfo("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPluginManager_RegisterAndGetPlugin(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("test-plugin", "1.0.0", "service")
	registerTestPlugin(pm, p, StateLoaded)

	got, err := pm.GetPlugin("test-plugin")
	require.NoError(t, err)
	assert.Equal(t, "test-plugin", got.Name())
	assert.Equal(t, "1.0.0", got.Version())
	assert.Equal(t, "service", got.Type())
}

func TestPluginManager_GetPluginInfo(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("info-plugin", "2.0.0", "worker")
	registerTestPlugin(pm, p, StateLoaded)

	info, err := pm.GetPluginInfo("info-plugin")
	require.NoError(t, err)
	assert.Equal(t, "info-plugin", info.Name)
	assert.Equal(t, "2.0.0", info.Version)
	assert.Equal(t, "worker", info.Type)
	assert.Equal(t, StateLoaded, info.State)
}

func TestPluginManager_ListPlugins(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	registerTestPlugin(pm, newMockPlugin("a", "1.0", "service"), StateLoaded)
	registerTestPlugin(pm, newMockPlugin("b", "2.0", "worker"), StateRunning)

	list := pm.ListPlugins()
	assert.Len(t, list, 2)
	assert.Contains(t, list, "a")
	assert.Contains(t, list, "b")
}

func TestPluginManager_ListPluginsByType(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	registerTestPlugin(pm, newMockPlugin("svc1", "1.0", "service"), StateLoaded)
	registerTestPlugin(pm, newMockPlugin("svc2", "1.0", "service"), StateLoaded)
	registerTestPlugin(pm, newMockPlugin("wrk1", "1.0", "worker"), StateLoaded)

	services := pm.ListPluginsByType("service")
	assert.Len(t, services, 2)

	workers := pm.ListPluginsByType("worker")
	assert.Len(t, workers, 1)

	none := pm.ListPluginsByType("nonexistent")
	assert.Len(t, none, 0)
}

func TestPluginManager_StartStopPlugin(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("lifecycle", "1.0", "service")
	registerTestPlugin(pm, p, StateLoaded)

	// Start
	err := pm.StartPlugin(context.Background(), "lifecycle")
	require.NoError(t, err)

	info, _ := pm.GetPluginInfo("lifecycle")
	assert.Equal(t, StateRunning, info.State)
	assert.False(t, info.StartedAt.IsZero())

	// Stop
	err = pm.StopPlugin(context.Background(), "lifecycle")
	require.NoError(t, err)

	info, _ = pm.GetPluginInfo("lifecycle")
	assert.Equal(t, StateLoaded, info.State)
}

func TestPluginManager_StartPlugin_NotFound(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	err := pm.StartPlugin(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestPluginManager_StartPlugin_WrongState(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("running", "1.0", "service")
	registerTestPlugin(pm, p, StateRunning) // already running

	err := pm.StartPlugin(context.Background(), "running")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in loaded state")
}

func TestPluginManager_StopPlugin_NotRunning(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("loaded", "1.0", "service")
	registerTestPlugin(pm, p, StateLoaded) // not running

	err := pm.StopPlugin(context.Background(), "loaded")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestPluginManager_StartPlugin_Error(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("fail-start", "1.0", "service")
	p.startErr = fmt.Errorf("start failed")
	registerTestPlugin(pm, p, StateLoaded)

	err := pm.StartPlugin(context.Background(), "fail-start")
	assert.Error(t, err)

	info, _ := pm.GetPluginInfo("fail-start")
	assert.Equal(t, StateError, info.State)
}

func TestPluginManager_StopPlugin_Error(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("fail-stop", "1.0", "service")
	p.stopErr = fmt.Errorf("stop failed")
	registerTestPlugin(pm, p, StateRunning)

	err := pm.StopPlugin(context.Background(), "fail-stop")
	assert.Error(t, err)

	info, _ := pm.GetPluginInfo("fail-stop")
	assert.Equal(t, StateError, info.State)
}

func TestPluginManager_UnloadPlugin(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("to-unload", "1.0", "service")
	registerTestPlugin(pm, p, StateLoaded)

	err := pm.UnloadPlugin(context.Background(), "to-unload")
	require.NoError(t, err)

	_, err = pm.GetPlugin("to-unload")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPluginManager_UnloadPlugin_Running(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("running-unload", "1.0", "service")
	registerTestPlugin(pm, p, StateRunning)

	// Should stop then unload
	err := pm.UnloadPlugin(context.Background(), "running-unload")
	require.NoError(t, err)

	_, err = pm.GetPlugin("running-unload")
	assert.Error(t, err)
}

func TestPluginManager_UnloadPlugin_NotFound(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	err := pm.UnloadPlugin(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestPluginManager_HealthCheck(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())

	healthy := newMockPlugin("healthy", "1.0", "service")
	registerTestPlugin(pm, healthy, StateRunning)

	unhealthy := newMockPlugin("unhealthy", "1.0", "service")
	unhealthy.healthErr = fmt.Errorf("not healthy")
	registerTestPlugin(pm, unhealthy, StateRunning)

	results := pm.HealthCheck(context.Background())
	assert.Len(t, results, 1) // only unhealthy plugin
	assert.Contains(t, results, "unhealthy")
}

func TestPluginManager_HealthCheck_AllHealthy(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	registerTestPlugin(pm, newMockPlugin("ok1", "1.0", "service"), StateRunning)
	registerTestPlugin(pm, newMockPlugin("ok2", "1.0", "worker"), StateRunning)

	results := pm.HealthCheck(context.Background())
	assert.Empty(t, results)
}

func TestPluginManager_UpdatePluginConfig(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	p := newMockPlugin("configurable", "1.0", "service")
	registerTestPlugin(pm, p, StateLoaded)

	newConfig := map[string]interface{}{"key": "value"}
	err := pm.UpdatePluginConfig("configurable", newConfig)
	require.NoError(t, err)

	info, _ := pm.GetPluginInfo("configurable")
	assert.Equal(t, "value", info.Config["key"])
}

func TestPluginManager_UpdatePluginConfig_NotFound(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	err := pm.UpdatePluginConfig("nonexistent", nil)
	assert.Error(t, err)
}

func TestPluginManager_StartAll(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	registerTestPlugin(pm, newMockPlugin("a", "1.0", "service"), StateLoaded)
	registerTestPlugin(pm, newMockPlugin("b", "1.0", "worker"), StateLoaded)

	err := pm.StartAll(context.Background())
	require.NoError(t, err)

	infoA, _ := pm.GetPluginInfo("a")
	infoB, _ := pm.GetPluginInfo("b")
	assert.Equal(t, StateRunning, infoA.State)
	assert.Equal(t, StateRunning, infoB.State)
}

func TestPluginManager_StopAll(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	registerTestPlugin(pm, newMockPlugin("a", "1.0", "service"), StateRunning)
	registerTestPlugin(pm, newMockPlugin("b", "1.0", "worker"), StateRunning)

	err := pm.StopAll(context.Background())
	require.NoError(t, err)

	infoA, _ := pm.GetPluginInfo("a")
	infoB, _ := pm.GetPluginInfo("b")
	assert.Equal(t, StateLoaded, infoA.State)
	assert.Equal(t, StateLoaded, infoB.State)
}

func TestPluginManager_UnloadAll(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	registerTestPlugin(pm, newMockPlugin("a", "1.0", "service"), StateRunning)
	registerTestPlugin(pm, newMockPlugin("b", "1.0", "worker"), StateLoaded)

	err := pm.UnloadAll(context.Background())
	require.NoError(t, err)

	assert.Empty(t, pm.ListPlugins())
}

func TestPluginManager_SetHotReload(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())
	pm.SetHotReload(true)
	assert.True(t, pm.hotReload)
	pm.SetHotReload(false)
	assert.False(t, pm.hotReload)
}

func TestBasePlugin(t *testing.T) {
	bp := NewBasePlugin("test", "1.0.0", "service", zap.NewNop())

	assert.Equal(t, "test", bp.Name())
	assert.Equal(t, "1.0.0", bp.Version())
	assert.Equal(t, "service", bp.Type())

	ctx := context.Background()
	config := map[string]interface{}{"timeout": 30}
	err := bp.Initialize(ctx, config)
	require.NoError(t, err)
	assert.Equal(t, 30, bp.Config()["timeout"])

	err = bp.Start(ctx)
	require.NoError(t, err)

	err = bp.Stop(ctx)
	require.NoError(t, err)

	err = bp.HealthCheck(ctx)
	require.NoError(t, err)

	assert.NoError(t, bp.ValidateConfig())
}

func TestBasePlugin_ConfigAccessors(t *testing.T) {
	bp := NewBasePlugin("test", "1.0", "service", zap.NewNop())
	bp.config = map[string]interface{}{
		"string_val": "hello",
		"int_val":    42,
		"float_val":  float64(3.14),
		"bool_val":   true,
		"duration":   "5s",
		"slice_val":  []string{"a", "b"},
	}

	s, ok := bp.GetConfigString("string_val")
	assert.True(t, ok)
	assert.Equal(t, "hello", s)

	i, ok := bp.GetConfigInt("int_val")
	assert.True(t, ok)
	assert.Equal(t, 42, i)

	// float64 is auto-converted to int
	fi, ok := bp.GetConfigInt("float_val")
	assert.True(t, ok)
	assert.Equal(t, 3, fi)

	b, ok := bp.GetConfigBool("bool_val")
	assert.True(t, ok)
	assert.True(t, b)

	d, ok := bp.GetConfigDuration("duration")
	assert.True(t, ok)
	assert.Equal(t, 5, int(d.Seconds()))

	sl, ok := bp.GetConfigSlice("slice_val")
	assert.True(t, ok)
	assert.Len(t, sl, 2)
}

func TestBasePlugin_ConfigAccessors_Missing(t *testing.T) {
	bp := NewBasePlugin("test", "1.0", "service", zap.NewNop())

	_, ok := bp.GetConfigString("missing")
	assert.False(t, ok)

	_, ok = bp.GetConfigInt("missing")
	assert.False(t, ok)

	_, ok = bp.GetConfigBool("missing")
	assert.False(t, ok)

	_, ok = bp.GetConfigDuration("missing")
	assert.False(t, ok)

	_, ok = bp.GetConfigSlice("missing")
	assert.False(t, ok)
}

func TestBasePlugin_GetConfigValueWithDefault(t *testing.T) {
	bp := NewBasePlugin("test", "1.0", "service", zap.NewNop())

	val := bp.GetConfigValueWithDefault("missing", "default")
	assert.Equal(t, "default", val)

	bp.config["existing"] = "value"
	val = bp.GetConfigValueWithDefault("existing", "default")
	assert.Equal(t, "value", val)
}

func TestBasePlugin_SetMetadata(t *testing.T) {
	bp := NewBasePlugin("test", "1.0", "service", zap.NewNop())
	bp.SetMetadata("key", "value")
	assert.Equal(t, "value", bp.Metadata()["key"])
}

func TestBasePlugin_Reload(t *testing.T) {
	bp := NewBasePlugin("test", "1.0", "service", zap.NewNop())
	newConfig := map[string]interface{}{"reloaded": true}
	err := bp.Reload(context.Background(), newConfig)
	require.NoError(t, err)
	assert.True(t, bp.Config()["reloaded"].(bool))
}

func TestGetPluginType(t *testing.T) {
	assert.Equal(t, "unknown", GetPluginType(nil))
	assert.Equal(t, "mockPlugin", GetPluginType(&mockPlugin{}))
}

func TestPluginManager_SetEventBus(t *testing.T) {
	pm := NewPluginManager(zap.NewNop())

	var publishedTopic string
	var publishedEvent interface{}
	mockBus := &mockEventBus{
		publishFn: func(ctx context.Context, topic string, event interface{}) error {
			publishedTopic = topic
			publishedEvent = event
			return nil
		},
	}

	pm.SetEventBus(mockBus)

	// Use internal publishEvent to verify it calls the bus
	pm.publishEvent(context.Background(), "plugin.test", map[string]interface{}{"name": "x"})
	assert.Equal(t, "plugin.events", publishedTopic)
	assert.NotNil(t, publishedEvent)
}

// mockEventBus implements EventBus for testing
type mockEventBus struct {
	publishFn func(ctx context.Context, topic string, event interface{}) error
}

func (m *mockEventBus) Publish(ctx context.Context, topic string, event interface{}) error {
	if m.publishFn != nil {
		return m.publishFn(ctx, topic, event)
	}
	return nil
}

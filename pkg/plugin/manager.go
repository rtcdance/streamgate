package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"plugin"
	"reflect"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PluginState represents the state of a plugin
type PluginState int

const (
	StateUnloaded PluginState = iota
	StateLoading
	StateLoaded
	StateRunning
	StateStopping
	StateError
)

func (s PluginState) String() string {
	switch s {
	case StateUnloaded:
		return "unloaded"
	case StateLoading:
		return "loading"
	case StateLoaded:
		return "loaded"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// Plugin defines the interface that all plugins must implement
type Plugin interface {
	Name() string
	Version() string
	Type() string
	Initialize(ctx context.Context, config map[string]interface{}) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	HealthCheck(ctx context.Context) error
	Config() map[string]interface{}
	Metadata() map[string]interface{}
}

// PluginInfo holds information about a plugin
type PluginInfo struct {
	Name         string
	Version      string
	Type         string
	Path         string
	State        PluginState
	Config       map[string]interface{}
	Metadata     map[string]interface{}
	LoadedAt     time.Time
	StartedAt    time.Time
	Error        error
	Dependencies []string
}

// PluginManager manages plugins
type PluginManager struct {
	plugins    map[string]Plugin
	pluginInfo map[string]*PluginInfo
	mu         sync.RWMutex
	logger     *zap.Logger
	configPath string
	hotReload  bool
	eventBus   EventBus
}

// EventBus defines the interface for plugin events
type EventBus interface {
	Publish(ctx context.Context, topic string, event interface{}) error
}

// NewPluginManager creates a new plugin manager
func NewPluginManager(logger *zap.Logger) *PluginManager {
	return &PluginManager{
		plugins:    make(map[string]Plugin),
		pluginInfo: make(map[string]*PluginInfo),
		logger:     logger,
		hotReload:  false,
	}
}

// LoadPlugin loads a plugin from a file
func (pm *PluginManager) LoadPlugin(ctx context.Context, path string, config map[string]interface{}) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve plugin path: %w", err)
	}

	pm.logger.Info("Loading plugin", zap.String("path", absPath))

	plug, err := plugin.Open(absPath)
	if err != nil {
		return fmt.Errorf("failed to open plugin: %w", err)
	}

	symPlugin, err := plug.Lookup("Plugin")
	if err != nil {
		return fmt.Errorf("failed to lookup Plugin symbol: %w", err)
	}

	pluginInstance, ok := symPlugin.(Plugin)
	if !ok {
		return fmt.Errorf("plugin does not implement the Plugin interface")
	}

	name := pluginInstance.Name()
	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin '%s' is already loaded", name)
	}

	info := &PluginInfo{
		Name:         name,
		Version:      pluginInstance.Version(),
		Type:         pluginInstance.Type(),
		Path:         absPath,
		State:        StateLoading,
		Config:       config,
		Metadata:     pluginInstance.Metadata(),
		Dependencies: pm.extractDependencies(pluginInstance),
	}

	pm.pluginInfo[name] = info

	if err := pm.checkDependencies(info.Dependencies); err != nil {
		info.State = StateError
		info.Error = err
		return fmt.Errorf("dependency check failed: %w", err)
	}

	if err := pluginInstance.Initialize(ctx, config); err != nil {
		info.State = StateError
		info.Error = err
		return fmt.Errorf("failed to initialize plugin: %w", err)
	}

	info.State = StateLoaded
	info.LoadedAt = time.Now()
	pm.plugins[name] = pluginInstance

	pm.logger.Info("Plugin loaded successfully",
		zap.String("name", name),
		zap.String("version", info.Version),
		zap.String("type", info.Type))

	pm.publishEvent(ctx, "plugin.loaded", map[string]interface{}{
		"name":    name,
		"version": info.Version,
		"type":    info.Type,
	})

	return nil
}

// UnloadPlugin unloads a plugin
func (pm *PluginManager) UnloadPlugin(ctx context.Context, name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pluginInstance, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	info := pm.pluginInfo[name]

	if info.State == StateRunning {
		if err := pluginInstance.Stop(ctx); err != nil {
			pm.logger.Error("Failed to stop plugin during unload",
				zap.String("name", name),
				zap.Error(err))
		}
	}

	delete(pm.plugins, name)
	delete(pm.pluginInfo, name)

	pm.logger.Info("Plugin unloaded", zap.String("name", name))

	pm.publishEvent(ctx, "plugin.unloaded", map[string]interface{}{
		"name": name,
	})

	return nil
}

// StartPlugin starts a loaded plugin
func (pm *PluginManager) StartPlugin(ctx context.Context, name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pluginInstance, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	info := pm.pluginInfo[name]

	if info.State != StateLoaded {
		return fmt.Errorf("plugin '%s' is not in loaded state", name)
	}

	info.State = StateLoading

	if err := pluginInstance.Start(ctx); err != nil {
		info.State = StateError
		info.Error = err
		return fmt.Errorf("failed to start plugin: %w", err)
	}

	info.State = StateRunning
	info.StartedAt = time.Now()

	pm.logger.Info("Plugin started", zap.String("name", name))

	pm.publishEvent(ctx, "plugin.started", map[string]interface{}{
		"name": name,
	})

	return nil
}

// StopPlugin stops a running plugin
func (pm *PluginManager) StopPlugin(ctx context.Context, name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pluginInstance, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	info := pm.pluginInfo[name]

	if info.State != StateRunning {
		return fmt.Errorf("plugin '%s' is not running", name)
	}

	info.State = StateStopping

	if err := pluginInstance.Stop(ctx); err != nil {
		info.State = StateError
		info.Error = err
		return fmt.Errorf("failed to stop plugin: %w", err)
	}

	info.State = StateLoaded

	pm.logger.Info("Plugin stopped", zap.String("name", name))

	pm.publishEvent(ctx, "plugin.stopped", map[string]interface{}{
		"name": name,
	})

	return nil
}

// ReloadPlugin reloads a plugin
func (pm *PluginManager) ReloadPlugin(ctx context.Context, name string) error {
	pm.mu.RLock()
	info, exists := pm.pluginInfo[name]
	pm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	path := info.Path
	config := info.Config

	if err := pm.UnloadPlugin(ctx, name); err != nil {
		return fmt.Errorf("failed to unload plugin: %w", err)
	}

	if err := pm.LoadPlugin(ctx, path, config); err != nil {
		return fmt.Errorf("failed to load plugin: %w", err)
	}

	if info.State == StateRunning {
		if err := pm.StartPlugin(ctx, name); err != nil {
			return fmt.Errorf("failed to start plugin: %w", err)
		}
	}

	pm.logger.Info("Plugin reloaded", zap.String("name", name))

	pm.publishEvent(ctx, "plugin.reloaded", map[string]interface{}{
		"name": name,
	})

	return nil
}

// GetPlugin returns a plugin by name
func (pm *PluginManager) GetPlugin(name string) (Plugin, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pluginInstance, exists := pm.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}

	return pluginInstance, nil
}

// GetPluginInfo returns plugin information
func (pm *PluginManager) GetPluginInfo(name string) (*PluginInfo, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	info, exists := pm.pluginInfo[name]
	if !exists {
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}

	return info, nil
}

// ListPlugins returns all plugins
func (pm *PluginManager) ListPlugins() map[string]*PluginInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]*PluginInfo, len(pm.pluginInfo))
	for k, v := range pm.pluginInfo {
		result[k] = v
	}
	return result
}

// ListPluginsByType returns plugins of a specific type
func (pm *PluginManager) ListPluginsByType(pluginType string) []Plugin {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make([]Plugin, 0)
	for _, p := range pm.plugins {
		if p.Type() == pluginType {
			result = append(result, p)
		}
	}
	return result
}

// HealthCheck checks the health of all plugins
func (pm *PluginManager) HealthCheck(ctx context.Context) map[string]error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	results := make(map[string]error)

	for name, pluginInstance := range pm.plugins {
		if err := pluginInstance.HealthCheck(ctx); err != nil {
			results[name] = err
		}
	}

	return results
}

// StartAll starts all loaded plugins
func (pm *PluginManager) StartAll(ctx context.Context) error {
	pm.mu.RLock()
	plugins := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		plugins = append(plugins, name)
	}
	pm.mu.RUnlock()

	for _, name := range plugins {
		if err := pm.StartPlugin(ctx, name); err != nil {
			pm.logger.Error("Failed to start plugin",
				zap.String("name", name),
				zap.Error(err))
		}
	}

	return nil
}

// StopAll stops all running plugins
func (pm *PluginManager) StopAll(ctx context.Context) error {
	pm.mu.RLock()
	plugins := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		plugins = append(plugins, name)
	}
	pm.mu.RUnlock()

	for _, name := range plugins {
		if err := pm.StopPlugin(ctx, name); err != nil {
			pm.logger.Error("Failed to stop plugin",
				zap.String("name", name),
				zap.Error(err))
		}
	}

	return nil
}

// UnloadAll unloads all plugins
func (pm *PluginManager) UnloadAll(ctx context.Context) error {
	pm.mu.RLock()
	plugins := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		plugins = append(plugins, name)
	}
	pm.mu.RUnlock()

	for _, name := range plugins {
		if err := pm.UnloadPlugin(ctx, name); err != nil {
			pm.logger.Error("Failed to unload plugin",
				zap.String("name", name),
				zap.Error(err))
		}
	}

	return nil
}

// SetEventBus sets the event bus for plugin events
func (pm *PluginManager) SetEventBus(eventBus EventBus) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.eventBus = eventBus
}

// SetHotReload enables or disables hot reload
func (pm *PluginManager) SetHotReload(enabled bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.hotReload = enabled
	pm.logger.Info("Hot reload setting changed", zap.Bool("enabled", enabled))
}

// UpdatePluginConfig updates a plugin's configuration
func (pm *PluginManager) UpdatePluginConfig(name string, config map[string]interface{}) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	info, exists := pm.pluginInfo[name]
	if !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	info.Config = config

	pm.logger.Info("Plugin configuration updated", zap.String("name", name))

	return nil
}

// checkDependencies checks if all dependencies are satisfied
func (pm *PluginManager) checkDependencies(dependencies []string) error {
	for _, dep := range dependencies {
		if _, exists := pm.plugins[dep]; !exists {
			return fmt.Errorf("dependency '%s' not found", dep)
		}
	}
	return nil
}

// extractDependencies extracts dependencies from a plugin
func (pm *PluginManager) extractDependencies(pluginInstance Plugin) []string {
	metadata := pluginInstance.Metadata()
	depsVal, ok := metadata["dependencies"]
	if !ok {
		return nil
	}

	if depsStr, ok := depsVal.(string); ok {
		return []string{depsStr}
	}

	if depsSlice, ok := depsVal.([]string); ok {
		return depsSlice
	}

	if depsSlice, ok := depsVal.([]interface{}); ok {
		result := make([]string, len(depsSlice))
		for i, d := range depsSlice {
			if str, ok := d.(string); ok {
				result[i] = str
			}
		}
		return result
	}

	return nil
}

// publishEvent publishes a plugin event
func (pm *PluginManager) publishEvent(ctx context.Context, eventType string, data map[string]interface{}) {
	if pm.eventBus == nil {
		return
	}

	event := map[string]interface{}{
		"type":      eventType,
		"timestamp": time.Now(),
		"data":      data,
	}

	if err := pm.eventBus.Publish(ctx, "plugin.events", event); err != nil {
		pm.logger.Error("Failed to publish plugin event",
			zap.String("type", eventType),
			zap.Error(err))
	}
}

// BasePlugin provides a base implementation for plugins
type BasePlugin struct {
	name       string
	version    string
	pluginType string
	config     map[string]interface{}
	metadata   map[string]interface{}
	logger     *zap.Logger
}

// NewBasePlugin creates a new base plugin
func NewBasePlugin(name, version, pluginType string, logger *zap.Logger) *BasePlugin {
	return &BasePlugin{
		name:       name,
		version:    version,
		pluginType: pluginType,
		config:     make(map[string]interface{}),
		metadata:   make(map[string]interface{}),
		logger:     logger,
	}
}

// Name returns the plugin name
func (bp *BasePlugin) Name() string {
	return bp.name
}

// Version returns the plugin version
func (bp *BasePlugin) Version() string {
	return bp.version
}

// Type returns the plugin type
func (bp *BasePlugin) Type() string {
	return bp.pluginType
}

// Initialize initializes the plugin
func (bp *BasePlugin) Initialize(ctx context.Context, config map[string]interface{}) error {
	bp.config = config
	bp.logger.Info("Plugin initialized", zap.String("name", bp.name))
	return nil
}

// Start starts the plugin
func (bp *BasePlugin) Start(ctx context.Context) error {
	bp.logger.Info("Plugin started", zap.String("name", bp.name))
	return nil
}

// Stop stops the plugin
func (bp *BasePlugin) Stop(ctx context.Context) error {
	bp.logger.Info("Plugin stopped", zap.String("name", bp.name))
	return nil
}

// HealthCheck checks the plugin health
func (bp *BasePlugin) HealthCheck(ctx context.Context) error {
	return nil
}

// Config returns the plugin configuration
func (bp *BasePlugin) Config() map[string]interface{} {
	return bp.config
}

// Metadata returns the plugin metadata
func (bp *BasePlugin) Metadata() map[string]interface{} {
	return bp.metadata
}

// SetMetadata sets metadata for the plugin
func (bp *BasePlugin) SetMetadata(key string, value interface{}) {
	bp.metadata[key] = value
}

// GetConfigValue gets a configuration value
func (bp *BasePlugin) GetConfigValue(key string) (interface{}, bool) {
	val, ok := bp.config[key]
	return val, ok
}

// GetConfigValueWithDefault gets a configuration value with a default
func (bp *BasePlugin) GetConfigValueWithDefault(key string, defaultValue interface{}) interface{} {
	if val, ok := bp.config[key]; ok {
		return val
	}
	return defaultValue
}

// GetConfigString gets a string configuration value
func (bp *BasePlugin) GetConfigString(key string) (string, bool) {
	if val, ok := bp.config[key]; ok {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

// GetConfigInt gets an int configuration value
func (bp *BasePlugin) GetConfigInt(key string) (int, bool) {
	if val, ok := bp.config[key]; ok {
		if i, ok := val.(int); ok {
			return i, true
		}
		if f, ok := val.(float64); ok {
			return int(f), true
		}
	}
	return 0, false
}

// GetConfigBool gets a bool configuration value
func (bp *BasePlugin) GetConfigBool(key string) (bool, bool) {
	if val, ok := bp.config[key]; ok {
		if b, ok := val.(bool); ok {
			return b, true
		}
	}
	return false, false
}

// GetConfigDuration gets a duration configuration value
func (bp *BasePlugin) GetConfigDuration(key string) (time.Duration, bool) {
	if val, ok := bp.config[key]; ok {
		switch v := val.(type) {
		case time.Duration:
			return v, true
		case string:
			if d, err := time.ParseDuration(v); err == nil {
				return d, true
			}
		}
	}
	return 0, false
}

// GetConfigSlice gets a slice configuration value
func (bp *BasePlugin) GetConfigSlice(key string) ([]interface{}, bool) {
	if val, ok := bp.config[key]; ok {
		if slice, ok := val.([]interface{}); ok {
			return slice, true
		}
		if slice, ok := val.([]string); ok {
			result := make([]interface{}, len(slice))
			for i, s := range slice {
				result[i] = s
			}
			return result, true
		}
	}
	return nil, false
}

// ValidateConfig validates the plugin configuration
func (bp *BasePlugin) ValidateConfig() error {
	return nil
}

// Reload reloads the plugin configuration
func (bp *BasePlugin) Reload(ctx context.Context, config map[string]interface{}) error {
	bp.config = config
	bp.logger.Info("Plugin configuration reloaded", zap.String("name", bp.name))
	return nil
}

// GetPluginType extracts the plugin type from a value
func GetPluginType(v interface{}) string {
	if v == nil {
		return "unknown"
	}

	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	return t.Name()
}

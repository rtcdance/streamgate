# StreamGate Plugin Development Guide

## Table of Contents

1. [Introduction](#introduction)
2. [Plugin Architecture](#plugin-architecture)
3. [Getting Started](#getting-started)
4. [Plugin Types](#plugin-types)
5. [Creating a Plugin](#creating-a-plugin)
6. [Plugin Lifecycle](#plugin-lifecycle)
7. [Plugin Configuration](#plugin-configuration)
8. [Plugin Communication](#plugin-communication)
9. [Testing Plugins](#testing-plugins)
10. [Best Practices](#best-practices)
11. [Examples](#examples)

## Introduction

StreamGate uses a microkernel plugin architecture that allows developers to extend functionality without modifying the core system. Plugins are self-contained modules that can be loaded, unloaded, and managed at runtime.

### Key Benefits

- **Modularity**: Each plugin is a self-contained module with a specific purpose
- **Hot Reload**: Plugins can be loaded and unloaded without restarting the system
- **Isolation**: Plugins operate independently with their own configuration and state
- **Extensibility**: New features can be added by creating new plugins
- **Maintainability**: Individual plugins can be updated without affecting others

## Plugin Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────────┐
│                     Plugin Manager                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │  Loader      │  │  Registry    │  │  Lifecycle   │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                      Plugin Interface                        │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Initialize(ctx, config) error                        │  │
│  │  Start(ctx) error                                     │  │
│  │  Stop(ctx) error                                      │  │
│  │  Cleanup(ctx) error                                   │  │
│  │  HealthCheck(ctx) error                               │  │
│  │  Name() string                                        │  │
│  │  Version() string                                     │  │
│  │  Type() PluginType                                    │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                      Plugin Implementations                  │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│  │  Cache   │ │  Auth    │ │  Upload  │ │  Stream  │      │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘      │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│  │  Trans   │ │  Meta    │ │  Worker  │ │  Search  │      │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘      │
└─────────────────────────────────────────────────────────────┘
```

### Plugin Manager

The Plugin Manager is responsible for:
- Loading and unloading plugins
- Managing plugin lifecycle
- Providing plugin discovery
- Handling plugin dependencies
- Monitoring plugin health

## Getting Started

### Prerequisites

- Go 1.21 or higher
- StreamGate source code
- Basic understanding of Go interfaces and concurrency

### Project Structure

```
streamgate/
├── pkg/
│   └── plugin/
│       ├── manager.go          # Plugin manager
│       ├── plugin.go           # Plugin interface
│       └── base.go             # Base plugin implementation
├── pkg/
│   └── plugins/
│       ├── cache/              # Cache plugin
│       ├── auth/               # Auth plugin
│       ├── upload/             # Upload plugin
│       └── ...                 # Other plugins
└── examples/
    └── plugins/
        └── custom/             # Custom plugin examples
```

## Plugin Types

StreamGate supports several plugin types:

### 1. Cache Plugin

**Purpose**: Manages content caching and retrieval

**Key Functions**:
- Store and retrieve cached content
- Manage cache eviction policies
- Track cache statistics

**Example Use Cases**:
- Redis-based caching
- Memory-based caching
- Distributed caching

### 2. Auth Plugin

**Purpose**: Handles authentication and authorization

**Key Functions**:
- Generate and verify authentication challenges
- Validate JWT tokens
- Manage user sessions

**Example Use Cases**:
- Wallet signature verification
- OAuth integration
- Custom authentication providers

### 3. Upload Plugin

**Purpose**: Manages file uploads

**Key Functions**:
- Handle chunked uploads
- Support resumable uploads
- Validate file types and sizes

**Example Use Cases**:
- Direct-to-storage uploads
- Chunked upload with progress tracking
- Virus scanning integration

### 4. Transcoder Plugin

**Purpose**: Handles video transcoding

**Key Functions**:
- Convert video formats
- Generate multiple quality levels
- Create HLS/DASH segments

**Example Use Cases**:
- FFmpeg-based transcoding
- Cloud transcoding services
- Hardware-accelerated transcoding

### 5. Streaming Plugin

**Purpose**: Manages content streaming

**Key Functions**:
- Serve HLS playlists
- Serve DASH manifests
- Handle range requests

**Example Use Cases**:
- HTTP-based streaming
- CDN integration
- Adaptive bitrate streaming

### 6. Metadata Plugin

**Purpose**: Manages content metadata

**Key Functions**:
- Store and retrieve metadata
- Search metadata
- Update metadata

**Example Use Cases**:
- Database-based metadata storage
- Elasticsearch integration
- Metadata enrichment

### 7. Worker Plugin

**Purpose**: Handles background jobs

**Key Functions**:
- Queue and process jobs
- Manage job priorities
- Track job status

**Example Use Cases**:
- Video processing jobs
- Notification jobs
- Cleanup jobs

### 8. Search Plugin

**Purpose**: Provides search functionality

**Key Functions**:
- Index content
- Execute search queries
- Return search results

**Example Use Cases**:
- Full-text search
- Metadata search
- Faceted search

## Creating a Plugin

### Step 1: Define Your Plugin

Create a new directory for your plugin under `pkg/plugins/`:

```bash
mkdir -p pkg/plugins/myplugin
cd pkg/plugins/myplugin
```

### Step 2: Implement the Plugin Interface

Create `plugin.go`:

```go
package myplugin

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/streamgate/pkg/plugin"
)

type MyPlugin struct {
	plugin.BasePlugin
	config Config
	logger *zap.Logger
}

type Config struct {
	Enabled bool   `json:"enabled"`
	Setting string `json:"setting"`
}

func NewPlugin(logger *zap.Logger) *MyPlugin {
	return &MyPlugin{
		BasePlugin: plugin.NewBasePlugin("myplugin", "1.0.0", plugin.TypeCustom, logger),
		logger:     logger,
	}
}

func (p *MyPlugin) Initialize(ctx context.Context, config map[string]interface{}) error {
	p.logger.Info("Initializing MyPlugin")

	if err := p.BasePlugin.Initialize(ctx, config); err != nil {
		return fmt.Errorf("failed to initialize base plugin: %w", err)
	}

	if err := p.parseConfig(config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	if !p.config.Enabled {
		p.logger.Info("MyPlugin is disabled")
		return nil
	}

	p.logger.Info("MyPlugin initialized successfully")
	return nil
}

func (p *MyPlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting MyPlugin")

	if !p.config.Enabled {
		return nil
	}

	if err := p.BasePlugin.Start(ctx); err != nil {
		return fmt.Errorf("failed to start base plugin: %w", err)
	}

	p.logger.Info("MyPlugin started successfully")
	return nil
}

func (p *MyPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping MyPlugin")

	if !p.config.Enabled {
		return nil
	}

	if err := p.BasePlugin.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop base plugin: %w", err)
	}

	p.logger.Info("MyPlugin stopped successfully")
	return nil
}

func (p *MyPlugin) Cleanup(ctx context.Context) error {
	p.logger.Info("Cleaning up MyPlugin")

	if err := p.BasePlugin.Cleanup(ctx); err != nil {
		return fmt.Errorf("failed to cleanup base plugin: %w", err)
	}

	p.logger.Info("MyPlugin cleaned up successfully")
	return nil
}

func (p *MyPlugin) HealthCheck(ctx context.Context) error {
	if !p.config.Enabled {
		return nil
	}

	return p.BasePlugin.HealthCheck(ctx)
}

func (p *MyPlugin) parseConfig(config map[string]interface{}) error {
	if enabled, ok := config["enabled"].(bool); ok {
		p.config.Enabled = enabled
	} else {
		p.config.Enabled = true
	}

	if setting, ok := config["setting"].(string); ok {
		p.config.Setting = setting
	} else {
		p.config.Setting = "default"
	}

	return nil
}

func (p *MyPlugin) DoSomething(ctx context.Context, input string) (string, error) {
	p.logger.Debug("Doing something", zap.String("input", input))

	result := fmt.Sprintf("Processed: %s with setting: %s", input, p.config.Setting)
	return result, nil
}
```

### Step 3: Register Your Plugin

Create `register.go`:

```go
package myplugin

import (
	"go.uber.org/zap"

	"github.com/streamgate/pkg/plugin"
)

func Register(logger *zap.Logger) plugin.Plugin {
	return NewPlugin(logger)
}
```

### Step 4: Add Plugin to Manager

Update the plugin manager to load your plugin:

```go
import (
	"github.com/streamgate/pkg/plugins/myplugin"
)

func (m *Manager) loadPlugins() error {
	plugins := []struct {
		name   string
		config map[string]interface{}
		factory func(*zap.Logger) plugin.Plugin
	}{
		{
			name:   "myplugin",
			config: m.config.Plugins["myplugin"],
			factory: myplugin.Register,
		},
	}

	for _, p := range plugins {
		plugin := p.factory(m.logger)
		if err := m.LoadPlugin(context.Background(), p.name, p.config); err != nil {
			return fmt.Errorf("failed to load plugin %s: %w", p.name, err)
		}
	}

	return nil
}
```

## Plugin Lifecycle

### States

1. **Unloaded**: Plugin is not loaded
2. **Loading**: Plugin is being loaded
3. **Loaded**: Plugin is loaded but not started
4. **Starting**: Plugin is starting
5. **Running**: Plugin is running and ready
6. **Stopping**: Plugin is stopping
7. **Stopped**: Plugin is stopped
8. **Unloading**: Plugin is being unloaded

### Lifecycle Methods

```
┌─────────┐
│Unloaded │
└────┬────┘
     │ LoadPlugin()
     ▼
┌─────────┐
│ Loading │
└────┬────┘
     │ Initialize()
     ▼
┌─────────┐
│ Loaded  │
└────┬────┘
     │ StartPlugin()
     ▼
┌─────────┐
│Starting │
└────┬────┘
     │ Start()
     ▼
┌─────────┐
│ Running │◄─────────────┐
└────┬────┘              │
     │ StopPlugin()      │ HealthCheck()
     ▼                   │
┌─────────┐              │
│Stopping │              │
└────┬────┘              │
     │ Stop()            │
     ▼                   │
┌─────────┐              │
│ Stopped │              │
└────┬────┘              │
     │ UnloadPlugin()    │
     ▼                   │
┌─────────┐              │
│Unloading│              │
└────┬────┘              │
     │ Cleanup()         │
     ▼                   │
┌─────────┐──────────────┘
│Unloaded │
└─────────┘
```

## Plugin Configuration

### Configuration File

Plugins can be configured through the main configuration file:

```yaml
plugins:
  myplugin:
    enabled: true
    setting: "custom-value"
```

### Environment Variables

Plugins can also use environment variables:

```go
func (p *MyPlugin) parseConfig(config map[string]interface{}) error {
	if setting := os.Getenv("MYPLUGIN_SETTING"); setting != "" {
		p.config.Setting = setting
	}
	return nil
}
```

### Dynamic Configuration

Plugins can receive configuration updates at runtime:

```go
func (p *MyPlugin) UpdateConfig(newConfig map[string]interface{}) error {
	p.logger.Info("Updating configuration")

	oldConfig := p.config
	if err := p.parseConfig(newConfig); err != nil {
		p.config = oldConfig
		return err
	}

	p.logger.Info("Configuration updated successfully")
	return nil
}
```

## Plugin Communication

### Direct Plugin Access

Plugins can access other plugins through the manager:

```go
func (p *MyPlugin) UseCachePlugin(ctx context.Context, key string) (interface{}, error) {
	cachePlugin, err := p.GetPlugin("cache")
	if err != nil {
		return nil, err
	}

	cache, ok := cachePlugin.(*cache.CachePlugin)
	if !ok {
		return nil, fmt.Errorf("cache plugin is not of expected type")
	}

	return cache.Get(ctx, key)
}
```

### Event-Based Communication

Plugins can communicate through the event bus:

```go
func (p *MyPlugin) PublishEvent(ctx context.Context, eventType string, data map[string]interface{}) error {
	event := plugin.Event{
		Type:   eventType,
		Source: p.Name(),
		Data:   data,
	}

	return p.eventBus.Publish(ctx, "myplugin.events", event)
}

func (p *MyPlugin) SubscribeToEvents(ctx context.Context) error {
	handler := func(ctx context.Context, event plugin.Event) error {
		p.logger.Info("Received event",
			zap.String("type", event.Type),
			zap.String("source", event.Source),
		)
		return p.handleEvent(ctx, event)
	}

	_, err := p.eventBus.Subscribe(ctx, "myplugin.events", handler)
	return err
}
```

## Testing Plugins

### Unit Tests

Create `plugin_test.go`:

```go
package myplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMyPlugin_Initialize(t *testing.T) {
	logger := zap.NewNop()
	plugin := NewPlugin(logger)

	ctx := context.Background()
	config := map[string]interface{}{
		"enabled": true,
		"setting": "test-value",
	}

	err := plugin.Initialize(ctx, config)
	require.NoError(t, err)
	assert.Equal(t, "test-value", plugin.config.Setting)
}

func TestMyPlugin_StartStop(t *testing.T) {
	logger := zap.NewNop()
	plugin := NewPlugin(logger)

	ctx := context.Background()
	config := map[string]interface{}{
		"enabled": true,
	}

	err := plugin.Initialize(ctx, config)
	require.NoError(t, err)

	err = plugin.Start(ctx)
	require.NoError(t, err)

	err = plugin.Stop(ctx)
	require.NoError(t, err)
}

func TestMyPlugin_DoSomething(t *testing.T) {
	logger := zap.NewNop()
	plugin := NewPlugin(logger)

	ctx := context.Background()
	config := map[string]interface{}{
		"enabled": true,
		"setting": "test-setting",
	}

	err := plugin.Initialize(ctx, config)
	require.NoError(t, err)

	result, err := plugin.DoSomething(ctx, "test-input")
	require.NoError(t, err)
	assert.Contains(t, result, "test-input")
	assert.Contains(t, result, "test-setting")
}
```

### Integration Tests

Create `integration_test.go`:

```go
package myplugin_test

import (
	"context"
	"testing"

	"github.com/streamgate/pkg/plugin"
	"github.com/streamgate/pkg/plugins/myplugin"
	"go.uber.org/zap"
)

func TestMyPluginIntegration(t *testing.T) {
	logger := zap.NewNop()
	manager := plugin.NewManager(logger)

	ctx := context.Background()
	config := map[string]interface{}{
		"enabled": true,
		"setting": "integration-test",
	}

	err := manager.LoadPlugin(ctx, "myplugin", config)
	require.NoError(t, err)

	err = manager.StartPlugin(ctx, "myplugin")
	require.NoError(t, err)

	plugin, err := manager.GetPlugin("myplugin")
	require.NoError(t, err)

	myPlugin, ok := plugin.(*myplugin.MyPlugin)
	require.True(t, ok)

	result, err := myPlugin.DoSomething(ctx, "integration-input")
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	err = manager.StopPlugin(ctx, "myplugin")
	require.NoError(t, err)

	err = manager.UnloadPlugin(ctx, "myplugin")
	require.NoError(t, err)
}
```

## Best Practices

### 1. Error Handling

Always wrap errors with context:

```go
if err != nil {
	return fmt.Errorf("failed to do something: %w", err)
}
```

### 2. Logging

Use structured logging:

```go
p.logger.Info("Processing request",
	zap.String("requestId", requestId),
	zap.String("userId", userId),
	zap.Duration("duration", duration),
)
```

### 3. Context Usage

Always respect context cancellation:

```go
select {
case <-ctx.Done():
	return ctx.Err()
case result := <-resultChan:
	return result, nil
}
```

### 4. Resource Cleanup

Always clean up resources:

```go
func (p *MyPlugin) Cleanup(ctx context.Context) error {
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			return fmt.Errorf("failed to close connection: %w", err)
		}
	}
	return nil
}
```

### 5. Configuration Validation

Validate configuration on initialization:

```go
func (p *MyPlugin) parseConfig(config map[string]interface{}) error {
	if setting, ok := config["setting"].(string); ok {
		if setting == "" {
			return fmt.Errorf("setting cannot be empty")
		}
		p.config.Setting = setting
	}
	return nil
}
```

### 6. Health Checks

Implement meaningful health checks:

```go
func (p *MyPlugin) HealthCheck(ctx context.Context) error {
	if p.conn == nil {
		return fmt.Errorf("connection not initialized")
	}

	if err := p.conn.Ping(ctx); err != nil {
		return fmt.Errorf("connection unhealthy: %w", err)
	}

	return nil
}
```

### 7. Concurrency Safety

Use mutexes for shared state:

```go
type MyPlugin struct {
	mu sync.RWMutex
	data map[string]string
}

func (p *MyPlugin) GetData(key string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	val, ok := p.data[key]
	return val, ok
}

func (p *MyPlugin) SetData(key, value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data[key] = value
}
```

### 8. Graceful Shutdown

Handle shutdown signals:

```go
func (p *MyPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping MyPlugin")

	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := p.gracefulShutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	return p.BasePlugin.Stop(ctx)
}
```

## Examples

### Example 1: Simple Cache Plugin

```go
package simplecache

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/streamgate/pkg/plugin"
)

type SimpleCachePlugin struct {
	plugin.BasePlugin
	data    map[string]cacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
	logger  *zap.Logger
}

type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

func NewPlugin(logger *zap.Logger) *SimpleCachePlugin {
	return &SimpleCachePlugin{
		BasePlugin: plugin.NewBasePlugin("simplecache", "1.0.0", plugin.TypeCache, logger),
		data:       make(map[string]cacheEntry),
		ttl:        5 * time.Minute,
		logger:     logger,
	}
}

func (p *SimpleCachePlugin) Get(ctx context.Context, key string) (interface{}, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	entry, ok := p.data[key]
	if !ok {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.value, true
}

func (p *SimpleCachePlugin) Set(ctx context.Context, key string, value interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.data[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(p.ttl),
	}

	return nil
}

func (p *SimpleCachePlugin) Delete(ctx context.Context, key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.data, key)
	return nil
}
```

### Example 2: Auth Plugin with Challenge-Response

```go
package simpleauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/streamgate/pkg/plugin"
)

type SimpleAuthPlugin struct {
	plugin.BasePlugin
	challenges map[string]challenge
	mu         sync.RWMutex
	logger     *zap.Logger
}

type challenge struct {
	value     string
	expiresAt time.Time
}

func NewPlugin(logger *zap.Logger) *SimpleAuthPlugin {
	return &SimpleAuthPlugin{
		BasePlugin: plugin.NewBasePlugin("simpleauth", "1.0.0", plugin.TypeAuth, logger),
		challenges: make(map[string]challenge),
		logger:     logger,
	}
}

func (p *SimpleAuthPlugin) GenerateChallenge(ctx context.Context, address string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate challenge: %w", err)
	}

	challengeValue := hex.EncodeToString(bytes)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.challenges[address] = challenge{
		value:     challengeValue,
		expiresAt: time.Now().Add(5 * time.Minute),
	}

	return challengeValue, nil
}

func (p *SimpleAuthPlugin) VerifySignature(ctx context.Context, address, challenge, signature string) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ch, ok := p.challenges[address]
	if !ok {
		return false, fmt.Errorf("challenge not found")
	}

	if time.Now().After(ch.expiresAt) {
		return false, fmt.Errorf("challenge expired")
	}

	if ch.value != challenge {
		return false, fmt.Errorf("challenge mismatch")
	}

	return true, nil
}
```

## Conclusion

This guide provides a comprehensive overview of developing plugins for StreamGate. For more examples and advanced topics, refer to the existing plugin implementations in the `pkg/plugins/` directory.

### Additional Resources

- [Plugin Interface Documentation](../../pkg/plugin/plugin.go)
- [Base Plugin Implementation](../../pkg/plugin/base.go)
- [Plugin Manager Documentation](../../pkg/plugin/manager.go)
- [Example Plugins](../../examples/plugins/)

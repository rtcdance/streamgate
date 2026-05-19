package core

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"streamgate/pkg/core/config"
	"streamgate/pkg/core/event"
	"streamgate/pkg/service"

	"go.uber.org/zap"
)

// topoSort returns plugin names in dependency order (Kahn's algorithm).
// plugins that depend on others appear after their dependencies.
func topoSort(plugins map[string]Plugin, deps map[string][]string) ([]string, error) {
	inDegree := make(map[string]int, len(plugins))
	graph := make(map[string][]string, len(plugins)) // dep → dependents

	for name := range plugins {
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}
		for _, dep := range deps[name] {
			if dep == name {
				continue // skip self-dependency
			}
			graph[dep] = append(graph[dep], name)
			inDegree[name]++
		}
	}

	queue := make([]string, 0, len(plugins))
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	result := make([]string, 0, len(plugins))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)
		for _, dependent := range graph[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(result) != len(plugins) {
		return nil, fmt.Errorf("circular dependency detected among plugins: %d of %d resolved", len(result), len(plugins))
	}
	return result, nil
}

// Plugin defines the interface for all plugins
type Plugin interface {
	Name() string
	Version() string
	Init(ctx context.Context, kernel *Microkernel) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health(ctx context.Context) error
	// DependsOn returns the names of plugins that must be initialized
	// and started before this plugin. Returning nil or an empty slice
	// means no dependencies. The kernel uses topological sort to
	// determine Init and Start ordering; Stop uses the reverse order.
	DependsOn() []string
}

// Microkernel is the core of the system
type Microkernel struct {
	config      *config.Config
	logger      *zap.Logger
	plugins     map[string]Plugin // name → plugin (fast lookup)
	pluginOrder []string          // topological order for Init/Start/Stop
	eventBus    event.EventBus
	registry    service.ServiceRegistry
	clientPool  *service.ClientPool
	mu          sync.RWMutex
	started     bool
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewMicrokernel creates a new microkernel instance
func NewMicrokernel(cfg *config.Config, logger *zap.Logger) (*Microkernel, error) {
	ctx, cancel := context.WithCancel(context.Background())

	if logger == nil {
		var err error
		logger, err = zap.NewDevelopment()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to initialize logger: %w", err)
		}
	}

	// Initialize event bus based on mode
	var eventBus event.EventBus
	var err error

	if cfg.Mode == "monolith" || cfg.Mode == "monolithic" {
		eventBus, err = event.NewMemoryEventBus()
	} else {
		eventBus, err = event.NewNATSEventBus(cfg.NATS.URL, logger)
		if err != nil {
			logger.Warn("NATS unavailable, falling back to in-memory event bus (inter-service events will be lost)",
				zap.String("nats_url", cfg.NATS.URL),
				zap.Error(err))
			eventBus, err = event.NewMemoryEventBus()
		}
	}

	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize event bus: %w", err)
	}

	// Initialize service registry for microservice mode
	var registry service.ServiceRegistry
	if cfg.Mode == "microservice" {
		registry, err = service.NewConsulRegistry(cfg, logger)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to initialize service registry: %w", err)
		}
	}

	// Initialize client pool for service-to-service communication
	var clientPool *service.ClientPool
	if registry != nil {
		clientPool = service.NewClientPool(registry, logger)
	}

	return &Microkernel{
		config:     cfg,
		logger:     logger,
		plugins:    make(map[string]Plugin),
		eventBus:   eventBus,
		registry:   registry,
		clientPool: clientPool,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// RegisterPlugin registers a plugin with the microkernel
func (m *Microkernel) RegisterPlugin(plugin Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot register plugin %q after microkernel has started", plugin.Name())
	}

	if _, exists := m.plugins[plugin.Name()]; exists {
		return fmt.Errorf("plugin %s already registered", plugin.Name())
	}

	m.plugins[plugin.Name()] = plugin
	m.logger.Info("Plugin registered",
		zap.String("name", plugin.Name()),
		zap.String("version", plugin.Version()))

	return nil
}

// GetPlugin retrieves a plugin by name
func (m *Microkernel) GetPlugin(name string) (Plugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", name)
	}

	return plugin, nil
}

// GetEventBus returns the event bus
func (m *Microkernel) GetEventBus() event.EventBus {
	return m.eventBus
}

// GetRegistry returns the service registry
func (m *Microkernel) GetRegistry() service.ServiceRegistry {
	return m.registry
}

// GetClientPool returns the client pool
func (m *Microkernel) GetClientPool() *service.ClientPool {
	return m.clientPool
}

// GetConfig returns the configuration
func (m *Microkernel) GetConfig() *config.Config {
	return m.config
}

// GetLogger returns the logger
func (m *Microkernel) GetLogger() *zap.Logger {
	return m.logger
}

// Start starts the microkernel and all plugins
func (m *Microkernel) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return fmt.Errorf("microkernel already started")
	}
	m.started = true
	m.mu.Unlock()

	m.logger.Info("Starting microkernel", zap.String("mode", m.config.Mode))

	// Compute topological order for plugin init/start/stop
	m.mu.RLock()
	deps := make(map[string][]string, len(m.plugins))
	for name, plugin := range m.plugins {
		deps[name] = plugin.DependsOn()
	}
	pluginNames := make(map[string]bool, len(m.plugins))
	for name := range m.plugins {
		pluginNames[name] = true
	}
	m.mu.RUnlock()

	order, err := topoSort(m.plugins, deps)
	if err != nil {
		return fmt.Errorf("plugin dependency sort failed: %w", err)
	}

	for name, pluginDeps := range deps {
		for _, dep := range pluginDeps {
			if !pluginNames[dep] {
				return fmt.Errorf("plugin %q depends on %q which is not registered", name, dep)
			}
		}
	}

	m.mu.Lock()
	m.pluginOrder = order
	m.mu.Unlock()

	// Register service with Consul if in microservice mode
	if m.registry != nil && m.config.Mode == "microservice" {
		serviceID := fmt.Sprintf("%s-%d", m.config.ServiceName, m.config.Server.Port)
		address := os.Getenv("SERVICE_HOST")
		if address == "" {
			address, _ = os.Hostname()
		}
		if address == "" {
			address = "localhost"
		}
		serviceInfo := &service.ServiceInfo{
			ID:      serviceID,
			Name:    m.config.ServiceName,
			Address: address,
			Port:    m.config.Server.Port,
			Tags:    []string{"v1", "microservice"},
			Metadata: map[string]string{
				"version": m.config.Version,
				"mode":    "microservice",
			},
			Check: &service.HealthCheck{
				HTTP:     fmt.Sprintf("http://%s:%d/health", address, m.config.Server.Port),
				Interval: "10s",
				Timeout:  "5s",
			},
		}

		if err := m.registry.Register(ctx, serviceInfo); err != nil {
			m.logger.Error("Failed to register service", zap.Error(err))
			return fmt.Errorf("failed to register service: %w", err)
		}

		m.logger.Info("Service registered with Consul", zap.String("service_id", serviceID))
	}

	// Initialize all plugins in dependency order
	m.mu.RLock()
	orderedPlugins := make([]Plugin, 0, len(m.pluginOrder))
	for _, name := range m.pluginOrder {
		if p, ok := m.plugins[name]; ok {
			orderedPlugins = append(orderedPlugins, p)
		}
	}
	m.mu.RUnlock()

	var initialized []Plugin
	for _, plugin := range orderedPlugins {
		if err := plugin.Init(ctx, m); err != nil {
			m.logger.Error("Failed to initialize plugin",
				zap.String("name", plugin.Name()),
				zap.Error(err))
			for i := len(initialized) - 1; i >= 0; i-- {
				if stopErr := initialized[i].Stop(ctx); stopErr != nil {
					m.logger.Error("Error stopping plugin during rollback",
						zap.String("name", initialized[i].Name()),
						zap.Error(stopErr))
				}
			}
			return fmt.Errorf("failed to initialize plugin %s: %w", plugin.Name(), err)
		}
		initialized = append(initialized, plugin)
	}

	var started []Plugin
	for _, plugin := range orderedPlugins {
		if err := plugin.Start(ctx); err != nil {
			m.logger.Error("Failed to start plugin",
				zap.String("name", plugin.Name()),
				zap.Error(err))
			for i := len(started) - 1; i >= 0; i-- {
				if stopErr := started[i].Stop(ctx); stopErr != nil {
					m.logger.Error("Error stopping plugin during rollback",
						zap.String("name", started[i].Name()),
						zap.Error(stopErr))
				}
			}
			return fmt.Errorf("failed to start plugin %s: %w", plugin.Name(), err)
		}
		started = append(started, plugin)
		m.logger.Info("Plugin started", zap.String("name", plugin.Name()))
	}

	m.logger.Info("Microkernel started successfully")
	return nil
}

// closeWithContext runs closeFn in a goroutine, respecting ctx cancellation.
// This prevents Shutdown from hanging indefinitely on unresponsive resources.
func closeWithContext(ctx context.Context, name string, closeFn func() error, logger *zap.Logger) {
	done := make(chan struct{}, 1)
	go func() {
		if err := closeFn(); err != nil {
			logger.Error("Error during "+name, zap.Error(err))
		}
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logger.Warn("Shutdown timed out for "+name, zap.Error(ctx.Err()))
	}
}

// Shutdown gracefully shuts down the microkernel and all plugins
func (m *Microkernel) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return fmt.Errorf("microkernel not started")
	}
	m.started = false
	m.mu.Unlock()

	m.logger.Info("Shutting down microkernel")

	// Stop all plugins in reverse dependency order
	m.mu.RLock()
	orderedPlugins := make([]Plugin, 0, len(m.pluginOrder))
	for _, name := range m.pluginOrder {
		if p, ok := m.plugins[name]; ok {
			orderedPlugins = append(orderedPlugins, p)
		}
	}
	m.mu.RUnlock()

	for i := len(orderedPlugins) - 1; i >= 0; i-- {
		plugin := orderedPlugins[i]
		done := make(chan struct{}, 1)
		go func(p Plugin) {
			if err := p.Stop(ctx); err != nil {
				m.logger.Error("Error stopping plugin",
					zap.String("name", p.Name()),
					zap.Error(err))
			} else {
				m.logger.Info("Plugin stopped", zap.String("name", p.Name()))
			}
			done <- struct{}{}
		}(plugin)
		select {
		case <-done:
		case <-ctx.Done():
			m.logger.Warn("Plugin stop timed out",
				zap.String("name", plugin.Name()),
				zap.Error(ctx.Err()))
		}
	}

	// Deregister service if in microservice mode
	if m.registry != nil && m.config.Mode == "microservice" {
		serviceID := fmt.Sprintf("%s-%d", m.config.ServiceName, m.config.Server.Port)
		closeWithContext(ctx, "service deregistration", func() error {
			return m.registry.Deregister(context.Background(), serviceID)
		}, m.logger)
	}

	// Close client pool with timeout protection
	closeWithContext(ctx, "client pool close", func() error {
		if m.clientPool != nil {
			return m.clientPool.Close()
		}
		return nil
	}, m.logger)

	// Close event bus with timeout protection
	closeWithContext(ctx, "event bus close", func() error {
		return m.eventBus.Close()
	}, m.logger)

	m.cancel()
	m.logger.Info("Microkernel shutdown complete")
	return nil
}

// Health checks the health of the microkernel and all plugins.
// Each plugin health check has a 5-second timeout. If a plugin
// health check hangs, the remaining plugins are still checked.
func (m *Microkernel) Health(ctx context.Context) error {
	m.mu.RLock()
	plugins := make([]Plugin, 0, len(m.plugins))
	for _, plugin := range m.plugins {
		plugins = append(plugins, plugin)
	}
	m.mu.RUnlock()

	var firstErr error
	for _, plugin := range plugins {
		func(p Plugin) {
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			done := make(chan error, 1)
			go func() {
				done <- p.Health(checkCtx)
			}()

			select {
			case err := <-done:
				if err != nil {
					m.logger.Error("Plugin health check failed",
						zap.String("name", p.Name()),
						zap.Error(err))
					if firstErr == nil {
						firstErr = fmt.Errorf("plugin %s health check failed: %w", p.Name(), err)
					}
				}
			case <-checkCtx.Done():
				m.logger.Warn("Plugin health check timed out",
					zap.String("name", p.Name()))
				if firstErr == nil {
					firstErr = fmt.Errorf("plugin %s health check timed out", p.Name())
				}
			}
		}(plugin)
	}

	return firstErr
}

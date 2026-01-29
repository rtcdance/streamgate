package core

import (
	"context"
	"fmt"
	"sync"

	"streamgate/pkg/core/config"
	"streamgate/pkg/core/event"
	"streamgate/pkg/service"
	"go.uber.org/zap"
)

// Plugin defines the interface for all plugins
type Plugin interface {
	Name() string
	Version() string
	Init(ctx context.Context, kernel *Microkernel) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health(ctx context.Context) error
}

// Microkernel is the core of the system
type Microkernel struct {
	config       *config.Config
	logger       *zap.Logger
	plugins      map[string]Plugin
	eventBus     event.EventBus
	registry     service.ServiceRegistry
	clientPool   *service.ClientPool
	mu           sync.RWMutex
	started      bool
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewMicrokernel creates a new microkernel instance
func NewMicrokernel(cfg *config.Config, logger *zap.Logger) (*Microkernel, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize event bus based on mode
	var eventBus event.EventBus
	var err error

	if cfg.Mode == "monolithic" {
		eventBus, err = event.NewMemoryEventBus()
	} else {
		eventBus, err = event.NewNATSEventBus(cfg.NATS.URL, logger)
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

	if _, exists := m.plugins[plugin.Name()]; exists {
		return fmt.Errorf("plugin %s already registered", plugin.Name())
	}

	m.plugins[plugin.Name()] = plugin
	m.logger.Info("Plugin registered", "name", plugin.Name(), "version", plugin.Version())

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

	m.logger.Info("Starting microkernel", "mode", m.config.Mode)

	// Register service with Consul if in microservice mode
	if m.registry != nil && m.config.Mode == "microservice" {
		serviceID := fmt.Sprintf("%s-%s", m.config.ServiceName, m.config.Server.Port)
		serviceInfo := &service.ServiceInfo{
			ID:      serviceID,
			Name:    m.config.ServiceName,
			Address: "localhost", // TODO: Get from config or environment
			Port:    m.config.Server.Port,
			Tags:    []string{"v1", "microservice"},
			Metadata: map[string]string{
				"version": "1.0.0",
				"mode":    "microservice",
			},
			Check: &service.HealthCheck{
				HTTP:     fmt.Sprintf("http://localhost:%d/health", m.config.Server.Port),
				Interval: "10s",
				Timeout:  "5s",
			},
		}

		if err := m.registry.Register(ctx, serviceInfo); err != nil {
			m.logger.Error("Failed to register service", "error", err)
			return fmt.Errorf("failed to register service: %w", err)
		}

		m.logger.Info("Service registered with Consul", "service_id", serviceID)
	}

	// Initialize all plugins
	m.mu.RLock()
	plugins := make([]Plugin, 0, len(m.plugins))
	for _, plugin := range m.plugins {
		plugins = append(plugins, plugin)
	}
	m.mu.RUnlock()

	for _, plugin := range plugins {
		if err := plugin.Init(ctx, m); err != nil {
			m.logger.Error("Failed to initialize plugin", "name", plugin.Name(), "error", err)
			return fmt.Errorf("failed to initialize plugin %s: %w", plugin.Name(), err)
		}
	}

	// Start all plugins
	for _, plugin := range plugins {
		if err := plugin.Start(ctx); err != nil {
			m.logger.Error("Failed to start plugin", "name", plugin.Name(), "error", err)
			return fmt.Errorf("failed to start plugin %s: %w", plugin.Name(), err)
		}
		m.logger.Info("Plugin started", "name", plugin.Name())
	}

	m.logger.Info("Microkernel started successfully")
	return nil
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

	// Stop all plugins in reverse order
	m.mu.RLock()
	plugins := make([]Plugin, 0, len(m.plugins))
	for _, plugin := range m.plugins {
		plugins = append(plugins, plugin)
	}
	m.mu.RUnlock()

	// Reverse order for shutdown
	for i := len(plugins) - 1; i >= 0; i-- {
		plugin := plugins[i]
		if err := plugin.Stop(ctx); err != nil {
			m.logger.Error("Error stopping plugin", "name", plugin.Name(), "error", err)
		} else {
			m.logger.Info("Plugin stopped", "name", plugin.Name())
		}
	}

	// Deregister service if in microservice mode
	if m.registry != nil && m.config.Mode == "microservice" {
		serviceID := fmt.Sprintf("%s-%s", m.config.ServiceName, m.config.Server.Port)
		if err := m.registry.Deregister(ctx, serviceID); err != nil {
			m.logger.Error("Error deregistering service", "error", err)
		}
	}

	// Close client pool
	if m.clientPool != nil {
		if err := m.clientPool.Close(); err != nil {
			m.logger.Error("Error closing client pool", "error", err)
		}
	}

	// Close event bus
	if err := m.eventBus.Close(); err != nil {
		m.logger.Error("Error closing event bus", "error", err)
	}

	m.cancel()
	m.logger.Info("Microkernel shutdown complete")
	return nil
}

// Health checks the health of the microkernel and all plugins
func (m *Microkernel) Health(ctx context.Context) error {
	m.mu.RLock()
	plugins := make([]Plugin, 0, len(m.plugins))
	for _, plugin := range m.plugins {
		plugins = append(plugins, plugin)
	}
	m.mu.RUnlock()

	for _, plugin := range plugins {
		if err := plugin.Health(ctx); err != nil {
			m.logger.Error("Plugin health check failed", "name", plugin.Name(), "error", err)
			return fmt.Errorf("plugin %s health check failed: %w", plugin.Name(), err)
		}
	}

	return nil
}

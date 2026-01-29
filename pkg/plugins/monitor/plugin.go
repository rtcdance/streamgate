package monitor

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

// MonitorPlugin is the monitoring service plugin
type MonitorPlugin struct {
	name   string
	kernel *core.Microkernel
	logger *zap.Logger
	config *config.Config
	server *MonitorServer
}

// NewMonitorPlugin creates a new monitor plugin
func NewMonitorPlugin(cfg *config.Config, logger *zap.Logger) *MonitorPlugin {
	return &MonitorPlugin{
		name:   "monitor",
		logger: logger,
		config: cfg,
	}
}

// Name returns the plugin name
func (p *MonitorPlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *MonitorPlugin) Version() string {
	return "1.0.0"
}

// Init initializes the plugin
func (p *MonitorPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing monitor plugin")

	// Initialize monitor server
	var err error
	p.server, err = NewMonitorServer(p.config, p.logger, kernel)
	if err != nil {
		return fmt.Errorf("failed to create monitor server: %w", err)
	}

	return nil
}

// Start starts the monitor service
func (p *MonitorPlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting monitor service", zap.Int("port", p.config.Server.Port))

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start monitor server: %w", err)
	}

	p.logger.Info("Monitor service started successfully", zap.Int("port", p.config.Server.Port))
	return nil
}

// Stop stops the monitor service
func (p *MonitorPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping monitor service")

	if p.server != nil {
		if err := p.server.Stop(ctx); err != nil {
			p.logger.Error("Error stopping monitor server", zap.Error(err))
			return err
		}
	}

	p.logger.Info("Monitor service stopped")
	return nil
}

// Health checks the health of the monitor service
func (p *MonitorPlugin) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("monitor service not started")
	}

	return p.server.Health(ctx)
}

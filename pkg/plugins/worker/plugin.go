package worker

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

// WorkerPlugin is the worker service plugin
type WorkerPlugin struct {
	name   string
	kernel *core.Microkernel
	logger *zap.Logger
	config *config.Config
	server *WorkerServer
}

// NewWorkerPlugin creates a new worker plugin
func NewWorkerPlugin(cfg *config.Config, logger *zap.Logger) *WorkerPlugin {
	return &WorkerPlugin{
		name:   "worker",
		logger: logger,
		config: cfg,
	}
}

// Name returns the plugin name
func (p *WorkerPlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *WorkerPlugin) Version() string {
	return "1.0.0"
}

// Init initializes the plugin
func (p *WorkerPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing worker plugin")

	// Initialize worker server
	var err error
	p.server, err = NewWorkerServer(p.config, p.logger, kernel)
	if err != nil {
		return fmt.Errorf("failed to create worker server: %w", err)
	}

	return nil
}

// Start starts the worker service
func (p *WorkerPlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting worker service", zap.Int("port", p.config.Server.Port))

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start worker server: %w", err)
	}

	p.logger.Info("Worker service started successfully", zap.Int("port", p.config.Server.Port))
	return nil
}

// Stop stops the worker service
func (p *WorkerPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping worker service")

	if p.server != nil {
		if err := p.server.Stop(ctx); err != nil {
			p.logger.Error("Error stopping worker server", zap.Error(err))
			return err
		}
	}

	p.logger.Info("Worker service stopped")
	return nil
}

// Health checks the health of the worker service
func (p *WorkerPlugin) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("worker service not started")
	}

	return p.server.Health(ctx)
}

package streaming

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

// StreamingPlugin is the streaming service plugin
type StreamingPlugin struct {
	name   string
	kernel *core.Microkernel
	logger *zap.Logger
	config *config.Config
	server *StreamingServer
}

// NewStreamingPlugin creates a new streaming plugin
func NewStreamingPlugin(cfg *config.Config, logger *zap.Logger) *StreamingPlugin {
	return &StreamingPlugin{
		name:   "streaming",
		logger: logger,
		config: cfg,
	}
}

// Name returns the plugin name
func (p *StreamingPlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *StreamingPlugin) Version() string {
	return "1.0.0"
}

// Init initializes the plugin
func (p *StreamingPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing streaming plugin")

	// Initialize streaming server
	var err error
	p.server, err = NewStreamingServer(p.config, p.logger, kernel)
	if err != nil {
		return fmt.Errorf("failed to create streaming server: %w", err)
	}

	return nil
}

// Start starts the streaming service
func (p *StreamingPlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting streaming service", zap.Int("port", p.config.Server.Port))

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start streaming server: %w", err)
	}

	p.logger.Info("Streaming service started successfully", zap.Int("port", p.config.Server.Port))
	return nil
}

// Stop stops the streaming service
func (p *StreamingPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping streaming service")

	if p.server != nil {
		if err := p.server.Stop(ctx); err != nil {
			p.logger.Error("Error stopping streaming server", zap.Error(err))
			return err
		}
	}

	p.logger.Info("Streaming service stopped")
	return nil
}

// Health checks the health of the streaming service
func (p *StreamingPlugin) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("streaming service not started")
	}

	return p.server.Health(ctx)
}

package transcoder

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

// TranscoderPluginWrapper is the transcoder service plugin wrapper
type TranscoderPluginWrapper struct {
	name   string
	kernel *core.Microkernel
	logger *zap.Logger
	config *config.Config
	server *TranscoderServer
}

// NewTranscoderPluginWrapper creates a new transcoder plugin wrapper
func NewTranscoderPluginWrapper(cfg *config.Config, logger *zap.Logger) *TranscoderPluginWrapper {
	return &TranscoderPluginWrapper{
		name:   "transcoder",
		logger: logger,
		config: cfg,
	}
}

// Name returns the plugin name
func (p *TranscoderPluginWrapper) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *TranscoderPluginWrapper) Version() string {
	return "1.0.0"
}

// Init initializes the plugin
func (p *TranscoderPluginWrapper) Init(ctx context.Context, kernel *core.Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing transcoder plugin")

	// Initialize transcoder server
	var err error
	p.server, err = NewTranscoderServer(p.config, p.logger, kernel)
	if err != nil {
		return fmt.Errorf("failed to create transcoder server: %w", err)
	}

	return nil
}

// Start starts the transcoder service
func (p *TranscoderPluginWrapper) Start(ctx context.Context) error {
	p.logger.Info("Starting transcoder service", zap.Int("port", p.config.Server.Port))

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start transcoder server: %w", err)
	}

	p.logger.Info("Transcoder service started successfully", zap.Int("port", p.config.Server.Port))
	return nil
}

// Stop stops the transcoder service
func (p *TranscoderPluginWrapper) Stop(ctx context.Context) error {
	p.logger.Info("Stopping transcoder service")

	if p.server != nil {
		if err := p.server.Stop(ctx); err != nil {
			p.logger.Error("Error stopping transcoder server", zap.Error(err))
			return err
		}
	}

	p.logger.Info("Transcoder service stopped")
	return nil
}

// Health checks the health of the transcoder service
func (p *TranscoderPluginWrapper) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("transcoder service not started")
	}

	return p.server.Health(ctx)
}

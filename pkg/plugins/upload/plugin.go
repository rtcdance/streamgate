package upload

import (
	"context"
	"fmt"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"go.uber.org/zap"
)

// UploadPlugin is the upload service plugin
type UploadPlugin struct {
	name   string
	kernel *core.Microkernel
	logger *zap.Logger
	config *config.Config
	server *UploadServer
}

// NewUploadPlugin creates a new upload plugin
func NewUploadPlugin(cfg *config.Config, logger *zap.Logger) *UploadPlugin {
	return &UploadPlugin{
		name:   "upload",
		logger: logger,
		config: cfg,
	}
}

// Name returns the plugin name
func (p *UploadPlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *UploadPlugin) Version() string {
	return "1.0.0"
}

// Init initializes the plugin
func (p *UploadPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing upload plugin")

	// Initialize upload server
	var err error
	p.server, err = NewUploadServer(p.config, p.logger, kernel)
	if err != nil {
		return fmt.Errorf("failed to create upload server: %w", err)
	}

	return nil
}

// Start starts the upload service
func (p *UploadPlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting upload service", "port", p.config.Server.Port)

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start upload server: %w", err)
	}

	p.logger.Info("Upload service started successfully", "port", p.config.Server.Port)
	return nil
}

// Stop stops the upload service
func (p *UploadPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping upload service")

	if p.server != nil {
		if err := p.server.Stop(ctx); err != nil {
			p.logger.Error("Error stopping upload server", "error", err)
			return err
		}
	}

	p.logger.Info("Upload service stopped")
	return nil
}

// Health checks the health of the upload service
func (p *UploadPlugin) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("upload service not started")
	}

	return p.server.Health(ctx)
}

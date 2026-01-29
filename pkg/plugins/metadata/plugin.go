package metadata

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

// MetadataPlugin is the metadata service plugin
type MetadataPlugin struct {
	name   string
	kernel *core.Microkernel
	logger *zap.Logger
	config *config.Config
	server *MetadataServer
}

// NewMetadataPlugin creates a new metadata plugin
func NewMetadataPlugin(cfg *config.Config, logger *zap.Logger) *MetadataPlugin {
	return &MetadataPlugin{
		name:   "metadata",
		logger: logger,
		config: cfg,
	}
}

// Name returns the plugin name
func (p *MetadataPlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *MetadataPlugin) Version() string {
	return "1.0.0"
}

// Init initializes the plugin
func (p *MetadataPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing metadata plugin")

	// Initialize metadata server
	var err error
	p.server, err = NewMetadataServer(p.config, p.logger, kernel)
	if err != nil {
		return fmt.Errorf("failed to create metadata server: %w", err)
	}

	return nil
}

// Start starts the metadata service
func (p *MetadataPlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting metadata service", "port", p.config.Server.Port)

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start metadata server: %w", err)
	}

	p.logger.Info("Metadata service started successfully", "port", p.config.Server.Port)
	return nil
}

// Stop stops the metadata service
func (p *MetadataPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping metadata service")

	if p.server != nil {
		if err := p.server.Stop(ctx); err != nil {
			p.logger.Error("Error stopping metadata server", "error", err)
			return err
		}
	}

	p.logger.Info("Metadata service stopped")
	return nil
}

// Health checks the health of the metadata service
func (p *MetadataPlugin) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("metadata service not started")
	}

	return p.server.Health(ctx)
}

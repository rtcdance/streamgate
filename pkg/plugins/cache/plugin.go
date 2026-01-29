package cache

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

// CachePlugin is the cache service plugin
type CachePlugin struct {
	name   string
	kernel *core.Microkernel
	logger *zap.Logger
	config *config.Config
	server *CacheServer
}

// NewCachePlugin creates a new cache plugin
func NewCachePlugin(cfg *config.Config, logger *zap.Logger) *CachePlugin {
	return &CachePlugin{
		name:   "cache",
		logger: logger,
		config: cfg,
	}
}

// Name returns the plugin name
func (p *CachePlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *CachePlugin) Version() string {
	return "1.0.0"
}

// Init initializes the plugin
func (p *CachePlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing cache plugin")

	// Initialize cache server
	var err error
	p.server, err = NewCacheServer(p.config, p.logger, kernel)
	if err != nil {
		return fmt.Errorf("failed to create cache server: %w", err)
	}

	return nil
}

// Start starts the cache service
func (p *CachePlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting cache service", "port", p.config.Server.Port)

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start cache server: %w", err)
	}

	p.logger.Info("Cache service started successfully", "port", p.config.Server.Port)
	return nil
}

// Stop stops the cache service
func (p *CachePlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping cache service")

	if p.server != nil {
		if err := p.server.Stop(ctx); err != nil {
			p.logger.Error("Error stopping cache server", "error", err)
			return err
		}
	}

	p.logger.Info("Cache service stopped")
	return nil
}

// Health checks the health of the cache service
func (p *CachePlugin) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("cache service not started")
	}

	return p.server.Health(ctx)
}

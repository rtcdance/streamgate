package auth

import (
	"context"
	"fmt"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"go.uber.org/zap"
)

// AuthPlugin is the authentication service plugin
type AuthPlugin struct {
	name   string
	kernel *core.Microkernel
	logger *zap.Logger
	config *config.Config
	server *AuthServer
}

// NewAuthPlugin creates a new auth plugin
func NewAuthPlugin(cfg *config.Config, logger *zap.Logger) *AuthPlugin {
	return &AuthPlugin{
		name:   "auth",
		logger: logger,
		config: cfg,
	}
}

// Name returns the plugin name
func (p *AuthPlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *AuthPlugin) Version() string {
	return "1.0.0"
}

// Init initializes the plugin
func (p *AuthPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing auth plugin")

	// Initialize auth server
	var err error
	p.server, err = NewAuthServer(p.config, p.logger, kernel)
	if err != nil {
		return fmt.Errorf("failed to create auth server: %w", err)
	}

	return nil
}

// Start starts the auth service
func (p *AuthPlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting auth service", "port", p.config.Server.Port)

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start auth server: %w", err)
	}

	p.logger.Info("Auth service started successfully", "port", p.config.Server.Port)
	return nil
}

// Stop stops the auth service
func (p *AuthPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping auth service")

	if p.server != nil {
		if err := p.server.Stop(ctx); err != nil {
			p.logger.Error("Error stopping auth server", "error", err)
			return err
		}
	}

	p.logger.Info("Auth service stopped")
	return nil
}

// Health checks the health of the auth service
func (p *AuthPlugin) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("auth service not started")
	}

	return p.server.Health(ctx)
}

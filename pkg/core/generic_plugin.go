package core

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"streamgate/pkg/core/config"
)

// ServerLifecycle is the interface that plugin servers must implement.
type ServerLifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health(ctx context.Context) error
}

// GenericPlugin is a reusable plugin wrapper that eliminates boilerplate.
// Each plugin in pkg/plugins/<name>/ has a New<Name>Server(cfg, logger, kernel)
// that returns a server satisfying ServerLifecycle. GenericPlugin captures
// that creation and delegation pattern once.
type GenericPlugin struct {
	name   string
	kernel *Microkernel
	logger *zap.Logger
	config *config.Config

	// initFn creates the server instance during Init. It receives cfg and logger
	// from the closure that created the GenericPlugin and only needs the kernel.
	initFn func(kernel *Microkernel) (ServerLifecycle, error)
	server ServerLifecycle
}

// NewGenericPlugin creates a GenericPlugin. The initFn receives the microkernel
// and returns a server that satisfies ServerLifecycle.
func NewGenericPlugin(name string, cfg *config.Config, logger *zap.Logger, initFn func(*Microkernel) (ServerLifecycle, error)) *GenericPlugin {
	return &GenericPlugin{
		name:   name,
		config: cfg,
		logger: logger,
		initFn: initFn,
	}
}

func (p *GenericPlugin) Name() string {
	return p.name
}

func (p *GenericPlugin) Version() string {
	return "1.0.0"
}

func (p *GenericPlugin) Init(ctx context.Context, kernel *Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing " + p.name + " plugin")
	server, err := p.initFn(kernel)
	if err != nil {
		return fmt.Errorf("failed to create %s server: %w", p.name, err)
	}
	p.server = server
	return nil
}

func (p *GenericPlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting "+p.name+" service", zap.Int("port", p.config.Server.Port))
	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start %s server: %w", p.name, err)
	}
	return nil
}

func (p *GenericPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping " + p.name + " service")
	if p.server != nil {
		if err := p.server.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop %s server: %w", p.name, err)
		}
	}
	return nil
}

func (p *GenericPlugin) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("%s service not started", p.name)
	}
	return p.server.Health(ctx)
}

func (p *GenericPlugin) DependsOn() []string {
	return nil
}

package core

import (
	"context"
	"fmt"

	"github.com/rtcdance/streamgate/pkg/core/config"

	"go.uber.org/zap"
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
	name    string
	version string
	kernel  *Microkernel
	logger  *zap.Logger
	config  *config.Config
	deps    []string

	initFn func(kernel *Microkernel) (ServerLifecycle, error)
	server ServerLifecycle
}

type GenericPluginOption func(*GenericPlugin)

func WithVersion(version string) GenericPluginOption {
	return func(p *GenericPlugin) {
		p.version = version
	}
}

func NewGenericPlugin(name string, cfg *config.Config, logger *zap.Logger, initFn func(*Microkernel) (ServerLifecycle, error), opts ...GenericPluginOption) *GenericPlugin {
	p := &GenericPlugin{
		name:    name,
		version: "1.0.0",
		config:  cfg,
		logger:  logger,
		initFn:  initFn,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func NewGenericPluginWithDeps(name string, cfg *config.Config, logger *zap.Logger, deps []string, initFn func(*Microkernel) (ServerLifecycle, error), opts ...GenericPluginOption) *GenericPlugin {
	p := &GenericPlugin{
		name:    name,
		version: "1.0.0",
		config:  cfg,
		logger:  logger,
		deps:    deps,
		initFn:  initFn,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *GenericPlugin) Name() string {
	return p.name
}

func (p *GenericPlugin) Version() string {
	return p.version
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
	if p.config.Mode == "monolith" && p.name != "api-gateway" {
		p.logger.Info("Skipping HTTP server in monolith mode (routes served by api-gateway)", zap.String("plugin", p.name))
		return nil
	}
	p.logger.Info("Starting "+p.name+" service", zap.Int("port", p.config.Server.Port))
	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start %s server: %w", p.name, err)
	}
	return nil
}

func (p *GenericPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping " + p.name + " service")
	if p.server != nil {
		if p.config.Mode == "monolith" && p.name != "api-gateway" {
			return nil
		}
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
	if p.config.Mode == "monolith" && p.name != "api-gateway" {
		return nil
	}
	return p.server.Health(ctx)
}

func (p *GenericPlugin) DependsOn() []string {
	if len(p.deps) == 0 {
		return nil
	}
	result := make([]string, len(p.deps))
	copy(result, p.deps)
	return result
}

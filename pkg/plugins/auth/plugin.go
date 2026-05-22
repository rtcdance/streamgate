package auth

import (
	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"

	"go.uber.org/zap"
)

func init() {
	core.RegisterPluginFactory("auth", NewAuthPlugin)
}

func NewAuthPlugin(cfg *config.Config, logger *zap.Logger) core.Plugin {
	return core.NewGenericPlugin("auth", cfg, logger, func(kernel *core.Microkernel) (core.ServerLifecycle, error) {
		return NewAuthServer(cfg, logger, kernel)
	})
}

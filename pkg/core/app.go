package core

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/core/lifecycle"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"go.uber.org/zap"
)

type AppConfig struct {
	HTTPServer        *http.Server
	GinEngine         *gin.Engine
	ListenAddr        string
	JWTAuthMiddleware gin.HandlerFunc
	NFTGateMiddleware gin.HandlerFunc
	Logger            *zap.Logger
}

type App struct {
	config AppConfig
	life   *lifecycle.Lifecycle
	logger *zap.Logger
}

func NewApp(cfg AppConfig) *App {
	return &App{
		config: cfg,
		life:   lifecycle.NewLifecycle(),
		logger: cfg.Logger,
	}
}

func (a *App) registerStop(ctx context.Context) error {
	if a.config.HTTPServer == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return a.config.HTTPServer.Shutdown(shutdownCtx)
}

func (a *App) registerMiddleware(ctx context.Context) error {
	if a.config.GinEngine == nil {
		return nil
	}
	if a.config.JWTAuthMiddleware != nil {
		a.config.GinEngine.Use(a.config.JWTAuthMiddleware)
	}
	if a.config.NFTGateMiddleware != nil {
		a.config.GinEngine.Use(a.config.NFTGateMiddleware)
	}
	return nil
}

func (a *App) startServer(ctx context.Context) error {
	if a.config.HTTPServer == nil {
		return nil
	}
	addr := a.config.ListenAddr
	if addr == "" {
		addr = ":8080"
	}
	a.config.HTTPServer.Addr = addr
	a.config.HTTPServer.Handler = a.config.GinEngine

	go func() {
		if err := a.config.HTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error("HTTP server error", zap.Error(err))
		}
	}()
	return nil
}

func (a *App) Start(ctx context.Context) error {
	a.logger.Info("Starting application")
	a.life.OnStop(a.registerStop)
	if err := a.registerMiddleware(ctx); err != nil {
		return err
	}
	return a.startServer(ctx)
}

func (a *App) WaitForSignal(drainTimeout time.Duration) {
	if drainTimeout <= 0 {
		drainTimeout = 30 * time.Second
	}
	GracefulShutdown(a.config.HTTPServer, a.logger, drainTimeout)
}

func WireHelper(
	verifier middleware.NFTOwnershipChecker,
	blockProver middleware.BlockProver,
	cache middleware.NFTAccessCache,
	defaultChainID int64,
	cacheTTL time.Duration,
	marketplaceURL string,
) middleware.NFTGateConfig {
	if cacheTTL == 0 {
		cacheTTL = 60 * time.Second
	}
	return middleware.NFTGateConfig{
		Verifier:       verifier,
		BlockProver:    blockProver,
		Cache:          cache,
		DefaultChainID: defaultChainID,
		CacheTTL:       cacheTTL,
		MarketplaceURL: marketplaceURL,
	}
}

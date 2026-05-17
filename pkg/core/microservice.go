package core

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core/config"
	"streamgate/pkg/core/logger"
)

// RunMicroservice starts a microservice with the given name and plugin constructor.
// It handles config loading, kernel startup, signal-based graceful shutdown, and
// error logging — eliminating the ~76-line boilerplate in every cmd/microservices/<name>/main.go.
func RunMicroservice(name string, newPlugin func(*config.Config, *zap.Logger) Plugin) {
	log := logger.NewDevelopmentLogger("streamgate-" + name)
	defer func() { _ = log.Sync() }()

	log.Info(fmt.Sprintf("Starting StreamGate %s Service...", name))

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	cfg.Mode = "microservice"
	cfg.ServiceName = name
	if err := cfg.ValidateProduction(log); err != nil {
		log.Fatal("Config validation failed", zap.Error(err))
	}
	log.Info("Configuration loaded",
		zap.String("mode", cfg.Mode),
		zap.String("service", cfg.ServiceName),
		zap.Int("port", cfg.Server.Port))

	kernel, err := NewMicrokernel(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize microkernel", zap.Error(err))
	}

	if err := kernel.RegisterPlugin(newPlugin(cfg, log)); err != nil {
		log.Fatal("Failed to register plugin", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := kernel.Start(ctx); err != nil {
		log.Fatal("Failed to start microkernel", zap.Error(err))
	}

	log.Info(fmt.Sprintf("StreamGate %s Service started successfully", name),
		zap.Int("port", cfg.Server.Port))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	log.Info("Received shutdown signal", zap.String("signal", sig.String()))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := kernel.Shutdown(shutdownCtx); err != nil {
		log.Error("Error during shutdown", zap.Error(err))
		os.Exit(1)
	}

	log.Info(fmt.Sprintf("StreamGate %s Service stopped gracefully", name))
}

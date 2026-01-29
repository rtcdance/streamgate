package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/core/logger"
	"streamgate/pkg/plugins/api"
)

func main() {
	// Initialize logger
	log := logger.NewDevelopmentLogger("streamgate-monolith")
	defer log.Sync()

	log.Info("Starting StreamGate Monolithic Mode...")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Force monolithic mode
	cfg.Mode = "monolith"
	log.Info("Configuration loaded", zap.String("mode", cfg.Mode), zap.Int("port", cfg.Server.Port))

	// Initialize microkernel
	kernel, err := core.NewMicrokernel(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize microkernel", zap.Error(err))
	}

	// Register plugins
	if err := kernel.RegisterPlugin(api.NewGatewayPlugin(cfg, log)); err != nil {
		log.Fatal("Failed to register API Gateway plugin", zap.Error(err))
	}

	// Start microkernel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := kernel.Start(ctx); err != nil {
		log.Fatal("Failed to start microkernel", zap.Error(err))
	}

	log.Info("StreamGate Monolithic Mode started successfully")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := kernel.Shutdown(shutdownCtx); err != nil {
		log.Error("Error during shutdown", zap.Error(err))
		os.Exit(1)
	}

	log.Info("StreamGate Monolithic Mode stopped gracefully")
}

package main

import (
	"go.uber.org/zap"
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/core/logger"
	"streamgate/pkg/plugins/monitor"
)

func main() {
	// Initialize logger
	log := logger.NewDevelopmentLogger("streamgate-monitor")
	defer log.Sync()

	log.Info("Starting StreamGate Monitor Service...")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Force microservice mode
	cfg.Mode = "microservice"
	cfg.ServiceName = "monitor"
	cfg.Server.Port = 9009
	log.Info("Configuration loaded",
		zap.String("mode", cfg.Mode),
		zap.String("service", cfg.ServiceName),
		zap.Int("port", cfg.Server.Port))

	// Initialize microkernel
	kernel, err := core.NewMicrokernel(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize microkernel", zap.Error(err))
	}

	// Register monitor plugin
	if err := kernel.RegisterPlugin(monitor.NewMonitorPlugin(cfg, log)); err != nil {
		log.Fatal("Failed to register monitor plugin", zap.Error(err))
	}

	// Start microkernel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := kernel.Start(ctx); err != nil {
		log.Fatal("Failed to start microkernel", zap.Error(err))
	}

	log.Info("StreamGate Monitor Service started successfully", zap.Int("port", cfg.Server.Port))

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

	log.Info("StreamGate Monitor Service stopped gracefully")
}

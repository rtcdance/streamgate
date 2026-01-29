package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/core/logger"
	"streamgate/pkg/plugins/transcoder"
)

func main() {
	// Initialize logger
	log := logger.NewDevelopmentLogger("streamgate-transcoder")
	defer log.Sync()

	log.Info("Starting StreamGate Transcoder Service...")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration", "error", err)
	}

	// Force microservice mode
	cfg.Mode = "microservice"
	cfg.ServiceName = "transcoder"
	cfg.Server.Port = 9092
	log.Info("Configuration loaded", "mode", cfg.Mode, "service", cfg.ServiceName, "port", cfg.Server.Port)

	// Initialize microkernel
	kernel, err := core.NewMicrokernel(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize microkernel", "error", err)
	}

	// Register transcoder plugin
	if err := kernel.RegisterPlugin(transcoder.NewTranscoderPluginWrapper(cfg, log)); err != nil {
		log.Fatal("Failed to register transcoder plugin", "error", err)
	}

	// Start microkernel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := kernel.Start(ctx); err != nil {
		log.Fatal("Failed to start microkernel", "error", err)
	}

	log.Info("StreamGate Transcoder Service started successfully", "port", cfg.Server.Port)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Info("Received shutdown signal", "signal", sig)

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := kernel.Shutdown(shutdownCtx); err != nil {
		log.Error("Error during shutdown", "error", err)
		os.Exit(1)
	}

	log.Info("StreamGate Transcoder Service stopped gracefully")
}

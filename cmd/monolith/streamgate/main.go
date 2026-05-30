package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/rtcdance/streamgate/migrations"
	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/core/logger"
	migrate "github.com/rtcdance/streamgate/pkg/storage/migrate"

	_ "github.com/rtcdance/streamgate/pkg/plugins/api"
	_ "github.com/rtcdance/streamgate/pkg/plugins/auth"
	_ "github.com/rtcdance/streamgate/pkg/plugins/cache"
	_ "github.com/rtcdance/streamgate/pkg/plugins/metadata"
	_ "github.com/rtcdance/streamgate/pkg/plugins/monitor"
	_ "github.com/rtcdance/streamgate/pkg/plugins/streaming"
	_ "github.com/rtcdance/streamgate/pkg/plugins/transcoder"
	_ "github.com/rtcdance/streamgate/pkg/plugins/upload"
	_ "github.com/rtcdance/streamgate/pkg/plugins/worker"
)

func main() {
	// Initialize logger
	log := logger.NewDevelopmentLogger("streamgate-monolith")
	defer func() { _ = log.Sync() }()

	log.Info("Starting StreamGate Monolithic Mode...")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Force monolithic mode
	cfg.Mode = "monolith"
	if err := cfg.ValidateProduction(log); err != nil {
		var ve *config.ValidationError
		if errors.As(err, &ve) && ve.HasCritical() {
			log.Fatal("Critical security config validation failed (cannot be bypassed)", zap.Strings("errors", ve.Critical))
		}
		if cfg.Debug {
			log.Warn("Production config validation failed (debug mode, continuing anyway)", zap.Error(err))
		} else {
			log.Fatal("Config validation failed", zap.Error(err))
		}
	}
	log.Info("Configuration loaded", zap.String("mode", cfg.Mode), zap.Int("port", cfg.Server.Port))

	dsn := cfg.Database.GetDSN()
	if dsn != "" {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			log.Warn("Failed to open DB for auto-migration", zap.Error(err))
		} else {
			runner := migrate.New(db, migrations.FS)
			if err := runner.Up(); err != nil {
				log.Warn("Auto-migration failed (continuing anyway)", zap.Error(err))
			} else {
				log.Info("Auto-migration completed")
			}
			_ = db.Close()
		}
	}

	// Initialize microkernel
	kernel, err := core.NewMicrokernel(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize microkernel", zap.Error(err))
	}

	// Register all plugins discovered via init() auto-registration
	// Each plugin package's init() calls core.RegisterPluginFactory()
	// Blank imports of plugin packages trigger their init() registration
	if err := kernel.LoadRegisteredPlugins(); err != nil {
		log.Fatal("Failed to load registered plugins", zap.Error(err))
	}
	log.Info("All registered plugins loaded",
		zap.Strings("plugins", core.RegisteredPluginNames()))

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

	core.SetDraining()
	log.Info("Drain state activated, rejecting new requests")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := kernel.Shutdown(shutdownCtx); err != nil {
		log.Error("Error during shutdown", zap.Error(err))
		os.Exit(1)
	}

	log.Info("StreamGate Monolithic Mode stopped gracefully")
}

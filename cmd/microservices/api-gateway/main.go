package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/core/logger"
	"github.com/rtcdance/streamgate/pkg/gateway"

	"go.uber.org/zap"
)

func main() {
	log := logger.NewDevelopmentLogger("streamgate-api-gateway")
	defer func() { _ = log.Sync() }()

	log.Info("Starting StreamGate API Gateway Service...")

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	cfg.Mode = "microservice"
	cfg.ServiceName = "api-gateway"
	grpcPort := cfg.GRPC.Port
	if grpcPort <= 0 {
		grpcPort = 9090
	}
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
	log.Info("Configuration loaded",
		zap.String("mode", cfg.Mode),
		zap.String("service", cfg.ServiceName),
		zap.Int("port", cfg.Server.Port))

	router, resources, err := gateway.SetupRouter(cfg, log)
	if err != nil {
		log.Fatal("Failed to setup router", zap.Error(err))
	}
	defer func() { _ = resources.Close() }()

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           router,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeout) * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	grpcAddr := fmt.Sprintf(":%d", grpcPort)
	grpcListener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatal("Failed to create gRPC listener", zap.Error(err))
	}

	grpcServices := &gateway.GRPCServices{
		AuthService:    resources.AuthService,
		Web3Service:    resources.Web3Service,
		NFTVerifier:    resources.NFTVerifier,
		StreamingSvc:   resources.StreamingSvc,
		ContentService: resources.ContentService,
		SegmentStorage: resources.SegmentStorage,
		UploadService:  resources.UploadService,
		TranscodingSvc: resources.TranscodingSvc,
	}
	grpcServer := gateway.SetupGRPCServer(context.Background(), cfg, log, grpcServices)

	go func() {
		log.Info("Starting HTTP server", zap.Int("port", cfg.Server.Port))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP server error", zap.Error(err))
		}
	}()

	go func() {
		log.Info("Starting gRPC server", zap.Int("port", grpcPort))
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Error("gRPC server error", zap.Error(err))
		}
	}()

	log.Info("StreamGate API Gateway Service started successfully",
		zap.Int("http_port", cfg.Server.Port),
		zap.Int("grpc_port", grpcPort))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	log.Info("Received shutdown signal", zap.String("signal", sig.String()))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("Error shutting down HTTP server", zap.Error(err))
	}
	grpcServer.GracefulStop()
	log.Info("StreamGate API Gateway Service stopped gracefully")
}

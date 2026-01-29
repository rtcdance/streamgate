package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"streamgate/pkg/core/config"
	"streamgate/pkg/core/logger"
	"streamgate/pkg/middleware"
)

func main() {
	// Initialize logger
	log := logger.NewDevelopmentLogger("streamgate-api-gateway")
	defer log.Sync()

	log.Info("Starting StreamGate API Gateway Service...")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Configure service
	cfg.Mode = "microservice"
	cfg.ServiceName = "api-gateway"
	cfg.Server.Port = 9090
	log.Info("Configuration loaded",
		zap.String("mode", cfg.Mode),
		zap.String("service", cfg.ServiceName),
		zap.Int("port", cfg.Server.Port))

	// Initialize HTTP router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Apply middleware
	middlewareSvc := middleware.NewService(log)
	router.Use(middlewareSvc.LoggingMiddleware())
	router.Use(middlewareSvc.RecoveryMiddleware())
	router.Use(middlewareSvc.CORSMiddleware())
	router.Use(middlewareSvc.RateLimitMiddleware())

	// Register health check routes
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "api-gateway",
			"timestamp": time.Now().Unix(),
		})
	})

	router.GET("/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ready",
			"service": "api-gateway",
		})
	})

	// Register API routes
	registerAuthRoutes(router, log)
	registerContentRoutes(router, log)
	registerNFTRoutes(router, log)
	registerStreamingRoutes(router, log)
	registerUploadRoutes(router, log)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Initialize gRPC server
	grpcListener, err := net.Listen("tcp", ":9091")
	if err != nil {
		log.Fatal("Failed to create gRPC listener", zap.Error(err))
	}

	grpcServer := grpc.NewServer()
	log.Info("gRPC server initialized", zap.Int("port", 9091))

	// Start HTTP server in goroutine
	go func() {
		log.Info("Starting HTTP server", zap.Int("port", cfg.Server.Port))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP server error", zap.Error(err))
		}
	}()

	// Start gRPC server in goroutine
	go func() {
		log.Info("Starting gRPC server", zap.Int("port", 9091))
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Error("gRPC server error", zap.Error(err))
		}
	}()

	log.Info("StreamGate API Gateway Service started successfully",
		zap.Int("http_port", cfg.Server.Port),
		zap.Int("grpc_port", 9091))

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("Error shutting down HTTP server", zap.Error(err))
	}

	// Shutdown gRPC server
	grpcServer.GracefulStop()

	log.Info("StreamGate API Gateway Service stopped gracefully")
}

// registerAuthRoutes registers authentication routes
func registerAuthRoutes(router *gin.Engine, log *zap.Logger) {
	auth := router.Group("/api/v1/auth")
	{
		auth.POST("/login", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "login endpoint"})
		})
		auth.POST("/logout", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "logout endpoint"})
		})
		auth.POST("/verify", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "verify endpoint"})
		})
		auth.GET("/profile", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "profile endpoint"})
		})
	}
	log.Info("Auth routes registered")
}

// registerContentRoutes registers content routes
func registerContentRoutes(router *gin.Engine, log *zap.Logger) {
	content := router.Group("/api/v1/content")
	{
		content.GET("", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "list content endpoint"})
		})
		content.GET("/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "get content endpoint"})
		})
		content.POST("", func(c *gin.Context) {
			c.JSON(http.StatusCreated, gin.H{"message": "create content endpoint"})
		})
		content.PUT("/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "update content endpoint"})
		})
		content.DELETE("/:id", func(c *gin.Context) {
			c.JSON(http.StatusNoContent, gin.H{"message": "delete content endpoint"})
		})
	}
	log.Info("Content routes registered")
}

// registerNFTRoutes registers NFT routes
func registerNFTRoutes(router *gin.Engine, log *zap.Logger) {
	nft := router.Group("/api/v1/nft")
	{
		nft.GET("", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "list NFT endpoint"})
		})
		nft.GET("/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "get NFT endpoint"})
		})
		nft.POST("/verify", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "verify NFT endpoint"})
		})
	}
	log.Info("NFT routes registered")
}

// registerStreamingRoutes registers streaming routes
func registerStreamingRoutes(router *gin.Engine, log *zap.Logger) {
	streaming := router.Group("/api/v1/streaming")
	{
		streaming.GET("/:id/manifest.m3u8", func(c *gin.Context) {
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.String(http.StatusOK, "#EXTM3U\n#EXT-X-VERSION:3\n")
		})
		streaming.GET("/:id/segment/:num", func(c *gin.Context) {
			c.Header("Content-Type", "video/mp2t")
			c.String(http.StatusOK, "")
		})
	}
	log.Info("Streaming routes registered")
}

// registerUploadRoutes registers upload routes
func registerUploadRoutes(router *gin.Engine, log *zap.Logger) {
	upload := router.Group("/api/v1/upload")
	{
		upload.POST("", func(c *gin.Context) {
			c.JSON(http.StatusCreated, gin.H{"message": "upload endpoint"})
		})
		upload.POST("/chunk", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "chunk upload endpoint"})
		})
		upload.GET("/:id/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "upload status endpoint"})
		})
	}
	log.Info("Upload routes registered")
}

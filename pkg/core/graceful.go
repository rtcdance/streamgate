package core

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// drainState tracks whether the server is draining (shutting down).
var drainState atomic.Bool

// IsDraining returns true when the server has started graceful shutdown.
// Handlers can use this to reject new work while allowing in-flight
// requests to complete.
func IsDraining() bool {
	return drainState.Load()
}

// DrainMiddleware returns a Gin middleware that rejects new requests
// with 503 Service Unavailable once the server starts draining.
// In-flight requests (already past the middleware) are allowed to complete.
func DrainMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsDraining() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "server is shutting down",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// GracefulShutdown blocks until a termination signal is received, then
// performs a graceful shutdown of the HTTP server with the given drain
// timeout. A second signal forces an immediate exit.
//
// The caller should invoke this in a goroutine after starting the server:
//
//	go core.GracefulShutdown(server, logger, 30*time.Second)
func GracefulShutdown(server *http.Server, logger *zap.Logger, drainTimeout time.Duration) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info("Received shutdown signal, draining connections",
		zap.String("signal", sig.String()),
		zap.Duration("drain_timeout", drainTimeout))

	// Mark draining so DrainMiddleware rejects new requests
	drainState.Store(true)

	// Second signal channel for force quit
	forceChan := make(chan os.Signal, 1)
	signal.Notify(forceChan, syscall.SIGINT, syscall.SIGTERM)

	// Begin shutdown with drain timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP server shutdown error", zap.Error(err))
		}
		close(done)
	}()

	select {
	case <-done:
		logger.Info("All connections drained, server stopped")
	case sig := <-forceChan:
		logger.Warn("Second signal received, forcing exit",
			zap.String("signal", sig.String()))
		cancel()
	case <-shutdownCtx.Done():
		logger.Warn("Drain timeout exceeded, forcing shutdown")
	}
}

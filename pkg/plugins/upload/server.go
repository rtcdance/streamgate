package upload

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"go.uber.org/zap"
)

// UploadServer handles file uploads
type UploadServer struct {
	config *config.Config
	logger *zap.Logger
	kernel *core.Microkernel
	server *http.Server
	store  *FileStore
}

// NewUploadServer creates a new upload server
func NewUploadServer(cfg *config.Config, logger *zap.Logger, kernel *core.Microkernel) (*UploadServer, error) {
	store, err := NewFileStore(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create file store: %w", err)
	}

	return &UploadServer{
		config: cfg,
		logger: logger,
		kernel: kernel,
		store:  store,
	}, nil
}

// Start starts the upload server
func (s *UploadServer) Start(ctx context.Context) error {
	handler := NewUploadHandler(s.store, s.logger, s.kernel)

	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)

	// Upload endpoints
	mux.HandleFunc("/api/v1/upload", handler.UploadHandler)
	mux.HandleFunc("/api/v1/upload/chunk", handler.UploadChunkHandler)
	mux.HandleFunc("/api/v1/upload/complete", handler.CompleteUploadHandler)
	mux.HandleFunc("/api/v1/upload/status", handler.GetUploadStatusHandler)

	// Catch-all for 404
	mux.HandleFunc("/", handler.NotFoundHandler)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Upload server error", "error", err)
		}
	}()

	return nil
}

// Stop stops the upload server
func (s *UploadServer) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down upload server", "error", err)
			return err
		}
	}

	if s.store != nil {
		if err := s.store.Close(); err != nil {
			s.logger.Error("Error closing file store", "error", err)
			return err
		}
	}

	return nil
}

// Health checks the health of the upload server
func (s *UploadServer) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("upload server not started")
	}

	if s.store == nil {
		return fmt.Errorf("file store not initialized")
	}

	return s.store.Health(ctx)
}

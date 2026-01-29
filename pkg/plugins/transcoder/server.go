package transcoder

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

// TranscoderServer handles video transcoding
type TranscoderServer struct {
	config *config.Config
	logger *zap.Logger
	kernel *core.Microkernel
	server *http.Server
	plugin *TranscoderPlugin
}

// NewTranscoderServer creates a new transcoder server
func NewTranscoderServer(cfg *config.Config, logger *zap.Logger, kernel *core.Microkernel) (*TranscoderServer, error) {
	// Initialize transcoder plugin
	scalingPolicy := &ScalingPolicy{
		MinWorkers:         2,
		MaxWorkers:         10,
		TargetQueueLen:     100,
		ScaleUpThreshold:   5.0,
		ScaleDownThreshold: 0.5,
		CheckInterval:      30 * time.Second,
	}

	transcoderConfig := &TranscoderConfig{
		WorkerPoolSize:      4,
		MaxConcurrentTasks:  100,
		MaxQueueSize:        1000,
		TaskTimeout:         30 * time.Minute,
		HealthCheckInterval: 1 * time.Minute,
		ScalingPolicy:       scalingPolicy,
	}

	plugin := NewTranscoderPlugin(transcoderConfig)

	return &TranscoderServer{
		config: cfg,
		logger: logger,
		kernel: kernel,
		plugin: plugin,
	}, nil
}

// Start starts the transcoder server
func (s *TranscoderServer) Start(ctx context.Context) error {
	// Initialize plugin
	if err := s.plugin.Init(ctx, s.kernel); err != nil {
		return fmt.Errorf("failed to initialize transcoder plugin: %w", err)
	}

	// Start plugin
	if err := s.plugin.Start(ctx); err != nil {
		return fmt.Errorf("failed to start transcoder plugin: %w", err)
	}

	handler := NewTranscoderHandler(s.plugin, s.logger, s.kernel)

	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)

	// Transcoding endpoints
	mux.HandleFunc("/api/v1/transcode/submit", handler.SubmitTaskHandler)
	mux.HandleFunc("/api/v1/transcode/status", handler.GetTaskStatusHandler)
	mux.HandleFunc("/api/v1/transcode/cancel", handler.CancelTaskHandler)
	mux.HandleFunc("/api/v1/transcode/list", handler.ListTasksHandler)
	mux.HandleFunc("/api/v1/transcode/metrics", handler.GetMetricsHandler)

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
			s.logger.Error("Transcoder server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop stops the transcoder server
func (s *TranscoderServer) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down transcoder server", zap.Error(err))
			return err
		}
	}

	if s.plugin != nil {
		if err := s.plugin.Stop(ctx); err != nil {
			s.logger.Error("Error stopping transcoder plugin", zap.Error(err))
			return err
		}
	}

	return nil
}

// Health checks the health of the transcoder server
func (s *TranscoderServer) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("transcoder server not started")
	}

	if s.plugin == nil {
		return fmt.Errorf("transcoder plugin not initialized")
	}

	return s.plugin.HealthCheck()
}

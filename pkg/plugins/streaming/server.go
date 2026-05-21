package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"

	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
)

// StreamingServer handles video streaming
type StreamingServer struct {
	config *config.Config
	logger *zap.Logger
	kernel *core.Microkernel
	server *http.Server
	cache  *StreamCache
}

// NewStreamingServer creates a new streaming server
func NewStreamingServer(cfg *config.Config, logger *zap.Logger, kernel *core.Microkernel) (*StreamingServer, error) {
	cache := NewStreamCache(logger)

	return &StreamingServer{
		config: cfg,
		logger: logger,
		kernel: kernel,
		cache:  cache,
	}, nil
}

// Start starts the streaming server
func (s *StreamingServer) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization required"})
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		tokenStr = strings.TrimSpace(tokenStr)
		if tokenStr == "" || tokenStr == authHeader {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid authorization format"})
			return
		}

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return []byte(s.config.Auth.JWTSecret), nil
		})
		if err != nil || !token.Valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
			return
		}

		wallet, _ := claims["wallet_address"].(string)
		if wallet == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet address required"})
			return
		}

		next(w, r)
	}
}

func (s *StreamingServer) Start(ctx context.Context) error {
	handler := NewStreamingHandler(s.cache, s.logger, s.kernel)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/health/live", handler.HealthHandler)
	mux.HandleFunc("/health/ready", handler.ReadyHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)

	mux.HandleFunc("/api/v1/stream/hls", s.requireAuth(handler.GetHLSPlaylistHandler))
	mux.HandleFunc("/api/v1/stream/dash", s.requireAuth(handler.GetDASHManifestHandler))
	mux.HandleFunc("/api/v1/stream/segment", s.requireAuth(handler.GetSegmentHandler))
	mux.HandleFunc("/api/v1/stream/info", s.requireAuth(handler.GetStreamInfoHandler))

	mux.HandleFunc("/", handler.NotFoundHandler)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Streaming server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop stops the streaming server
func (s *StreamingServer) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down streaming server", zap.Error(err))
			return err
		}
	}

	if s.cache != nil {
		s.cache.Close()
	}

	return nil
}

// Health checks the health of the streaming server
func (s *StreamingServer) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("streaming server not started")
	}

	return nil
}

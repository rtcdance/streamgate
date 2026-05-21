package cache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"

	"go.uber.org/zap"
)

// ErrNotFound is returned when a cache key does not exist.
var ErrNotFound = errors.New("cache key not found")

// CacheServer handles distributed caching
type CacheServer struct {
	config *config.Config
	logger *zap.Logger
	kernel *core.Microkernel
	server *http.Server
	store  *CacheStore
}

// NewCacheServer creates a new cache server
func NewCacheServer(cfg *config.Config, logger *zap.Logger, kernel *core.Microkernel) (*CacheServer, error) {
	store, err := NewCacheStore(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache store: %w", err)
	}

	return &CacheServer{
		config: cfg,
		logger: logger,
		kernel: kernel,
		store:  store,
	}, nil
}

// Start starts the cache server
func (s *CacheServer) Start(ctx context.Context) error {
	handler := NewCacheHandler(s.store, s.logger, s.kernel)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/health/live", handler.HealthHandler)
	mux.HandleFunc("/health/ready", handler.ReadyHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)

	// Cache endpoints
	mux.HandleFunc("/api/v1/cache/get", handler.GetHandler)
	mux.HandleFunc("/api/v1/cache/set", handler.SetHandler)
	mux.HandleFunc("/api/v1/cache/delete", handler.DeleteHandler)
	mux.HandleFunc("/api/v1/cache/clear", handler.ClearHandler)
	mux.HandleFunc("/api/v1/cache/stats", handler.StatsHandler)

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
			s.logger.Error("Cache server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop stops the cache server
func (s *CacheServer) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down cache server", zap.Error(err))
			return err
		}
	}

	if s.store != nil {
		if err := s.store.Close(); err != nil {
			s.logger.Error("Error closing cache store", zap.Error(err))
			return err
		}
	}

	return nil
}

// Health checks the health of the cache server
func (s *CacheServer) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("cache server not started")
	}

	if s.store == nil {
		return fmt.Errorf("cache store not initialized")
	}

	return s.store.Health(ctx)
}

// CacheStore handles cache storage operations.
// In development mode it uses the in-process LRU cache; in production
// it should be replaced with a Redis-backed implementation.
type CacheStore struct {
	config  *config.Config
	logger  *zap.Logger
	lru     *LRU
	maxSize int
}

// NewCacheStore creates a new cache store.
// When cfg.Redis is configured, a Redis backend should be used instead.
// For local development, an in-process LRU cache is created.
func NewCacheStore(cfg *config.Config, logger *zap.Logger) (*CacheStore, error) {
	logger.Info("Initializing cache store", zap.String("host", cfg.Redis.Host), zap.Int("port", cfg.Redis.Port))

	maxSize := 10000 // default LRU capacity
	store := &CacheStore{
		config:  cfg,
		logger:  logger,
		lru:     NewLRU(maxSize),
		maxSize: maxSize,
	}

	return store, nil
}

// Get retrieves a value from cache.
// Returns ErrNotFound if the key does not exist or has expired.
func (s *CacheStore) Get(ctx context.Context, key string) (interface{}, error) {
	s.logger.Debug("Getting cache value", zap.String("key", key))

	value, ok := s.lru.Get(key)
	if !ok {
		return nil, ErrNotFound
	}
	return value, nil
}

// Set stores a value in cache with an optional TTL.
func (s *CacheStore) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	s.logger.Debug("Setting cache value", zap.String("key", key), zap.Duration("ttl", ttl))

	s.lru.SetWithTTL(key, value, ttl)
	return nil
}

// Delete removes a value from cache.
func (s *CacheStore) Delete(ctx context.Context, key string) error {
	s.logger.Debug("Deleting cache value", zap.String("key", key))

	s.lru.Delete(key)
	return nil
}

// Clear clears all cache entries.
func (s *CacheStore) Clear(ctx context.Context) error {
	s.logger.Info("Clearing all cache")

	s.lru.Clear()
	return nil
}

// Stats returns cache statistics from the underlying LRU.
func (s *CacheStore) Stats(ctx context.Context) *CacheStats {
	return s.lru.GetStats()
}

// Health checks the health of the cache store.
func (s *CacheStore) Health(ctx context.Context) error {
	if s.lru == nil {
		return fmt.Errorf("cache LRU not initialized")
	}
	return nil
}

// Close closes the cache store.
func (s *CacheStore) Close() error {
	if s.lru != nil {
		s.lru.Clear()
	}
	return nil
}

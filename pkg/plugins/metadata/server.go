package metadata

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

// MetadataServer handles content metadata operations
type MetadataServer struct {
	config *config.Config
	logger *zap.Logger
	kernel *core.Microkernel
	server *http.Server
	db     *MetadataDB
}

// NewMetadataServer creates a new metadata server
func NewMetadataServer(cfg *config.Config, logger *zap.Logger, kernel *core.Microkernel) (*MetadataServer, error) {
	db, err := NewMetadataDB(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata database: %w", err)
	}

	return &MetadataServer{
		config: cfg,
		logger: logger,
		kernel: kernel,
		db:     db,
	}, nil
}

// Start starts the metadata server
func (s *MetadataServer) Start(ctx context.Context) error {
	handler := NewMetadataHandler(s.db, s.logger, s.kernel)

	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)

	// Metadata endpoints
	mux.HandleFunc("/api/v1/metadata", handler.GetMetadataHandler)
	mux.HandleFunc("/api/v1/metadata/create", handler.CreateMetadataHandler)
	mux.HandleFunc("/api/v1/metadata/update", handler.UpdateMetadataHandler)
	mux.HandleFunc("/api/v1/metadata/delete", handler.DeleteMetadataHandler)
	mux.HandleFunc("/api/v1/metadata/search", handler.SearchMetadataHandler)

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
			s.logger.Error("Metadata server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop stops the metadata server
func (s *MetadataServer) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down metadata server", zap.Error(err))
			return err
		}
	}

	if s.db != nil {
		if err := s.db.Close(); err != nil {
			s.logger.Error("Error closing metadata database", zap.Error(err))
			return err
		}
	}

	return nil
}

// Health checks the health of the metadata server
func (s *MetadataServer) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("metadata server not started")
	}

	if s.db == nil {
		return fmt.Errorf("metadata database not initialized")
	}

	return s.db.Health(ctx)
}

// MetadataDB handles database operations for metadata
type MetadataDB struct {
	config *config.Config
	logger *zap.Logger
	mu     sync.RWMutex
	data   map[string]*ContentMetadata
}

// NewMetadataDB creates a new metadata database
func NewMetadataDB(cfg *config.Config, logger *zap.Logger) (*MetadataDB, error) {
	logger.Info("Initializing metadata database", zap.String("host", cfg.Database.Host), zap.String("database", cfg.Database.Database))

	db := &MetadataDB{
		config: cfg,
		logger: logger,
		data:   make(map[string]*ContentMetadata),
	}

	return db, nil
}

func (db *MetadataDB) GetMetadata(ctx context.Context, contentID string) (*ContentMetadata, error) {
	db.logger.Info("Getting metadata", zap.String("content_id", contentID))

	db.mu.RLock()
	defer db.mu.RUnlock()

	if metadata, exists := db.data[contentID]; exists {
		return metadata, nil
	}
	return &ContentMetadata{ContentID: contentID}, nil
}

func (db *MetadataDB) CreateMetadata(ctx context.Context, metadata *ContentMetadata) error {
	db.logger.Info("Creating metadata", zap.String("content_id", metadata.ContentID))

	db.mu.Lock()
	defer db.mu.Unlock()
	db.data[metadata.ContentID] = metadata
	return nil
}

func (db *MetadataDB) UpdateMetadata(ctx context.Context, metadata *ContentMetadata) error {
	db.logger.Info("Updating metadata", zap.String("content_id", metadata.ContentID))

	db.mu.Lock()
	defer db.mu.Unlock()
	if _, exists := db.data[metadata.ContentID]; !exists {
		return fmt.Errorf("metadata not found: %s", metadata.ContentID)
	}
	db.data[metadata.ContentID] = metadata
	return nil
}

func (db *MetadataDB) DeleteMetadata(ctx context.Context, contentID string) error {
	db.logger.Info("Deleting metadata", zap.String("content_id", contentID))

	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.data, contentID)
	return nil
}

func (db *MetadataDB) SearchMetadata(ctx context.Context, query string) ([]*ContentMetadata, error) {
	db.logger.Info("Searching metadata", zap.String("query", query))

	db.mu.RLock()
	defer db.mu.RUnlock()

	results := make([]*ContentMetadata, 0, len(db.data))
	for _, metadata := range db.data {
		results = append(results, metadata)
	}
	return results, nil
}

func (db *MetadataDB) Health(ctx context.Context) error {
	return nil
}

func (db *MetadataDB) Close() error {
	return nil
}

// ContentMetadata represents content metadata
type ContentMetadata struct {
	ContentID   string `json:"content_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Duration    int    `json:"duration"` // in seconds
	FileSize    int64  `json:"file_size"`
	Format      string `json:"format"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

package upload

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core/config"
)

// uploadEntry tracks a single upload (complete or chunked) in memory.
type uploadEntry struct {
	data       []byte
	chunks     map[int][]byte // chunkIndex → data
	totalSize  int
	createdAt  time.Time
	completed  bool
	fileID     string // set after CompleteUpload
}

// FileStore handles file storage operations.
// Development mode: in-memory map with chunk tracking.
// Production: replace with MinIO/S3 backend.
type FileStore struct {
	config  *config.Config
	logger  *zap.Logger
	mu      sync.RWMutex
	entries map[string]*uploadEntry // uploadID or fileID → entry
	files   map[string][]byte       // fileID → final data
}

// NewFileStore creates a new file store
func NewFileStore(cfg *config.Config, logger *zap.Logger) (*FileStore, error) {
	logger.Info("Initializing file store", zap.String("type", cfg.Storage.Type), zap.String("endpoint", cfg.Storage.Endpoint))

	return &FileStore{
		config:  cfg,
		logger:  logger,
		entries: make(map[string]*uploadEntry),
		files:   make(map[string][]byte),
	}, nil
}

// UploadFile uploads a complete file to storage
func (s *FileStore) UploadFile(ctx context.Context, fileID string, data []byte) error {
	s.logger.Info("Uploading file", zap.String("file_id", fileID), zap.Int("size", len(data)))

	s.mu.Lock()
	defer s.mu.Unlock()

	s.files[fileID] = data
	s.entries[fileID] = &uploadEntry{
		data:      data,
		totalSize: len(data),
		createdAt: time.Now(),
		completed: true,
		fileID:    fileID,
	}
	return nil
}

// UploadChunk uploads a chunk of a file
func (s *FileStore) UploadChunk(ctx context.Context, uploadID string, chunkIndex int, data []byte) error {
	s.logger.Info("Uploading chunk", zap.String("upload_id", uploadID), zap.Int("chunk_index", chunkIndex), zap.Int("size", len(data)))

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[uploadID]
	if !ok {
		entry = &uploadEntry{
			chunks:    make(map[int][]byte),
			createdAt: time.Now(),
		}
		s.entries[uploadID] = entry
	}

	if entry.completed {
		return fmt.Errorf("upload %s already completed", uploadID)
	}

	entry.chunks[chunkIndex] = data
	entry.totalSize += len(data)
	return nil
}

// CompleteUpload completes a chunked upload by combining all chunks in order
func (s *FileStore) CompleteUpload(ctx context.Context, uploadID string) (string, error) {
	s.logger.Info("Completing upload", zap.String("upload_id", uploadID))

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[uploadID]
	if !ok {
		return "", fmt.Errorf("upload not found: %s", uploadID)
	}

	if entry.completed {
		return entry.fileID, nil
	}

	if len(entry.chunks) == 0 {
		return "", fmt.Errorf("no chunks uploaded for: %s", uploadID)
	}

	// Combine chunks in index order
	indices := make([]int, 0, len(entry.chunks))
	for idx := range entry.chunks {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	combined := make([]byte, 0, entry.totalSize)
	for _, idx := range indices {
		combined = append(combined, entry.chunks[idx]...)
	}

	fileID := uploadID // use uploadID as the final file ID
	entry.data = combined
	entry.completed = true
	entry.fileID = fileID
	entry.chunks = nil // free chunk memory

	s.files[fileID] = combined
	return fileID, nil
}

// GetUploadStatus gets the status of an upload
func (s *FileStore) GetUploadStatus(ctx context.Context, uploadID string) (*UploadStatusInfo, error) {
	s.logger.Debug("Getting upload status", zap.String("upload_id", uploadID))

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[uploadID]
	if !ok {
		return nil, fmt.Errorf("upload not found: %s", uploadID)
	}

	info := &UploadStatusInfo{
		UploadID:       uploadID,
		UploadedChunks: len(entry.chunks),
		Status:         "uploading",
	}

	if entry.completed {
		info.Status = "completed"
		info.Progress = 1.0
		info.UploadedChunks = 0 // chunks cleared after completion
	} else if len(entry.chunks) > 0 {
		info.Progress = float64(entry.totalSize) / float64(max(entry.totalSize*2, 1))
		if info.Progress > 0.99 {
			info.Progress = 0.99
		}
	}

	return info, nil
}

// DeleteUpload deletes an upload
func (s *FileStore) DeleteUpload(ctx context.Context, uploadID string) error {
	s.logger.Info("Deleting upload", zap.String("upload_id", uploadID))

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[uploadID]
	if !ok {
		return fmt.Errorf("upload not found: %s", uploadID)
	}

	if entry.fileID != "" {
		delete(s.files, entry.fileID)
	}
	delete(s.entries, uploadID)
	return nil
}

// Health checks the health of the file store
func (s *FileStore) Health(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.entries == nil {
		return fmt.Errorf("file store not initialized")
	}
	return nil
}

// Close closes the file store and releases memory
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = make(map[string]*uploadEntry)
	s.files = make(map[string][]byte)
	return nil
}

// UploadStatusInfo represents the status of an upload
type UploadStatusInfo struct {
	UploadID       string  `json:"upload_id"`
	TotalChunks    int     `json:"total_chunks"`
	UploadedChunks int     `json:"uploaded_chunks"`
	Status         string  `json:"status"` // pending, uploading, completed, failed
	Progress       float64 `json:"progress"`
	Error          string  `json:"error,omitempty"`
}

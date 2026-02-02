package upload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ResumableUploadManager handles resumable file uploads
type ResumableUploadManager struct {
	storageDir string
	chunksDir  string
	logger     *zap.Logger
	mu         sync.RWMutex
	sessions   map[string]*UploadSession
}

// UploadSession represents an active upload session
type UploadSession struct {
	ID        string
	FileName  string
	FileSize  int64
	ChunkSize int64
	Uploaded  int64
	Status    UploadStatus
	CreatedAt time.Time
	UpdatedAt time.Time
	Checksum  string
	mu        sync.Mutex
}

// UploadStatus represents the status of an upload
type UploadStatus string

const (
	UploadStatusPending   UploadStatus = "pending"
	UploadStatusUploading UploadStatus = "uploading"
	UploadStatusCompleted UploadStatus = "completed"
	UploadStatusFailed    UploadStatus = "failed"
	UploadStatusCancelled UploadStatus = "cancelled"
)

// NewResumableUploadManager creates a new resumable upload manager
func NewResumableUploadManager(storageDir string, logger *zap.Logger) (*ResumableUploadManager, error) {
	chunksDir := filepath.Join(storageDir, "chunks")

	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	if err := os.MkdirAll(chunksDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create chunks directory: %w", err)
	}

	return &ResumableUploadManager{
		storageDir: storageDir,
		chunksDir:  chunksDir,
		logger:     logger,
		sessions:   make(map[string]*UploadSession),
	}, nil
}

// StartUpload starts a new resumable upload session
func (rum *ResumableUploadManager) StartUpload(ctx context.Context, fileName string, fileSize, chunkSize int64, checksum string) (*UploadSession, error) {
	rum.logger.Debug("Starting upload session",
		zap.String("file_name", fileName),
		zap.Int64("file_size", fileSize),
		zap.Int64("chunk_size", chunkSize))

	sessionID := generateSessionID()

	session := &UploadSession{
		ID:        sessionID,
		FileName:  fileName,
		FileSize:  fileSize,
		ChunkSize: chunkSize,
		Uploaded:  0,
		Status:    UploadStatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Checksum:  checksum,
	}

	rum.mu.Lock()
	rum.sessions[sessionID] = session
	rum.mu.Unlock()

	rum.logger.Debug("Upload session started",
		zap.String("session_id", sessionID),
		zap.String("file_name", fileName))

	return session, nil
}

// UploadChunk uploads a chunk of data
func (rum *ResumableUploadManager) UploadChunk(ctx context.Context, sessionID string, chunkIndex int, data []byte) error {
	rum.logger.Debug("Uploading chunk",
		zap.String("session_id", sessionID),
		zap.Int("chunk_index", chunkIndex),
		zap.Int("chunk_size", len(data)))

	rum.mu.RLock()
	session, exists := rum.sessions[sessionID]
	rum.mu.RUnlock()

	if !exists {
		return fmt.Errorf("upload session not found: %s", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.Status == UploadStatusCompleted {
		return fmt.Errorf("upload already completed")
	}

	if session.Status == UploadStatusCancelled {
		return fmt.Errorf("upload cancelled")
	}

	// Calculate expected chunk position
	expectedOffset := int64(chunkIndex) * session.ChunkSize
	if expectedOffset >= session.FileSize {
		return fmt.Errorf("invalid chunk index: %d", chunkIndex)
	}

	// Save chunk to disk
	chunkPath := rum.getChunkPath(sessionID, chunkIndex)
	if err := os.WriteFile(chunkPath, data, 0644); err != nil {
		return fmt.Errorf("failed to save chunk: %w", err)
	}

	// Update session
	session.Status = UploadStatusUploading
	session.Uploaded = expectedOffset + int64(len(data))
	session.UpdatedAt = time.Now()

	rum.logger.Debug("Chunk uploaded",
		zap.String("session_id", sessionID),
		zap.Int("chunk_index", chunkIndex),
		zap.Int64("uploaded", session.Uploaded))

	return nil
}

// CompleteUpload completes an upload session
func (rum *ResumableUploadManager) CompleteUpload(ctx context.Context, sessionID string) (string, error) {
	rum.logger.Debug("Completing upload",
		zap.String("session_id", sessionID))

	rum.mu.RLock()
	session, exists := rum.sessions[sessionID]
	rum.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("upload session not found: %s", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.Status == UploadStatusCompleted {
		return rum.getFinalPath(sessionID), nil
	}

	// Verify all chunks are uploaded
	if session.Uploaded < session.FileSize {
		return "", fmt.Errorf("upload incomplete: %d/%d bytes uploaded", session.Uploaded, session.FileSize)
	}

	// Assemble chunks into final file
	finalPath := rum.getFinalPath(sessionID)
	if err := rum.assembleChunks(sessionID, finalPath); err != nil {
		return "", fmt.Errorf("failed to assemble chunks: %w", err)
	}

	// Verify checksum if provided
	if session.Checksum != "" {
		if err := rum.verifyChecksum(finalPath, session.Checksum); err != nil {
			os.Remove(finalPath)
			return "", fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Update session status
	session.Status = UploadStatusCompleted
	session.UpdatedAt = time.Now()

	// Clean up chunks
	go rum.cleanupChunks(sessionID)

	rum.logger.Debug("Upload completed",
		zap.String("session_id", sessionID),
		zap.String("final_path", finalPath))

	return finalPath, nil
}

// GetSession retrieves an upload session
func (rum *ResumableUploadManager) GetSession(ctx context.Context, sessionID string) (*UploadSession, error) {
	rum.mu.RLock()
	defer rum.mu.RUnlock()

	session, exists := rum.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("upload session not found: %s", sessionID)
	}

	return session, nil
}

// GetProgress gets the upload progress for a session
func (rum *ResumableUploadManager) GetProgress(ctx context.Context, sessionID string) (float64, error) {
	session, err := rum.GetSession(ctx, sessionID)
	if err != nil {
		return 0, err
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.FileSize == 0 {
		return 0, nil
	}

	progress := float64(session.Uploaded) / float64(session.FileSize) * 100
	return progress, nil
}

// GetUploadedChunks returns the list of uploaded chunk indices
func (rum *ResumableUploadManager) GetUploadedChunks(ctx context.Context, sessionID string) ([]int, error) {
	rum.mu.RLock()
	session, exists := rum.sessions[sessionID]
	rum.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("upload session not found: %s", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	// Scan chunks directory for uploaded chunks
	sessionDir := filepath.Join(rum.chunksDir, sessionID)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read chunks directory: %w", err)
	}

	chunkIndices := make([]int, 0, len(entries))
	for _, entry := range entries {
		var chunkIndex int
		if _, err := fmt.Sscanf(entry.Name(), "chunk_%d.bin", &chunkIndex); err == nil {
			chunkIndices = append(chunkIndices, chunkIndex)
		}
	}

	return chunkIndices, nil
}

// CancelUpload cancels an upload session
func (rum *ResumableUploadManager) CancelUpload(ctx context.Context, sessionID string) error {
	rum.logger.Debug("Cancelling upload",
		zap.String("session_id", sessionID))

	rum.mu.RLock()
	session, exists := rum.sessions[sessionID]
	rum.mu.RUnlock()

	if !exists {
		return fmt.Errorf("upload session not found: %s", sessionID)
	}

	session.mu.Lock()
	session.Status = UploadStatusCancelled
	session.UpdatedAt = time.Now()
	session.mu.Unlock()

	// Clean up chunks
	go rum.cleanupChunks(sessionID)

	rum.logger.Debug("Upload cancelled",
		zap.String("session_id", sessionID))

	return nil
}

// ResumeUpload resumes an upload session
func (rum *ResumableUploadManager) ResumeUpload(ctx context.Context, sessionID string) (*UploadSession, error) {
	rum.logger.Debug("Resuming upload",
		zap.String("session_id", sessionID))

	rum.mu.RLock()
	session, exists := rum.sessions[sessionID]
	rum.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("upload session not found: %s", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.Status == UploadStatusCompleted {
		return nil, fmt.Errorf("upload already completed")
	}

	if session.Status == UploadStatusCancelled {
		return nil, fmt.Errorf("upload cancelled, cannot resume")
	}

	session.Status = UploadStatusUploading
	session.UpdatedAt = time.Now()

	rum.logger.Debug("Upload resumed",
		zap.String("session_id", sessionID))

	return session, nil
}

// CleanupOldSessions removes old upload sessions
func (rum *ResumableUploadManager) CleanupOldSessions(ctx context.Context, olderThan time.Duration) error {
	rum.logger.Debug("Cleaning up old sessions",
		zap.Duration("older_than", olderThan))

	rum.mu.Lock()
	defer rum.mu.Unlock()

	now := time.Now()
	for sessionID, session := range rum.sessions {
		session.mu.Lock()
		age := now.Sub(session.UpdatedAt)
		session.mu.Unlock()

		if age > olderThan {
			rum.logger.Debug("Removing old session",
				zap.String("session_id", sessionID),
				zap.Duration("age", age))

			delete(rum.sessions, sessionID)
			rum.cleanupChunks(sessionID)
		}
	}

	return nil
}

// assembleChunks assembles chunks into the final file
func (rum *ResumableUploadManager) assembleChunks(sessionID, finalPath string) error {
	rum.mu.RLock()
	session, exists := rum.sessions[sessionID]
	rum.mu.RUnlock()

	if !exists {
		return fmt.Errorf("upload session not found: %s", sessionID)
	}

	finalFile, err := os.Create(finalPath)
	if err != nil {
		return fmt.Errorf("failed to create final file: %w", err)
	}
	defer finalFile.Close()

	// Calculate number of chunks
	numChunks := int((session.FileSize + session.ChunkSize - 1) / session.ChunkSize)

	// Assemble chunks in order
	for i := 0; i < numChunks; i++ {
		chunkPath := rum.getChunkPath(sessionID, i)
		chunkData, err := os.ReadFile(chunkPath)
		if err != nil {
			return fmt.Errorf("failed to read chunk %d: %w", i, err)
		}

		if _, err := finalFile.Write(chunkData); err != nil {
			return fmt.Errorf("failed to write chunk %d: %w", i, err)
		}
	}

	return nil
}

// verifyChecksum verifies the file checksum
func (rum *ResumableUploadManager) verifyChecksum(filePath, expectedChecksum string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	actualChecksum := hex.EncodeToString(hasher.Sum(nil))

	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}

// cleanupChunks removes chunk files for a session
func (rum *ResumableUploadManager) cleanupChunks(sessionID string) {
	sessionDir := filepath.Join(rum.chunksDir, sessionID)

	if err := os.RemoveAll(sessionDir); err != nil {
		rum.logger.Error("Failed to cleanup chunks",
			zap.String("session_id", sessionID),
			zap.Error(err))
	}
}

// getChunkPath returns the path for a chunk file
func (rum *ResumableUploadManager) getChunkPath(sessionID string, chunkIndex int) string {
	sessionDir := filepath.Join(rum.chunksDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		rum.logger.Error("Failed to create session directory",
			zap.String("session_id", sessionID),
			zap.Error(err))
	}

	return filepath.Join(sessionDir, fmt.Sprintf("chunk_%d.bin", chunkIndex))
}

// getFinalPath returns the path for the final file
func (rum *ResumableUploadManager) getFinalPath(sessionID string) string {
	rum.mu.RLock()
	session, exists := rum.sessions[sessionID]
	rum.mu.RUnlock()

	if !exists {
		return filepath.Join(rum.storageDir, sessionID)
	}

	return filepath.Join(rum.storageDir, sessionID+"_"+session.FileName)
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// UploadProgress represents upload progress information
type UploadProgress struct {
	SessionID string
	FileName  string
	FileSize  int64
	Uploaded  int64
	Progress  float64
	Status    UploadStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// GetProgressInfo returns detailed progress information
func (rum *ResumableUploadManager) GetProgressInfo(ctx context.Context, sessionID string) (*UploadProgress, error) {
	session, err := rum.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	progress, _ := rum.GetProgress(ctx, sessionID)

	return &UploadProgress{
		SessionID: session.ID,
		FileName:  session.FileName,
		FileSize:  session.FileSize,
		Uploaded:  session.Uploaded,
		Progress:  progress,
		Status:    session.Status,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}, nil
}

// ListSessions returns all active upload sessions
func (rum *ResumableUploadManager) ListSessions(ctx context.Context) []*UploadSession {
	rum.mu.RLock()
	defer rum.mu.RUnlock()

	sessions := make([]*UploadSession, 0, len(rum.sessions))
	for _, session := range rum.sessions {
		sessions = append(sessions, session)
	}

	return sessions
}

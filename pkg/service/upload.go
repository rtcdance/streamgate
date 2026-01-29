package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// UploadService handles upload operations
type UploadService struct {
	db      *sql.DB
	storage UploadObjectStorage
	bucket  string
}

// UploadObjectStorage defines the interface for object storage
type UploadObjectStorage interface {
	Upload(bucket, key string, data []byte) error
	Download(bucket, key string) ([]byte, error)
	Delete(bucket, key string) error
	Exists(bucket, key string) (bool, error)
}

// UploadInfo represents upload information
type UploadInfo struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	Hash        string    `json:"hash"`
	Status      string    `json:"status"` // pending, uploading, completed, failed
	URL         string    `json:"url"`
	OwnerID     string    `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ChunkInfo represents chunk upload information
type ChunkInfo struct {
	UploadID    string `json:"upload_id"`
	ChunkIndex  int    `json:"chunk_index"`
	TotalChunks int    `json:"total_chunks"`
	ChunkSize   int64  `json:"chunk_size"`
	Uploaded    bool   `json:"uploaded"`
}

// NewUploadService creates a new upload service
func NewUploadService(db *sql.DB, storage UploadObjectStorage, bucket string) *UploadService {
	return &UploadService{
		db:      db,
		storage: storage,
		bucket:  bucket,
	}
}

// Upload uploads file
func (s *UploadService) Upload(filename string, data []byte, ownerID string) (string, error) {
	// Generate upload ID
	uploadID := uuid.New().String()

	// Calculate file hash
	hash := calculateHash(data)

	// Generate storage key
	ext := filepath.Ext(filename)
	storageKey := fmt.Sprintf("%s/%s%s", ownerID, uploadID, ext)

	// Upload to storage
	if err := s.storage.Upload(s.bucket, storageKey, data); err != nil {
		return "", fmt.Errorf("failed to upload to storage: %w", err)
	}

	// Save upload info to database
	uploadInfo := &UploadInfo{
		ID:          uploadID,
		Filename:    filename,
		Size:        int64(len(data)),
		ContentType: detectContentType(filename),
		Hash:        hash,
		Status:      "completed",
		URL:         fmt.Sprintf("/%s/%s", s.bucket, storageKey),
		OwnerID:     ownerID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.saveUploadInfo(uploadInfo); err != nil {
		// Try to clean up storage
		s.storage.Delete(s.bucket, storageKey)
		return "", fmt.Errorf("failed to save upload info: %w", err)
	}

	return uploadID, nil
}

// GetUploadStatus gets upload status
func (s *UploadService) GetUploadStatus(uploadID string) (*UploadInfo, error) {
	query := `
		SELECT id, filename, size, content_type, hash, status, url, owner_id, created_at, updated_at
		FROM uploads
		WHERE id = $1
	`

	var info UploadInfo
	err := s.db.QueryRow(query, uploadID).Scan(
		&info.ID,
		&info.Filename,
		&info.Size,
		&info.ContentType,
		&info.Hash,
		&info.Status,
		&info.URL,
		&info.OwnerID,
		&info.CreatedAt,
		&info.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("upload not found: %s", uploadID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query upload: %w", err)
	}

	return &info, nil
}

// InitiateChunkedUpload initiates a chunked upload
func (s *UploadService) InitiateChunkedUpload(filename string, totalSize int64, totalChunks int, ownerID string) (string, error) {
	uploadID := uuid.New().String()

	uploadInfo := &UploadInfo{
		ID:          uploadID,
		Filename:    filename,
		Size:        totalSize,
		ContentType: detectContentType(filename),
		Status:      "uploading",
		OwnerID:     ownerID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.saveUploadInfo(uploadInfo); err != nil {
		return "", fmt.Errorf("failed to save upload info: %w", err)
	}

	return uploadID, nil
}

// UploadChunk uploads a single chunk
func (s *UploadService) UploadChunk(uploadID string, chunkIndex int, data []byte) error {
	// Generate chunk storage key
	storageKey := fmt.Sprintf("chunks/%s/%d", uploadID, chunkIndex)

	// Upload chunk to storage
	if err := s.storage.Upload(s.bucket, storageKey, data); err != nil {
		return fmt.Errorf("failed to upload chunk: %w", err)
	}

	// Update upload status
	if err := s.updateUploadStatus(uploadID, "uploading"); err != nil {
		return fmt.Errorf("failed to update upload status: %w", err)
	}

	return nil
}

// CompleteChunkedUpload completes a chunked upload by merging chunks
func (s *UploadService) CompleteChunkedUpload(uploadID string, totalChunks int) error {
	// Get upload info
	uploadInfo, err := s.GetUploadStatus(uploadID)
	if err != nil {
		return err
	}

	// Download and merge all chunks
	var mergedData []byte
	for i := 0; i < totalChunks; i++ {
		chunkKey := fmt.Sprintf("chunks/%s/%d", uploadID, i)
		chunkData, err := s.storage.Download(s.bucket, chunkKey)
		if err != nil {
			return fmt.Errorf("failed to download chunk %d: %w", i, err)
		}
		mergedData = append(mergedData, chunkData...)
	}

	// Calculate hash
	hash := calculateHash(mergedData)

	// Generate final storage key
	ext := filepath.Ext(uploadInfo.Filename)
	storageKey := fmt.Sprintf("%s/%s%s", uploadInfo.OwnerID, uploadID, ext)

	// Upload merged file
	if err := s.storage.Upload(s.bucket, storageKey, mergedData); err != nil {
		return fmt.Errorf("failed to upload merged file: %w", err)
	}

	// Clean up chunks
	for i := 0; i < totalChunks; i++ {
		chunkKey := fmt.Sprintf("chunks/%s/%d", uploadID, i)
		s.storage.Delete(s.bucket, chunkKey)
	}

	// Update upload info
	query := `
		UPDATE uploads
		SET status = $2, hash = $3, url = $4, updated_at = $5
		WHERE id = $1
	`
	_, err = s.db.Exec(query, uploadID, "completed", hash, fmt.Sprintf("/%s/%s", s.bucket, storageKey), time.Now())
	if err != nil {
		return fmt.Errorf("failed to update upload info: %w", err)
	}

	return nil
}

// DeleteUpload deletes an upload
func (s *UploadService) DeleteUpload(uploadID string) error {
	// Get upload info
	uploadInfo, err := s.GetUploadStatus(uploadID)
	if err != nil {
		return err
	}

	// Extract storage key from URL
	// URL format: /bucket/owner_id/upload_id.ext
	storageKey := uploadInfo.URL[len("/"+s.bucket+"/"):]

	// Delete from storage
	if err := s.storage.Delete(s.bucket, storageKey); err != nil {
		// Log error but continue with database deletion
	}

	// Delete from database
	query := "DELETE FROM uploads WHERE id = $1"
	_, err = s.db.Exec(query, uploadID)
	if err != nil {
		return fmt.Errorf("failed to delete upload: %w", err)
	}

	return nil
}

// ListUploads lists uploads for a user
func (s *UploadService) ListUploads(ownerID string, limit, offset int) ([]*UploadInfo, error) {
	query := `
		SELECT id, filename, size, content_type, hash, status, url, owner_id, created_at, updated_at
		FROM uploads
		WHERE owner_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(query, ownerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query uploads: %w", err)
	}
	defer rows.Close()

	uploads := make([]*UploadInfo, 0)
	for rows.Next() {
		var info UploadInfo
		err := rows.Scan(
			&info.ID,
			&info.Filename,
			&info.Size,
			&info.ContentType,
			&info.Hash,
			&info.Status,
			&info.URL,
			&info.OwnerID,
			&info.CreatedAt,
			&info.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan upload: %w", err)
		}
		uploads = append(uploads, &info)
	}

	return uploads, nil
}

// saveUploadInfo saves upload info to database
func (s *UploadService) saveUploadInfo(info *UploadInfo) error {
	query := `
		INSERT INTO uploads (id, filename, size, content_type, hash, status, url, owner_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := s.db.Exec(query,
		info.ID,
		info.Filename,
		info.Size,
		info.ContentType,
		info.Hash,
		info.Status,
		info.URL,
		info.OwnerID,
		info.CreatedAt,
		info.UpdatedAt,
	)

	return err
}

// updateUploadStatus updates upload status
func (s *UploadService) updateUploadStatus(uploadID, status string) error {
	query := "UPDATE uploads SET status = $2, updated_at = $3 WHERE id = $1"
	_, err := s.db.Exec(query, uploadID, status, time.Now())
	return err
}

// calculateHash calculates SHA-256 hash of data
func calculateHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// detectContentType detects content type from filename
func detectContentType(filename string) string {
	ext := filepath.Ext(filename)
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

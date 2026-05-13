package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"streamgate/pkg/storage"
)

// DefaultMaxUploadSize is the default maximum upload size (5 GB)
const DefaultMaxUploadSize int64 = 5 * 1024 * 1024 * 1024

// DefaultStorageQuotaPerWallet is the default storage quota per wallet (50 GB)
const DefaultStorageQuotaPerWallet int64 = 50 * 1024 * 1024 * 1024

// UploadService handles upload operations
type UploadService struct {
	db            storage.DB
	objStore      UploadObjectStorage
	presigner     PresignedURLer
	bucket        string
	maxUploadSize int64
	storageQuota  int64
	logger        *zap.Logger
}

// UploadObjectStorage defines the interface for object storage
type UploadObjectStorage interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
	UploadStream(ctx context.Context, bucket, key string, reader io.Reader, size int64) error
	Download(ctx context.Context, bucket, key string) ([]byte, error)
	Delete(ctx context.Context, bucket, key string) error
	Exists(ctx context.Context, bucket, key string) (bool, error)
}

// PresignedURLer generates time-limited download URLs for stored objects.
type PresignedURLer interface {
	PresignedURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)
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

// NewUploadService creates a new upload service.
// If a PresignedURLer is provided as the last optional logger, it is ignored;
// call SetPresigner separately to enable download URL generation.
func NewUploadService(db storage.DB, objStorage UploadObjectStorage, bucket string, logger ...*zap.Logger) *UploadService {
	var l *zap.Logger
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	} else {
		l = zap.NewNop()
	}
	return &UploadService{
		db:            db,
		objStore:      objStorage,
		bucket:        bucket,
		maxUploadSize: DefaultMaxUploadSize,
		storageQuota:  DefaultStorageQuotaPerWallet,
		logger:        l,
	}
}

// SetPresigner sets the presigned URL generator for download URL support.
func (s *UploadService) SetPresigner(p PresignedURLer) {
	s.presigner = p
}

// GetDownloadURL returns a presigned URL for downloading the uploaded file.
// The URL is valid for the specified expiry duration.
// If ownerID is non-empty, it checks that the upload belongs to that wallet.
func (s *UploadService) GetDownloadURL(ctx context.Context, uploadID string, expiry time.Duration, ownerID ...string) (string, error) {
	if s.presigner == nil {
		return "", fmt.Errorf("presigned URL support not configured")
	}
	info, err := s.GetUploadStatus(ctx, uploadID)
	if err != nil {
		return "", err
	}
	if len(ownerID) > 0 && ownerID[0] != "" && info.OwnerID != ownerID[0] {
		return "", fmt.Errorf("upload does not belong to this wallet")
	}
	if info.Status != "completed" && info.Status != "processed" {
		return "", fmt.Errorf("upload not completed: %s", info.Status)
	}
	storageKey := strings.TrimPrefix(info.URL, "/"+s.bucket+"/")
	return s.presigner.PresignedURL(ctx, s.bucket, storageKey, expiry)
}

// SetMaxUploadSize sets the maximum allowed upload size.
// A value of 0 means no limit.
func (s *UploadService) SetMaxUploadSize(size int64) {
	s.maxUploadSize = size
}

// SetStorageQuota sets the per-wallet storage quota.
// A value of 0 means no quota.
func (s *UploadService) SetStorageQuota(quota int64) {
	s.storageQuota = quota
}

// CheckStorageQuota returns an error if the wallet has exceeded its storage quota.
func (s *UploadService) CheckStorageQuota(ctx context.Context, ownerID string, newFileSize int64) error {
	if s.storageQuota <= 0 || s.db == nil {
		return nil
	}
	var used int64
	err := s.db.QueryRow(ctx,
		"SELECT COALESCE(SUM(size), 0) FROM uploads WHERE owner_id = $1", ownerID).Scan(&used)
	if err != nil {
		return fmt.Errorf("quota check failed: %w", err)
	}
	if used+newFileSize > s.storageQuota {
		return fmt.Errorf("storage quota exceeded: %d/%d used", used+newFileSize, s.storageQuota)
	}
	return nil
}

// Upload uploads file from a byte slice (legacy convenience wrapper).
func (s *UploadService) Upload(ctx context.Context, filename string, data []byte, ownerID string) (string, error) {
	return s.UploadStream(ctx, filename, bytesReader(data), int64(len(data)), ownerID)
}

// UploadStream uploads a file from an io.Reader without buffering the entire
// content into memory. The caller must provide the total size for storage
// metadata and the hash is computed on-the-fly using a TeeReader.
func (s *UploadService) UploadStream(ctx context.Context, filename string, reader io.Reader, size int64, ownerID string) (string, error) {
	uploadID := uuid.New().String()

	// Hash while uploading: tee the reader through SHA-256
	h := sha256.New()
	tee := io.TeeReader(reader, h)

	ext := filepath.Ext(filename)
	storageKey := fmt.Sprintf("%s/%s%s", ownerID, uploadID, ext)

	if err := s.objStore.UploadStream(ctx, s.bucket, storageKey, tee, size); err != nil {
		return "", fmt.Errorf("failed to upload to storage: %w", err)
	}

	hash := hex.EncodeToString(h.Sum(nil))

	uploadInfo := &UploadInfo{
		ID:          uploadID,
		Filename:    filename,
		Size:        size,
		ContentType: detectContentType(filename),
		Hash:        hash,
		Status:      "completed",
		URL:         fmt.Sprintf("/%s/%s", s.bucket, storageKey),
		OwnerID:     ownerID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.saveUploadInfo(ctx, uploadInfo); err != nil {
		_ = s.objStore.Delete(ctx, s.bucket, storageKey)
		return "", fmt.Errorf("failed to save upload info: %w", err)
	}

	return uploadID, nil
}

// bytesReader is a helper to create an io.Reader from []byte for the legacy
// Upload method. Defined at package level to avoid allocation in hot paths.
type bytesSliceReader struct {
	data []byte
	pos  int
}

func bytesReader(data []byte) *bytesSliceReader {
	return &bytesSliceReader{data: data}
}

func (r *bytesSliceReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// GetUploadStatus gets upload status
func (s *UploadService) GetUploadStatus(ctx context.Context, uploadID string) (*UploadInfo, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `
		SELECT id, filename, size, content_type, hash, status, url, owner_id, created_at, updated_at
		FROM uploads
		WHERE id = $1
	`

	var info UploadInfo
	var contentType, hash, status, url, ownerID sql.NullString
	err := s.db.QueryRow(ctx, query, uploadID).Scan(
		&info.ID,
		&info.Filename,
		&info.Size,
		&contentType,
		&hash,
		&status,
		&url,
		&ownerID,
		&info.CreatedAt,
		&info.UpdatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("upload not found: %s", uploadID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query upload: %w", err)
	}

	info.ContentType = contentType.String
	info.Hash = hash.String
	info.Status = status.String
	info.URL = url.String
	info.OwnerID = ownerID.String

	return &info, nil
}

// InitiateChunkedUpload initiates a chunked upload
func (s *UploadService) InitiateChunkedUpload(ctx context.Context, filename string, totalSize int64, totalChunks int, ownerID string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not available")
	}
	if s.maxUploadSize > 0 && totalSize > s.maxUploadSize {
		return "", fmt.Errorf("upload size %d exceeds maximum allowed size %d", totalSize, s.maxUploadSize)
	}

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

	if err := s.saveUploadInfo(ctx, uploadInfo); err != nil {
		return "", fmt.Errorf("failed to save upload info: %w", err)
	}

	return uploadID, nil
}

// UploadChunk uploads a single chunk from a byte slice (legacy).
func (s *UploadService) UploadChunk(ctx context.Context, uploadID string, chunkIndex int, data []byte) error {
	return s.UploadChunkStream(ctx, uploadID, chunkIndex, bytesReader(data), int64(len(data)))
}

func (s *UploadService) UploadChunkStream(ctx context.Context, uploadID string, chunkIndex int, reader io.Reader, size int64) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	storageKey := fmt.Sprintf("chunks/%s/%d", uploadID, chunkIndex)

	exists, err := s.objStore.Exists(ctx, s.bucket, storageKey)
	if err != nil {
		s.logger.Warn("chunk existence check failed", zap.String("key", storageKey), zap.Error(err))
	}
	if exists {
		return fmt.Errorf("chunk %d already uploaded for upload %s", chunkIndex, uploadID)
	}

	if err := s.objStore.UploadStream(ctx, s.bucket, storageKey, reader, size); err != nil {
		return fmt.Errorf("failed to upload chunk stream: %w", err)
	}

	if err := s.updateUploadStatus(ctx, uploadID, "uploading"); err != nil {
		return fmt.Errorf("failed to update upload status: %w", err)
	}

	return nil
}

// CompleteChunkedUpload completes a chunked upload by merging chunks
func (s *UploadService) CompleteChunkedUpload(ctx context.Context, uploadID string, totalChunks int) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	// Get upload info
	uploadInfo, err := s.GetUploadStatus(ctx, uploadID)
	if err != nil {
		return err
	}

	if uploadInfo.Status != "uploading" {
		return fmt.Errorf("upload not in uploading state: %s", uploadInfo.Status)
	}
	if s.maxUploadSize > 0 && uploadInfo.Size > s.maxUploadSize {
		return fmt.Errorf("upload size %d exceeds maximum allowed size %d", uploadInfo.Size, s.maxUploadSize)
	}

	// Generate final storage key
	ext := filepath.Ext(uploadInfo.Filename)
	storageKey := fmt.Sprintf("%s/%s%s", uploadInfo.OwnerID, uploadID, ext)

	// Stream chunks through a pipe: writer side downloads and hashes
	// each chunk sequentially, reader side uploads via UploadStream.
	// This avoids materializing the entire file in memory.
	pr, pw := io.Pipe()
	h := sha256.New()
	hashWriter := io.MultiWriter(pw, h)

	errCh := make(chan error, 1)
	go func() {
		defer func() { _ = pw.Close() }()
		for i := 0; i < totalChunks; i++ {
			chunkKey := fmt.Sprintf("chunks/%s/%d", uploadID, i)
			chunkData, err := s.objStore.Download(ctx, s.bucket, chunkKey)
			if err != nil {
				errCh <- fmt.Errorf("failed to download chunk %d: %w", i, err)
				pw.CloseWithError(err)
				return
			}
			if _, err := hashWriter.Write(chunkData); err != nil {
				errCh <- fmt.Errorf("failed to write chunk %d: %w", i, err)
				pw.CloseWithError(err)
				return
			}
		}
		errCh <- nil
	}()

	if err := s.objStore.UploadStream(ctx, s.bucket, storageKey, pr, uploadInfo.Size); err != nil {
		pw.CloseWithError(err) // unblock the goroutine writing to the pipe
		if writeErr := <-errCh; writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("failed to upload merged file: %w", err)
	}
	// Ensure goroutine completed
	if writeErr := <-errCh; writeErr != nil {
		return writeErr
	}

	hash := hex.EncodeToString(h.Sum(nil))

	// Clean up chunks
	for i := 0; i < totalChunks; i++ {
		chunkKey := fmt.Sprintf("chunks/%s/%d", uploadID, i)
		if err := s.objStore.Delete(ctx, s.bucket, chunkKey); err != nil {
			s.logger.Debug("Failed to delete chunk after merge",
				zap.String("chunk_key", chunkKey),
				zap.Error(err))
		}
	}

	// Update upload info
	query := `
		UPDATE uploads
		SET status = $2, hash = $3, url = $4, updated_at = $5
		WHERE id = $1
	`
	_, err = s.db.Exec(ctx, query, uploadID, "completed", hash, fmt.Sprintf("/%s/%s", s.bucket, storageKey), time.Now())
	if err != nil {
		return fmt.Errorf("failed to update upload info: %w", err)
	}

	return nil
}

// DeleteUpload deletes an upload
func (s *UploadService) DeleteUpload(ctx context.Context, uploadID string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	// Get upload info
	uploadInfo, err := s.GetUploadStatus(ctx, uploadID)
	if err != nil {
		return err
	}

	// Extract storage key from URL
	// URL format: /bucket/owner_id/upload_id.ext
	storageKey := uploadInfo.URL[len("/"+s.bucket+"/"):]

	// Delete from storage
	if err := s.objStore.Delete(ctx, s.bucket, storageKey); err != nil {
		s.logger.Warn("Failed to delete from storage", zap.Error(err))
	}

	// Delete from database
	_, err = s.db.Exec(ctx, "DELETE FROM uploads WHERE id = $1", uploadID)
	if err != nil {
		return fmt.Errorf("failed to delete upload: %w", err)
	}

	return nil
}

// ListUploads lists uploads for a user
func (s *UploadService) ListUploads(ctx context.Context, ownerID string, limit, offset int) ([]*UploadInfo, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `
		SELECT id, filename, size, content_type, hash, status, url, owner_id, created_at, updated_at
		FROM uploads
		WHERE owner_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(ctx, query, ownerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query uploads: %w", err)
	}
	defer func() { _ = rows.Close() }()

	uploads := make([]*UploadInfo, 0)
	for rows.Next() {
		var info UploadInfo
		var contentType, hash, status, url, ownerID sql.NullString
		err := rows.Scan(
			&info.ID,
			&info.Filename,
			&info.Size,
			&contentType,
			&hash,
			&status,
			&url,
			&ownerID,
			&info.CreatedAt,
			&info.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan upload: %w", err)
		}

		info.ContentType = contentType.String
		info.Hash = hash.String
		info.Status = status.String
		info.URL = url.String
		info.OwnerID = ownerID.String
		uploads = append(uploads, &info)
	}

	return uploads, nil
}

// saveUploadInfo saves upload info to database
func (s *UploadService) saveUploadInfo(ctx context.Context, info *UploadInfo) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	query := `
		INSERT INTO uploads (id, filename, size, content_type, hash, status, url, owner_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := s.db.Exec(ctx, query,
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
func (s *UploadService) updateUploadStatus(ctx context.Context, uploadID, status string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	query := "UPDATE uploads SET status = $2, updated_at = $3 WHERE id = $1"
	_, err := s.db.Exec(ctx, query, uploadID, status, time.Now())
	return err
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

// CompleteUploadWithTx marks an upload as completed and creates a
// corresponding content record in a single transaction. Uses the
// defer-tx.Rollback() pattern for safe cleanup.
// Returns the content ID of the newly created content record.
func (s *UploadService) CompleteUploadWithTx(ctx context.Context, uploadID string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not available")
	}

	// Get current upload info
	upload, err := s.GetUploadStatus(ctx, uploadID)
	if err != nil {
		return "", fmt.Errorf("get upload status: %w", err)
	}

	if upload.Status != "completed" {
		return "", fmt.Errorf("upload not completed: %s", upload.Status)
	}

	var contentID string
	err = s.db.InTransaction(ctx, func(tx *sql.Tx) error {
		// Update upload status to "processed"
		if _, err := tx.ExecContext(ctx, `
			UPDATE uploads SET status = $2, updated_at = $3 WHERE id = $1
		`, uploadID, "processed", time.Now()); err != nil {
			return fmt.Errorf("update upload: %w", err)
		}

		// Create content record from upload
		contentID = uuid.New().String()
		thumbnailURL := fmt.Sprintf("https://via.placeholder.com/320x180?text=%s", upload.Filename)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO contents (id, title, type, size, status, owner_id, url, thumbnail_url, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, contentID, upload.Filename, contentTypeToType(upload.ContentType), upload.Size, "pending",
			upload.OwnerID, upload.URL, thumbnailURL, time.Now(), time.Now()); err != nil {
			return fmt.Errorf("insert content: %w", err)
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	s.logger.Info("Upload completed with content record",
		zap.String("upload_id", uploadID),
		zap.String("content_id", contentID))
	return contentID, nil
}

// contentTypeToType maps a MIME content type to a content type string.
func contentTypeToType(mime string) string {
	switch {
	case len(mime) >= 5 && mime[:5] == "video":
		return "video"
	case len(mime) >= 5 && mime[:5] == "audio":
		return "audio"
	case len(mime) >= 5 && mime[:5] == "image":
		return "image"
	default:
		return "other"
	}
}

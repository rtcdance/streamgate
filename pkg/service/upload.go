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
	"strconv"
	"strings"
	"sync"
	"time"

	"streamgate/pkg/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
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
	uploadSigner  UploadPresignedURLer
	bucket        string
	maxUploadSize int64
	storageQuota  int64
	logger        *zap.Logger
	onProcessed   []PostUploadHook
	hookMu        sync.Mutex

	chunkMergeConcurrency int // parallel chunk downloads during merge
}

const defaultChunkMergeConcurrency = 5

type PostUploadHook func(ctx context.Context, uploadID, contentID, ownerID string)

func (s *UploadService) RegisterPostUploadHook(hook PostUploadHook) {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()
	s.onProcessed = append(s.onProcessed, hook)
}

type AutoTranscodeHookDeps struct {
	TranscodingSvc    *TranscodingService
	Presigner         PresignedURLer
	Bucket            string
	Profiles          []string
	PresignedURLExpiry time.Duration // zero = default 2h
}

const defaultPresignedURLExpiry = 2 * time.Hour

func (s *UploadService) RegisterAutoTranscodeHook(deps AutoTranscodeHookDeps) {
	if deps.TranscodingSvc == nil {
		return
	}
	profiles := deps.Profiles
	if len(profiles) == 0 {
		profiles = []string{"720p"}
	}
	s.RegisterPostUploadHook(func(ctx context.Context, uploadID, contentID, ownerID string) {
		upload, err := s.GetUploadStatus(ctx, uploadID)
		if err != nil {
			s.logger.Warn("Post-upload hook: failed to get upload info", zap.Error(err))
			return
		}

		inputURL := upload.URL
		if deps.Presigner != nil {
			storageKey := strings.TrimPrefix(upload.URL, "/"+deps.Bucket+"/")
			if storageKey == "" {
				storageKey = upload.URL
			}
			expiry := deps.PresignedURLExpiry
			if expiry <= 0 {
				expiry = defaultPresignedURLExpiry
			}
			presignedURL, err := deps.Presigner.PresignedURL(ctx, deps.Bucket, storageKey, expiry)
			if err != nil {
				s.logger.Warn("Post-upload hook: failed to generate presigned URL", zap.Error(err))
			} else {
				inputURL = presignedURL
			}
		}

		for _, profile := range profiles {
			if _, err := deps.TranscodingSvc.Transcode(ctx, contentID, profile, inputURL, 5, ownerID); err != nil {
				s.logger.Warn("Post-upload hook: auto-transcode failed",
					zap.String("content_id", contentID),
					zap.String("profile", profile),
					zap.Error(err))
			} else {
				s.logger.Info("Post-upload hook: auto-transcode triggered",
					zap.String("content_id", contentID),
					zap.String("profile", profile))
			}
		}
	})
}

// UploadObjectStorage defines the interface for object storage
type UploadObjectStorage interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
	UploadStream(ctx context.Context, bucket, key string, reader io.Reader, size int64) error
	Download(ctx context.Context, bucket, key string) ([]byte, error)
	DownloadStream(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, bucket, key string) error
	DeleteObjects(ctx context.Context, bucket string, keys []string) error
	Exists(ctx context.Context, bucket, key string) (bool, error)
	ListObjects(ctx context.Context, bucket, prefix string) ([]string, error)
}

// PresignedURLer generates time-limited download URLs for stored objects.
type PresignedURLer interface {
	PresignedURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)
}

type UploadPresignedURLer interface {
	PresignedUploadURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)
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

func (s *UploadService) SetPresigner(p PresignedURLer) {
	s.presigner = p
}

func (s *UploadService) SetUploadPresigner(p UploadPresignedURLer) {
	s.uploadSigner = p
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

// SetChunkMergeConcurrency sets the number of parallel chunk downloads during merge.
func (s *UploadService) SetChunkMergeConcurrency(n int) {
	if n > 0 {
		s.chunkMergeConcurrency = n
	}
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

// GetUploadProgress computes upload progress as a percentage (0-100).
// For completed/processed uploads this is always 100.
// For chunked uploads in "uploading" state it uses completed/total chunks.
func (s *UploadService) GetUploadProgress(ctx context.Context, uploadID string) (int, error) {
	info, err := s.GetUploadStatus(ctx, uploadID)
	if err != nil {
		return 0, err
	}
	if info.Status == "completed" || info.Status == "processed" {
		return 100, nil
	}
	chunks, err := s.GetChunkStatuses(ctx, uploadID)
	if err != nil || len(chunks) == 0 {
		return 0, nil
	}
	uploaded := 0
	for _, ch := range chunks {
		if ch.Uploaded {
			uploaded++
		}
	}
	if uploaded == 0 {
		return 0, nil
	}
	return int(float64(uploaded) / float64(len(chunks)) * 100), nil
}

func (s *UploadService) GetChunkStatuses(ctx context.Context, uploadID string) ([]ChunkInfo, error) {
	if s.objStore == nil {
		return nil, fmt.Errorf("storage not available")
	}
	prefix := fmt.Sprintf("chunks/%s/", uploadID)
	objs, err := s.objStore.ListObjects(ctx, s.bucket, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list chunks: %w", err)
	}
	uploadedSet := make(map[int]bool, len(objs))
	maxIndex := -1
	for _, key := range objs {
		rel := strings.TrimPrefix(key, prefix)
		idxStr := rel
		if i := strings.Index(rel, "/"); i >= 0 {
			idxStr = rel[:i]
		}
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		uploadedSet[idx] = true
		if idx > maxIndex {
			maxIndex = idx
		}
	}
	totalChunks := maxIndex + 1
	if totalChunks == 0 {
		return nil, nil
	}
	chunks := make([]ChunkInfo, totalChunks)
	for i := 0; i < totalChunks; i++ {
		chunks[i] = ChunkInfo{
			UploadID:    uploadID,
			ChunkIndex:  i,
			TotalChunks: totalChunks,
			Uploaded:    uploadedSet[i],
		}
	}
	return chunks, nil
}

// InitiateChunkedUpload initiates a chunked upload
func (s *UploadService) InitiateChunkedUpload(ctx context.Context, filename string, totalSize int64, totalChunks int, ownerID string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not available")
	}
	if s.maxUploadSize > 0 && totalSize > s.maxUploadSize {
		return "", fmt.Errorf("upload size %d exceeds maximum allowed size %d", totalSize, s.maxUploadSize)
	}
	if err := s.CheckStorageQuota(ctx, ownerID, totalSize); err != nil {
		return "", err
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

const defaultPresignedUploadExpiry = 2 * time.Hour

func (s *UploadService) InitiatePresignedUpload(ctx context.Context, filename string, size int64, contentType, ownerID string) (uploadID, presignedURL, storageKey string, err error) {
	if s.uploadSigner == nil {
		return "", "", "", fmt.Errorf("presigned upload support not configured")
	}
	if s.db == nil {
		return "", "", "", fmt.Errorf("database not available")
	}
	if s.maxUploadSize > 0 && size > s.maxUploadSize {
		return "", "", "", fmt.Errorf("upload size %d exceeds maximum allowed size %d", size, s.maxUploadSize)
	}
	if err := s.CheckStorageQuota(ctx, ownerID, size); err != nil {
		return "", "", "", err
	}

	uploadID = uuid.New().String()
	ext := filepath.Ext(filename)
	storageKey = fmt.Sprintf("%s/%s%s", ownerID, uploadID, ext)

	if contentType == "" {
		contentType = detectContentType(filename)
	}

	uploadInfo := &UploadInfo{
		ID:          uploadID,
		Filename:    filename,
		Size:        size,
		ContentType: contentType,
		Status:      "uploading",
		URL:         fmt.Sprintf("/%s/%s", s.bucket, storageKey),
		OwnerID:     ownerID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.saveUploadInfo(ctx, uploadInfo); err != nil {
		return "", "", "", fmt.Errorf("failed to save upload info: %w", err)
	}

	url, err := s.uploadSigner.PresignedUploadURL(ctx, s.bucket, storageKey, defaultPresignedUploadExpiry)
	if err != nil {
		_ = s.DeleteUpload(ctx, uploadID)
		return "", "", "", fmt.Errorf("failed to generate presigned upload URL: %w", err)
	}

	return uploadID, url, storageKey, nil
}

// UploadChunk uploads a single chunk from a byte slice (legacy).
func (s *UploadService) UploadChunk(ctx context.Context, uploadID string, chunkIndex int, data []byte, ownerID string) error {
	return s.UploadChunkStream(ctx, uploadID, chunkIndex, bytesReader(data), int64(len(data)), ownerID)
}

func (s *UploadService) UploadChunkStream(ctx context.Context, uploadID string, chunkIndex int, reader io.Reader, size int64, ownerID string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}

	info, err := s.GetUploadStatus(ctx, uploadID)
	if err != nil {
		return fmt.Errorf("upload not found: %s", uploadID)
	}
	if ownerID != "" && info.OwnerID != ownerID {
		return fmt.Errorf("upload does not belong to this wallet")
	}
	if info.Status != "uploading" {
		return fmt.Errorf("upload not in uploading state: %s", info.Status)
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

type streamDLResult struct {
	index  int
	reader io.ReadCloser
	err    error
}

// CompleteChunkedUpload completes a chunked upload by merging chunks.
// Chunks are downloaded in parallel (chunkMergeConcurrency) and streamed
// through a pipe to the object store in the correct order.
func (s *UploadService) CompleteChunkedUpload(ctx context.Context, uploadID string, totalChunks int) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
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

	ext := filepath.Ext(uploadInfo.Filename)
	storageKey := fmt.Sprintf("%s/%s%s", uploadInfo.OwnerID, uploadID, ext)

	// Cancel all inflight downloads on any error.
	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Parallel download: send each chunk stream to resultCh.
	resultCh := make(chan streamDLResult, totalChunks)
	conc := s.chunkMergeConcurrency
	if conc <= 0 {
		conc = defaultChunkMergeConcurrency
	}
	sem := make(chan struct{}, conc)
	var dlWg sync.WaitGroup
	for i := 0; i < totalChunks; i++ {
		dlWg.Add(1)
		go func(idx int) {
			defer dlWg.Done()
			select {
			case sem <- struct{}{}:
			case <-dlCtx.Done():
				return
			}
			defer func() { <-sem }()

			chunkKey := fmt.Sprintf("chunks/%s/%d", uploadID, idx)
			reader, dlErr := s.objStore.DownloadStream(dlCtx, s.bucket, chunkKey)
			resultCh <- streamDLResult{index: idx, reader: reader, err: dlErr}
		}(i)
	}
	go func() {
		dlWg.Wait()
		close(resultCh)
	}()

	// Stream through pipe: collector reorders chunk streams and copies them in order.
	pr, pw := io.Pipe()
	h := sha256.New()
	hashWriter := io.MultiWriter(pw, h)

	errCh := make(chan error, 1)
	go func() {
		defer func() { _ = pw.Close() }()
		pending := make(map[int]io.ReadCloser)
		nextIdx := 0

		for res := range resultCh {
			if res.err != nil {
				cancel()
				errCh <- fmt.Errorf("failed to download chunk %d: %w", res.index, res.err)
				pw.CloseWithError(res.err)
				return
			}
			pending[res.index] = res.reader
			// Drain contiguous chunks in order.
			for {
				reader, ok := pending[nextIdx]
				if !ok {
					break
				}
				if _, cErr := io.Copy(hashWriter, reader); cErr != nil {
					reader.Close()
					cancel()
					errCh <- fmt.Errorf("failed to stream chunk %d: %w", nextIdx, cErr)
					pw.CloseWithError(cErr)
					return
				}
				reader.Close()
				delete(pending, nextIdx)
				nextIdx++
			}
		}
		if nextIdx != totalChunks {
			errCh <- fmt.Errorf("incomplete merge: wrote %d of %d chunks", nextIdx, totalChunks)
			pw.CloseWithError(io.ErrUnexpectedEOF)
			return
		}
		errCh <- nil
	}()

	if err := s.objStore.UploadStream(ctx, s.bucket, storageKey, pr, uploadInfo.Size); err != nil {
		pw.CloseWithError(err)
		if writeErr := <-errCh; writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("failed to upload merged file: %w", err)
	}
	if writeErr := <-errCh; writeErr != nil {
		return writeErr
	}

	hash := hex.EncodeToString(h.Sum(nil))

	// Clean up chunks in parallel so large (>1000 chunk) uploads finish quickly.
	var cleanWg sync.WaitGroup
	for i := 0; i < totalChunks; i++ {
		cleanWg.Add(1)
		go func(idx int) {
			defer cleanWg.Done()
			chunkKey := fmt.Sprintf("chunks/%s/%d", uploadID, idx)
			if err := s.objStore.Delete(ctx, s.bucket, chunkKey); err != nil {
				s.logger.Debug("Failed to delete chunk after merge",
					zap.String("chunk_key", chunkKey),
					zap.Error(err))
			}
		}(i)
	}
	cleanWg.Wait()

	// Update upload info
	query := `
		UPDATE uploads
		SET status = $2, hash = $3, url = $4, updated_at = $5
		WHERE id = $1 AND status = 'uploading'
	`
	result, err := s.db.Exec(ctx, query, uploadID, "completed", hash, fmt.Sprintf("/%s/%s", s.bucket, storageKey), time.Now())
	if err != nil {
		return fmt.Errorf("failed to update upload info: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("upload already completed or status changed")
	}

	return nil
}

// DeleteUpload deletes an upload
func (s *UploadService) DeleteUpload(ctx context.Context, uploadID string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	uploadInfo, err := s.GetUploadStatus(ctx, uploadID)
	if err != nil {
		return err
	}

	prefix := "/" + s.bucket + "/"
	if len(uploadInfo.URL) < len(prefix) {
		return fmt.Errorf("unexpected URL format in upload %s: %s", uploadID, uploadInfo.URL)
	}
	storageKey := uploadInfo.URL[len(prefix):]

	if err := s.objStore.Delete(ctx, s.bucket, storageKey); err != nil {
		s.logger.Warn("Failed to delete from storage", zap.Error(err))
	}

	chunkPrefix := fmt.Sprintf("chunks/%s/", uploadID)
	chunks, err := s.objStore.ListObjects(ctx, s.bucket, chunkPrefix)
	if err != nil {
		s.logger.Warn("Failed to list chunks for cleanup", zap.String("upload_id", uploadID), zap.Error(err))
	} else if len(chunks) > 0 {
		if err := s.objStore.DeleteObjects(ctx, s.bucket, chunks); err != nil {
			s.logger.Warn("Failed to batch delete chunks", zap.String("upload_id", uploadID), zap.Error(err))
		}
	}

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
		res, err := tx.ExecContext(ctx, `
			UPDATE uploads SET status = $2, updated_at = $3 WHERE id = $1 AND status = 'completed'
		`, uploadID, "processed", time.Now())
		if err != nil {
			return fmt.Errorf("update upload: %w", err)
		}
		ra, _ := res.RowsAffected()
		if ra == 0 {
			return fmt.Errorf("upload not in completed state")
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

	s.hookMu.Lock()
	hooks := make([]PostUploadHook, len(s.onProcessed))
	copy(hooks, s.onProcessed)
	s.hookMu.Unlock()

	for _, hook := range hooks {
		go func(h PostUploadHook) {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error("PostUploadHook panic recovered",
						zap.String("upload_id", uploadID),
						zap.String("content_id", contentID),
						zap.Any("panic", r))
				}
			}()
			hookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			h(hookCtx, uploadID, contentID, upload.OwnerID)
		}(hook)
	}

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

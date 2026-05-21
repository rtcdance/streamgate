package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rtcdance/streamgate/pkg/cachetypes"
	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// ContentService handles content operations
type ContentService struct {
	db          storage.DB
	objStore    ContentObjectStorage
	cache       cachetypes.CacheBackend
	registry    ContentRegistry
	auditLogger storage.AuditLogger
	logger      *zap.Logger
	sf          singleflight.Group
}

// ContentRegistry defines the interface for on-chain content registration.
// Implemented by web3.ContentRegistryBinding; nil means on-chain registration is disabled.
type ContentRegistry interface {
	RegisterContent(ctx context.Context, contentHash [32]byte, metadata string) (string, error)
}

// ContentObjectStorage defines the interface for object storage
type ContentObjectStorage interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
	Download(ctx context.Context, bucket, key string) ([]byte, error)
	Delete(ctx context.Context, bucket, key string) error
	Exists(ctx context.Context, bucket, key string) (bool, error)
}

// Content represents a content item
type Content struct {
	ID           string                 `json:"id"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description"`
	Type         string                 `json:"type"` // video, audio, image, etc.
	URL          string                 `json:"url"`
	ThumbnailURL string                 `json:"thumbnail_url"`
	Duration     int                    `json:"duration"` // in seconds
	Size         int64                  `json:"size"`     // in bytes
	Status       string                 `json:"status"`   // pending, processing, ready, failed
	OwnerID      string                 `json:"owner_id"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// NewContentService creates a new content service
func NewContentService(db storage.DB, objStorage ContentObjectStorage, cache cachetypes.CacheBackend, logger ...*zap.Logger) *ContentService {
	var l *zap.Logger
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	} else {
		l = zap.NewNop()
	}
	return &ContentService{
		db:       db,
		objStore: objStorage,
		cache:    cache,
		logger:   l,
	}
}

// SetContentRegistry sets the on-chain content registry for registration after DB insert.
func (s *ContentService) SetContentRegistry(registry ContentRegistry) {
	s.registry = registry
}

// SetAuditLogger sets the audit logger for tracking content operations.
func (s *ContentService) SetAuditLogger(al storage.AuditLogger) {
	s.auditLogger = al
}

// GetContent gets content by ID
func (s *ContentService) GetContent(ctx context.Context, id string) (*Content, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	if s.cache != nil {
		if cached, err := s.cache.Get("content:" + id); err == nil {
			if content, ok := cached.(*Content); ok {
				return content, nil
			}
		}
	}

	v, err, _ := s.sf.Do("content:"+id, func() (interface{}, error) {
		query := `
			SELECT id, title, description, type, url, thumbnail_url, 
			       duration, size, status, owner_id, created_at, updated_at, metadata
			FROM contents
			WHERE id = $1
		`

		var content Content
		var metadataJSON []byte
		var desc, url, thumbURL, status, ownerID sql.NullString
		var duration sql.NullInt64
		var size sql.NullInt64

		err := s.db.QueryRow(ctx, query, id).Scan(
			&content.ID,
			&content.Title,
			&desc,
			&content.Type,
			&url,
			&thumbURL,
			&duration,
			&size,
			&status,
			&ownerID,
			&content.CreatedAt,
			&content.UpdatedAt,
			&metadataJSON,
		)

		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("content not found: %s", id)
		} else if err != nil {
			return nil, fmt.Errorf("failed to query content: %w", err)
		}

		content.Description = desc.String
		content.URL = url.String
		content.ThumbnailURL = thumbURL.String
		content.Duration = int(duration.Int64)
		content.Size = size.Int64
		content.Status = status.String
		content.OwnerID = ownerID.String

		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &content.Metadata); err != nil {
				return nil, fmt.Errorf("failed to parse metadata: %w", err)
			}
		}

		if s.cache != nil {
			cp := content
			if err := s.cache.SetWithExpiration("content:"+id, &cp, 15*time.Minute); err != nil {
				s.logger.Warn("Failed to cache content", zap.String("id", id), zap.Error(err))
			}
		}

		return &content, nil
	})
	if err != nil {
		return nil, err
	}
	content, ok := v.(*Content)
	if !ok {
		return nil, fmt.Errorf("unexpected cache value type: %T", v)
	}
	return content, nil
}

// CreateContent creates new content
func (s *ContentService) CreateContent(ctx context.Context, content *Content) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not available")
	}
	// Generate ID if not provided
	if content.ID == "" {
		content.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	content.CreatedAt = now
	content.UpdatedAt = now

	// Set default status
	if content.Status == "" {
		content.Status = "pending"
	}

	// Serialize metadata
	metadataJSON, err := json.Marshal(content.Metadata)
	if err != nil {
		return "", fmt.Errorf("failed to serialize metadata: %w", err)
	}

	// Insert into database
	query := `
		INSERT INTO contents (id, title, description, type, url, thumbnail_url,
		                     duration, size, status, owner_id, created_at, updated_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = s.db.Exec(ctx, query,
		content.ID,
		content.Title,
		content.Description,
		content.Type,
		content.URL,
		content.ThumbnailURL,
		content.Duration,
		content.Size,
		content.Status,
		content.OwnerID,
		content.CreatedAt,
		content.UpdatedAt,
		metadataJSON,
	)

	if err != nil {
		return "", fmt.Errorf("failed to insert content: %w", err)
	}

	s.logger.Info("Content created",
		zap.String("id", content.ID),
		zap.String("title", content.Title))

	if s.auditLogger != nil {
		s.auditLogger.Log(ctx, "content.create", content.OwnerID, "content", content.ID, true, "", content.Title)
	}

	return content.ID, nil
}

// UpdateContent updates existing content
func (s *ContentService) UpdateContent(ctx context.Context, content *Content) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	content.UpdatedAt = time.Now()

	// Serialize metadata
	metadataJSON, err := json.Marshal(content.Metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	query := `
		UPDATE contents
		SET title = $2, description = $3, type = $4, url = $5, thumbnail_url = $6,
		    duration = $7, size = $8, status = $9, updated_at = $10, metadata = $11
		WHERE id = $1 AND owner_id = $12
	`

	result, err := s.db.Exec(ctx, query,
		content.ID,
		content.Title,
		content.Description,
		content.Type,
		content.URL,
		content.ThumbnailURL,
		content.Duration,
		content.Size,
		content.Status,
		content.UpdatedAt,
		metadataJSON,
		content.OwnerID,
	)

	if err != nil {
		return fmt.Errorf("failed to update content: %w", err)
	}

	rowsAffected, errRA := result.RowsAffected()
	if errRA != nil {
		return errRA
	}
	if rowsAffected == 0 {
		return fmt.Errorf("content not found: %s", content.ID)
	}

	// Invalidate cache
	if s.cache != nil {
		if err := s.cache.Delete("content:" + content.ID); err != nil {
			s.logger.Warn("Failed to invalidate content cache", zap.String("id", content.ID), zap.Error(err))
		}
	}

	if s.auditLogger != nil {
		s.auditLogger.Log(ctx, "content.update", content.OwnerID, "content", content.ID, true, "", content.Title)
	}

	return nil
}

// CreateContentWithTx inserts content and its metadata in a single database
// transaction.  It uses the idiomatic defer-tx.Rollback pattern: if Commit
// succeeds the Rollback is a no-op; if anything fails the deferred Rollback
// cleans up automatically.
func (s *ContentService) CreateContentWithTx(ctx context.Context, content *Content) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not available")
	}

	// Generate ID if not provided
	if content.ID == "" {
		content.ID = uuid.New().String()
	}

	now := time.Now()
	content.CreatedAt = now
	content.UpdatedAt = now

	if content.Status == "" {
		content.Status = "pending"
	}

	metadataJSON, err := json.Marshal(content.Metadata)
	if err != nil {
		return "", fmt.Errorf("failed to serialize metadata: %w", err)
	}

	// Begin transaction
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after successful Commit

	// Insert content row
	contentQuery := `
		INSERT INTO contents (id, title, description, type, url, thumbnail_url,
		                      duration, size, status, owner_id, created_at, updated_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err = tx.ExecContext(ctx, contentQuery,
		content.ID, content.Title, content.Description, content.Type,
		content.URL, content.ThumbnailURL, content.Duration, content.Size,
		content.Status, content.OwnerID, content.CreatedAt, content.UpdatedAt,
		metadataJSON,
	)
	if err != nil {
		return "", fmt.Errorf("insert content: %w", err)
	}

	// Insert metadata row (if content_metadata table exists)
	metaQuery := `
		INSERT INTO content_metadata (content_id, metadata, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (content_id) DO NOTHING
	`
	if _, err := tx.ExecContext(ctx, metaQuery, content.ID, metadataJSON, now); err != nil {
		s.logger.Warn("failed to insert content metadata row",
			zap.String("contentID", content.ID), zap.Error(err))
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit tx: %w", err)
	}

	// Optionally register on-chain after successful DB commit
	if s.registry != nil {
		var contentHash [32]byte
		copy(contentHash[:], []byte(content.ID))
		if txHash, err := s.registry.RegisterContent(ctx, contentHash, content.Title); err != nil {
			s.logger.Warn("On-chain registration failed (content still in DB)",
				zap.String("id", content.ID),
				zap.Error(err))
		} else {
			s.logger.Info("Content registered on-chain",
				zap.String("id", content.ID),
				zap.String("tx_hash", txHash))
		}
	}

	s.logger.Info("Content created with tx", zap.String("id", content.ID))
	return content.ID, nil
}

// DeleteContentWithTx deletes content and its metadata in a single transaction.
func (s *ContentService) DeleteContentWithTx(ctx context.Context, id, ownerID string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete metadata first (foreign key constraint)
	if _, err := tx.ExecContext(ctx, "DELETE FROM content_metadata WHERE content_id = $1", id); err != nil {
		s.logger.Warn("failed to delete content metadata row",
			zap.String("contentID", id), zap.Error(err))
	}

	// Delete content
	result, err := tx.ExecContext(ctx, "DELETE FROM contents WHERE id = $1 AND owner_id = $2", id, ownerID)
	if err != nil {
		return fmt.Errorf("delete content: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("content not found: %s", id)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	// Invalidate cache
	if s.cache != nil {
		if err := s.cache.Delete("content:" + id); err != nil {
			s.logger.Warn("Failed to invalidate content cache", zap.String("id", id), zap.Error(err))
		}
	}

	return nil
}

// DeleteContent deletes content
func (s *ContentService) DeleteContent(ctx context.Context, id, ownerID string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	// Get content first to get file URL
	content, err := s.GetContent(ctx, id)
	if err != nil {
		return err
	}

	// Delete from storage if URL exists
	if content.URL != "" && s.objStore != nil {
		bucket := "content"
		key := id
		if err := s.objStore.Delete(ctx, bucket, key); err != nil {
			s.logger.Warn("Failed to delete from storage", zap.Error(err))
		}
	}

	err = s.db.InTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM contents WHERE id = $1 AND owner_id = $2", id, ownerID)
		if err != nil {
			return fmt.Errorf("delete content: %w", err)
		}
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			return fmt.Errorf("content not found: %s", id)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Invalidate cache
	if s.cache != nil {
		if err := s.cache.Delete("content:" + id); err != nil {
			s.logger.Warn("Failed to invalidate content cache on delete", zap.String("id", id), zap.Error(err))
		}
	}

	if s.auditLogger != nil {
		s.auditLogger.Log(ctx, "content.delete", ownerID, "content", id, true, "", content.Title)
	}

	return nil
}

func (s *ContentService) ListContentsWithCount(ctx context.Context, ownerID string, limit, offset int) ([]*Content, int, error) {
	if s.db == nil {
		return nil, 0, fmt.Errorf("database not available")
	}
	query := `
		SELECT COUNT(*) OVER() AS total_count,
		       id, title, description, type, url, thumbnail_url,
		       duration, size, status, owner_id, created_at, updated_at, metadata
		FROM contents
		WHERE owner_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(ctx, query, ownerID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query contents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	contents := make([]*Content, 0)
	var totalCount int
	for rows.Next() {
		var content Content
		var metadataJSON []byte
		var desc, url, thumbURL, status, ownerIDVal sql.NullString
		var duration sql.NullInt64
		var size sql.NullInt64

		err := rows.Scan(
			&totalCount,
			&content.ID,
			&content.Title,
			&desc,
			&content.Type,
			&url,
			&thumbURL,
			&duration,
			&size,
			&status,
			&ownerIDVal,
			&content.CreatedAt,
			&content.UpdatedAt,
			&metadataJSON,
		)

		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan content: %w", err)
		}

		content.Description = desc.String
		content.URL = url.String
		content.ThumbnailURL = thumbURL.String
		content.Duration = int(duration.Int64)
		content.Size = size.Int64
		content.Status = status.String
		content.OwnerID = ownerIDVal.String

		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &content.Metadata); err != nil {
				return nil, 0, fmt.Errorf("failed to parse metadata: %w", err)
			}
		}

		contents = append(contents, &content)
	}

	return contents, totalCount, nil
}

// ListContents lists contents with pagination
func (s *ContentService) ListContents(ctx context.Context, ownerID string, limit, offset int) ([]*Content, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `
		SELECT id, title, description, type, url, thumbnail_url,
		       duration, size, status, owner_id, created_at, updated_at, metadata
		FROM contents
		WHERE owner_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(ctx, query, ownerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query contents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	contents := make([]*Content, 0)
	for rows.Next() {
		var content Content
		var metadataJSON []byte
		var desc, url, thumbURL, status, ownerID sql.NullString
		var duration sql.NullInt64
		var size sql.NullInt64

		err := rows.Scan(
			&content.ID,
			&content.Title,
			&desc,
			&content.Type,
			&url,
			&thumbURL,
			&duration,
			&size,
			&status,
			&ownerID,
			&content.CreatedAt,
			&content.UpdatedAt,
			&metadataJSON,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan content: %w", err)
		}

		content.Description = desc.String
		content.URL = url.String
		content.ThumbnailURL = thumbURL.String
		content.Duration = int(duration.Int64)
		content.Size = size.Int64
		content.Status = status.String
		content.OwnerID = ownerID.String

		// Parse metadata
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &content.Metadata); err != nil {
				return nil, fmt.Errorf("failed to parse metadata: %w", err)
			}
		}

		contents = append(contents, &content)
	}

	return contents, nil
}

// CountContents returns the total number of contents for an owner
func (s *ContentService) CountContents(ctx context.Context, ownerID string) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("database not available")
	}
	var count int
	err := s.db.QueryRow(ctx, "SELECT COUNT(*) FROM contents WHERE owner_id = $1", ownerID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count contents: %w", err)
	}
	return count, nil
}

// UpdateContentStatus updates content status
func (s *ContentService) UpdateContentStatus(ctx context.Context, id, status string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}

	var currentStatus string
	if err := s.db.QueryRow(ctx, "SELECT status FROM contents WHERE id = $1", id).Scan(&currentStatus); err != nil {
		return fmt.Errorf("content not found: %s", id)
	}
	if !models.IsValidContentTransition(models.ContentStatus(currentStatus), models.ContentStatus(status)) {
		return fmt.Errorf("invalid status transition: %s -> %s", currentStatus, status)
	}

	query := "UPDATE contents SET status = $2, updated_at = $3 WHERE id = $1 AND status = $4"
	result, err := s.db.Exec(ctx, query, id, status, time.Now(), currentStatus)
	if err != nil {
		return fmt.Errorf("failed to update content status: %w", err)
	}

	rowsAffected, errRA := result.RowsAffected()
	if errRA != nil {
		return errRA
	}
	if rowsAffected == 0 {
		return fmt.Errorf("content status changed concurrently, please retry")
	}

	// Invalidate cache
	if s.cache != nil {
		if err := s.cache.Delete("content:" + id); err != nil {
			s.logger.Warn("Failed to invalidate content cache on status change", zap.String("id", id), zap.Error(err))
		}
	}

	return nil
}

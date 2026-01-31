package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ContentService handles content operations
type ContentService struct {
	db      *sql.DB
	storage ContentObjectStorage
	cache   ContentCacheStorage
}

// ContentObjectStorage defines the interface for object storage
type ContentObjectStorage interface {
	Upload(bucket, key string, data []byte) error
	Download(bucket, key string) ([]byte, error)
	Delete(bucket, key string) error
	Exists(bucket, key string) (bool, error)
}

// ContentCacheStorage defines the interface for cache storage
type ContentCacheStorage interface {
	Get(key string) (interface{}, error)
	Set(key string, value interface{}) error
	Delete(key string) error
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
func NewContentService(db *sql.DB, storage ContentObjectStorage, cache ContentCacheStorage) *ContentService {
	return &ContentService{
		db:      db,
		storage: storage,
		cache:   cache,
	}
}

// GetContent gets content by ID
func (s *ContentService) GetContent(id string) (*Content, error) {
	// Try cache first
	if s.cache != nil {
		if cached, err := s.cache.Get("content:" + id); err == nil {
			if content, ok := cached.(*Content); ok {
				return content, nil
			}
		}
	}

	// Query from database
	query := `
		SELECT id, title, description, type, url, thumbnail_url, 
		       duration, size, status, owner_id, created_at, updated_at, metadata
		FROM contents
		WHERE id = $1
	`

	var content Content
	var metadataJSON []byte

	err := s.db.QueryRow(query, id).Scan(
		&content.ID,
		&content.Title,
		&content.Description,
		&content.Type,
		&content.URL,
		&content.ThumbnailURL,
		&content.Duration,
		&content.Size,
		&content.Status,
		&content.OwnerID,
		&content.CreatedAt,
		&content.UpdatedAt,
		&metadataJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("content not found: %s", id)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query content: %w", err)
	}

	// Parse metadata
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &content.Metadata); err != nil {
			return nil, fmt.Errorf("failed to parse metadata: %w", err)
		}
	}

	// Cache the result
	if s.cache != nil {
		s.cache.Set("content:"+id, &content)
	}

	return &content, nil
}

// CreateContent creates new content
func (s *ContentService) CreateContent(content *Content) (string, error) {
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

	_, err = s.db.Exec(query,
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

	return content.ID, nil
}

// UpdateContent updates existing content
func (s *ContentService) UpdateContent(content *Content) error {
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
		WHERE id = $1
	`

	result, err := s.db.Exec(query,
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
	)

	if err != nil {
		return fmt.Errorf("failed to update content: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("content not found: %s", content.ID)
	}

	// Invalidate cache
	if s.cache != nil {
		s.cache.Delete("content:" + content.ID)
	}

	return nil
}

// DeleteContent deletes content
func (s *ContentService) DeleteContent(id string) error {
	// Get content first to get file URL
	content, err := s.GetContent(id)
	if err != nil {
		return err
	}

	// Delete from storage if URL exists
	if content.URL != "" && s.storage != nil {
		// Extract bucket and key from URL
		// This is a simplified version, actual implementation would parse the URL
		bucket := "content"
	key := id
	if err := s.storage.Delete(bucket, key); err != nil {
		s.logger.Warn("Failed to delete from storage", zap.Error(err))
	}
	}

	// Delete from database
	query := "DELETE FROM contents WHERE id = $1"
	result, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete content: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("content not found: %s", id)
	}

	// Invalidate cache
	if s.cache != nil {
		s.cache.Delete("content:" + id)
	}

	return nil
}

// ListContents lists contents with pagination
func (s *ContentService) ListContents(ownerID string, limit, offset int) ([]*Content, error) {
	query := `
		SELECT id, title, description, type, url, thumbnail_url,
		       duration, size, status, owner_id, created_at, updated_at, metadata
		FROM contents
		WHERE owner_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(query, ownerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query contents: %w", err)
	}
	defer rows.Close()

	contents := make([]*Content, 0)
	for rows.Next() {
		var content Content
		var metadataJSON []byte

		err := rows.Scan(
			&content.ID,
			&content.Title,
			&content.Description,
			&content.Type,
			&content.URL,
			&content.ThumbnailURL,
			&content.Duration,
			&content.Size,
			&content.Status,
			&content.OwnerID,
			&content.CreatedAt,
			&content.UpdatedAt,
			&metadataJSON,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan content: %w", err)
		}

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

// UpdateContentStatus updates content status
func (s *ContentService) UpdateContentStatus(id, status string) error {
	query := "UPDATE contents SET status = $2, updated_at = $3 WHERE id = $1"
	result, err := s.db.Exec(query, id, status, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update content status: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("content not found: %s", id)
	}

	// Invalidate cache
	if s.cache != nil {
		s.cache.Delete("content:" + id)
	}

	return nil
}

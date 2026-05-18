package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"streamgate/pkg/cachetypes"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/storage"

	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// StreamingService handles streaming operations
type StreamingService struct {
	db       storage.DB
	objStore StreamingObjectStorage
	cache    cachetypes.CacheBackend
	baseURL  string
	logger   *zap.Logger
	sf       singleflight.Group
}

// StreamingObjectStorage defines the interface for object storage
type StreamingObjectStorage interface {
	Download(ctx context.Context, bucket, key string) ([]byte, error)
	Exists(ctx context.Context, bucket, key string) (bool, error)
}

// StreamInfo represents stream information
type StreamInfo struct {
	ID        string    `json:"id"`
	ContentID string    `json:"content_id"`
	Type      string    `json:"type"` // hls, dash, progressive
	URL       string    `json:"url"`
	Playlist  string    `json:"playlist"`
	Qualities []Quality `json:"qualities"`
	Duration  int       `json:"duration"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Quality represents a stream quality variant
type Quality struct {
	Name       string `json:"name"`       // 1080p, 720p, 480p, etc.
	Resolution string `json:"resolution"` // 1920x1080, 1280x720, etc.
	Bitrate    int    `json:"bitrate"`    // in kbps
	URL        string `json:"url"`
}

// NewStreamingService creates a new streaming service
func NewStreamingService(db storage.DB, objStorage StreamingObjectStorage, cache cachetypes.CacheBackend, baseURL string, logger ...*zap.Logger) *StreamingService {
	var l *zap.Logger
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	} else {
		l = zap.NewNop()
	}
	return &StreamingService{
		db:       db,
		objStore: objStorage,
		cache:    cache,
		baseURL:  baseURL,
		logger:   l,
	}
}

// GetStream gets stream information
func (s *StreamingService) GetStream(ctx context.Context, contentID string) (*StreamInfo, error) {
	ctx, span := monitoring.StartOTelSpan(ctx, "streaming.get_stream",
		attribute.String("content_id", contentID),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("stream:%s", contentID)
	if s.cache != nil {
		if cached, err := s.cache.Get(cacheKey); err == nil {
			if streamInfo, ok := cached.(*StreamInfo); ok {
				return streamInfo, nil
			}
		}
	}

	v, err, _ := s.sf.Do(cacheKey, func() (interface{}, error) {
		if s.db == nil {
			return nil, fmt.Errorf("stream not found for content: %s", contentID)
		}

		query := `
			SELECT id, content_id, type, url, playlist, duration, status, created_at, expires_at
			FROM streams
			WHERE content_id = $1 AND status = 'ready'
			ORDER BY created_at DESC
			LIMIT 1
		`

		var streamInfo StreamInfo
		var url, playlist, status sql.NullString
		var duration sql.NullInt64
		var expiresAt sql.NullTime
		err := s.db.QueryRow(ctx, query, contentID).Scan(
			&streamInfo.ID,
			&streamInfo.ContentID,
			&streamInfo.Type,
			&url,
			&playlist,
			&duration,
			&status,
			&streamInfo.CreatedAt,
			&expiresAt,
		)

		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("stream not found for content: %s", contentID)
		} else if err != nil {
			return nil, fmt.Errorf("failed to query stream: %w", err)
		}

		streamInfo.URL = url.String
		streamInfo.Playlist = playlist.String
		streamInfo.Duration = int(duration.Int64)
		streamInfo.Status = status.String
		if expiresAt.Valid {
			streamInfo.ExpiresAt = expiresAt.Time
		}

		qualities, err := s.getStreamQualities(ctx, streamInfo.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get stream qualities: %w", err)
		}
		streamInfo.Qualities = qualities

		if s.cache != nil {
			if err := s.cache.SetWithExpiration(cacheKey, &streamInfo, 10*time.Minute); err != nil {
				s.logger.Warn("Failed to cache stream info", zap.String("content_id", contentID), zap.Error(err))
			}
		}

		return &streamInfo, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*StreamInfo), nil
}

// CreateStream creates a new stream
func (s *StreamingService) CreateStream(ctx context.Context, contentID, streamType string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not available")
	}
	// Generate stream ID
	streamID := fmt.Sprintf("stream_%s_%d", contentID, time.Now().Unix())

	// Create stream record
	query := `
		INSERT INTO streams (id, content_id, type, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	_, err := s.db.Exec(ctx, query, streamID, contentID, streamType, "pending", now, expiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to create stream: %w", err)
	}

	return streamID, nil
}

// GenerateHLSPlaylist generates an HLS playlist
func (s *StreamingService) GenerateHLSPlaylist(contentID string, qualities []Quality) (string, error) {
	var playlist strings.Builder

	// Write HLS header
	playlist.WriteString("#EXTM3U\n")
	playlist.WriteString("#EXT-X-VERSION:3\n")

	// Write quality variants
	for _, quality := range qualities {
		playlist.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n",
			quality.Bitrate*1000, quality.Resolution))
		playlist.WriteString(fmt.Sprintf("%s/%s/%s.m3u8\n",
			s.baseURL, contentID, quality.Name))
	}

	return playlist.String(), nil
}

// GenerateDASHManifest generates a DASH manifest
func (s *StreamingService) GenerateDASHManifest(contentID string, qualities []Quality) (string, error) {
	var manifest strings.Builder

	// Write DASH header
	manifest.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	manifest.WriteString("\n")
	manifest.WriteString(`<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static">`)
	manifest.WriteString("\n")
	manifest.WriteString(`  <Period>`)
	manifest.WriteString("\n")
	manifest.WriteString(`    <AdaptationSet mimeType="video/mp4">`)
	manifest.WriteString("\n")

	// Write quality representations
	for _, quality := range qualities {
		manifest.WriteString(fmt.Sprintf(`      <Representation bandwidth="%d" width=%q>`, //nolint:gocritic // "%s" is XML attribute syntax, not Go quoting
			quality.Bitrate*1000, strings.Split(quality.Resolution, "x")[0]))
		manifest.WriteString("\n")
		manifest.WriteString(fmt.Sprintf(`        <BaseURL>%s/%s/%s.mp4</BaseURL>`,
			s.baseURL, contentID, quality.Name))
		manifest.WriteString("\n")
		manifest.WriteString(`      </Representation>`)
		manifest.WriteString("\n")
	}

	manifest.WriteString(`    </AdaptationSet>`)
	manifest.WriteString("\n")
	manifest.WriteString(`  </Period>`)
	manifest.WriteString("\n")
	manifest.WriteString(`</MPD>`)

	return manifest.String(), nil
}

// UpdateStreamStatus updates stream status
var validStreamTransitions = map[string][]string{
	"pending": {"ready", "error"},
	"ready":   {"live", "error", "expired"},
	"live":    {"ended", "error"},
	"ended":   {"expired"},
	"expired": {},
	"error":   {"pending"},
}

func isValidStreamTransition(from, to string) bool {
	if from == to {
		return true
	}
	allowed, ok := validStreamTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

func (s *StreamingService) UpdateStreamStatus(ctx context.Context, streamID, status string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}

	var currentStatus, contentID string
	if err := s.db.QueryRow(ctx, "SELECT status, content_id FROM streams WHERE id = $1", streamID).Scan(&currentStatus, &contentID); err != nil {
		return fmt.Errorf("stream not found: %s", streamID)
	}
	if !isValidStreamTransition(currentStatus, status) {
		return fmt.Errorf("invalid stream status transition: %s -> %s", currentStatus, status)
	}

	query := "UPDATE streams SET status = $2 WHERE id = $1 AND status = $3"
	result, err := s.db.Exec(ctx, query, streamID, status, currentStatus)
	if err != nil {
		return fmt.Errorf("failed to update stream status: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("stream status changed concurrently, please retry")
	}

	if s.cache != nil && contentID != "" {
		if err := s.cache.Delete(fmt.Sprintf("stream:%s", contentID)); err != nil {
			s.logger.Warn("Failed to invalidate stream cache on status update", zap.String("content_id", contentID), zap.Error(err))
		}
	}

	return nil
}

// UpdateStreamPlaylist updates stream playlist
func (s *StreamingService) UpdateStreamPlaylist(ctx context.Context, streamID, playlist string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	url := fmt.Sprintf("%s/streams/%s/playlist.m3u8", s.baseURL, streamID)
	query := "UPDATE streams SET playlist = $2, url = $3 WHERE id = $1 RETURNING content_id"

	var contentID string
	if err := s.db.QueryRow(ctx, query, streamID, playlist, url).Scan(&contentID); err != nil {
		return fmt.Errorf("failed to update stream playlist: %w", err)
	}

	if s.cache != nil && contentID != "" {
		if delErr := s.cache.Delete(fmt.Sprintf("stream:%s", contentID)); delErr != nil {
			s.logger.Warn("Failed to invalidate stream cache on playlist update", zap.String("content_id", contentID), zap.Error(delErr))
		}
	}

	return nil
}

// AddStreamQuality adds a quality variant to a stream
func (s *StreamingService) AddStreamQuality(ctx context.Context, streamID string, quality Quality) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}

	query := `
		INSERT INTO stream_qualities (stream_id, name, resolution, bitrate, url)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := s.db.Exec(ctx, query, streamID, quality.Name, quality.Resolution, quality.Bitrate, quality.URL)
	if err != nil {
		return fmt.Errorf("failed to add stream quality: %w", err)
	}

	if s.cache != nil {
		var contentID string
		if err := s.db.QueryRow(ctx, "SELECT content_id FROM streams WHERE id = $1", streamID).Scan(&contentID); err == nil && contentID != "" {
			if delErr := s.cache.Delete(fmt.Sprintf("stream:%s", contentID)); delErr != nil {
				s.logger.Warn("Failed to invalidate stream cache on quality add", zap.String("content_id", contentID), zap.Error(delErr))
			}
		}
	}

	return nil
}

// getStreamQualities gets all quality variants for a stream
func (s *StreamingService) getStreamQualities(ctx context.Context, streamID string) ([]Quality, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `
		SELECT name, resolution, bitrate, url
		FROM stream_qualities
		WHERE stream_id = $1
		ORDER BY bitrate DESC
	`

	rows, err := s.db.Query(ctx, query, streamID)
	if err != nil {
		return nil, fmt.Errorf("failed to query stream qualities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	qualities := make([]Quality, 0)
	for rows.Next() {
		var quality Quality
		err := rows.Scan(&quality.Name, &quality.Resolution, &quality.Bitrate, &quality.URL)
		if err != nil {
			return nil, fmt.Errorf("failed to scan quality: %w", err)
		}
		qualities = append(qualities, quality)
	}

	return qualities, nil
}

// DeleteStream deletes a stream
func (s *StreamingService) DeleteStream(ctx context.Context, streamID string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}

	return s.db.InTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM stream_qualities WHERE stream_id = $1", streamID); err != nil {
			return fmt.Errorf("failed to delete stream qualities: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM streams WHERE id = $1", streamID); err != nil {
			return fmt.Errorf("failed to delete stream: %w", err)
		}
		return nil
	})
}

// GetStreamByID gets stream by ID
func (s *StreamingService) GetStreamByID(ctx context.Context, streamID string) (*StreamInfo, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `
		SELECT id, content_id, type, url, playlist, duration, status, created_at, expires_at
		FROM streams
		WHERE id = $1
	`

	var streamInfo StreamInfo
	var url, playlist, status sql.NullString
	var duration sql.NullInt64
	var expiresAt sql.NullTime
	err := s.db.QueryRow(ctx, query, streamID).Scan(
		&streamInfo.ID,
		&streamInfo.ContentID,
		&streamInfo.Type,
		&url,
		&playlist,
		&duration,
		&status,
		&streamInfo.CreatedAt,
		&expiresAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("stream not found: %s", streamID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query stream: %w", err)
	}

	streamInfo.URL = url.String
	streamInfo.Playlist = playlist.String
	streamInfo.Duration = int(duration.Int64)
	streamInfo.Status = status.String
	if expiresAt.Valid {
		streamInfo.ExpiresAt = expiresAt.Time
	}

	// Get qualities
	qualities, err := s.getStreamQualities(ctx, streamInfo.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream qualities: %w", err)
	}
	streamInfo.Qualities = qualities

	return &streamInfo, nil
}

// DetectStreamType detects stream type from file extension
func DetectStreamType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".m3u8":
		return "hls"
	case ".mpd":
		return "dash"
	case ".mp4", ".webm":
		return "progressive"
	default:
		return "unknown"
	}
}

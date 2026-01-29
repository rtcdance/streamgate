package service

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// StreamingService handles streaming operations
type StreamingService struct {
	db      *sql.DB
	storage StreamingObjectStorage
	cache   StreamingCacheStorage
	baseURL string
}

// StreamingObjectStorage defines the interface for object storage
type StreamingObjectStorage interface {
	Download(bucket, key string) ([]byte, error)
	Exists(bucket, key string) (bool, error)
}

// StreamingCacheStorage defines the interface for cache storage
type StreamingCacheStorage interface {
	Get(key string) (interface{}, error)
	Set(key string, value interface{}) error
	Delete(key string) error
}

// StreamInfo represents stream information
type StreamInfo struct {
	ID          string    `json:"id"`
	ContentID   string    `json:"content_id"`
	Type        string    `json:"type"` // hls, dash, progressive
	URL         string    `json:"url"`
	Playlist    string    `json:"playlist"`
	Qualities   []Quality `json:"qualities"`
	Duration    int       `json:"duration"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Quality represents a stream quality variant
type Quality struct {
	Name       string `json:"name"`       // 1080p, 720p, 480p, etc.
	Resolution string `json:"resolution"` // 1920x1080, 1280x720, etc.
	Bitrate    int    `json:"bitrate"`    // in kbps
	URL        string `json:"url"`
}

// NewStreamingService creates a new streaming service
func NewStreamingService(db *sql.DB, storage StreamingObjectStorage, cache StreamingCacheStorage, baseURL string) *StreamingService {
	return &StreamingService{
		db:      db,
		storage: storage,
		cache:   cache,
		baseURL: baseURL,
	}
}

// GetStream gets stream information
func (s *StreamingService) GetStream(contentID string) (*StreamInfo, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("stream:%s", contentID)
	if s.cache != nil {
		if cached, err := s.cache.Get(cacheKey); err == nil {
			if streamInfo, ok := cached.(*StreamInfo); ok {
				return streamInfo, nil
			}
		}
	}

	// Query from database
	query := `
		SELECT id, content_id, type, url, playlist, duration, status, created_at, expires_at
		FROM streams
		WHERE content_id = $1 AND status = 'ready'
		ORDER BY created_at DESC
		LIMIT 1
	`

	var streamInfo StreamInfo
	err := s.db.QueryRow(query, contentID).Scan(
		&streamInfo.ID,
		&streamInfo.ContentID,
		&streamInfo.Type,
		&streamInfo.URL,
		&streamInfo.Playlist,
		&streamInfo.Duration,
		&streamInfo.Status,
		&streamInfo.CreatedAt,
		&streamInfo.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("stream not found for content: %s", contentID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query stream: %w", err)
	}

	// Get qualities
	qualities, err := s.getStreamQualities(streamInfo.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream qualities: %w", err)
	}
	streamInfo.Qualities = qualities

	// Cache the result
	if s.cache != nil {
		s.cache.Set(cacheKey, &streamInfo)
	}

	return &streamInfo, nil
}

// CreateStream creates a new stream
func (s *StreamingService) CreateStream(contentID, streamType string) (string, error) {
	// Generate stream ID
	streamID := fmt.Sprintf("stream_%s_%d", contentID, time.Now().Unix())

	// Create stream record
	query := `
		INSERT INTO streams (id, content_id, type, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	_, err := s.db.Exec(query, streamID, contentID, streamType, "pending", now, expiresAt)
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
		manifest.WriteString(fmt.Sprintf(`      <Representation bandwidth="%d" width="%s">`,
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
func (s *StreamingService) UpdateStreamStatus(streamID, status string) error {
	query := "UPDATE streams SET status = $2 WHERE id = $1"
	_, err := s.db.Exec(query, streamID, status)
	if err != nil {
		return fmt.Errorf("failed to update stream status: %w", err)
	}

	// Invalidate cache
	if s.cache != nil {
		// Get content ID first
		var contentID string
		s.db.QueryRow("SELECT content_id FROM streams WHERE id = $1", streamID).Scan(&contentID)
		if contentID != "" {
			s.cache.Delete(fmt.Sprintf("stream:%s", contentID))
		}
	}

	return nil
}

// UpdateStreamPlaylist updates stream playlist
func (s *StreamingService) UpdateStreamPlaylist(streamID, playlist string) error {
	query := "UPDATE streams SET playlist = $2, url = $3 WHERE id = $1"
	url := fmt.Sprintf("%s/streams/%s/playlist.m3u8", s.baseURL, streamID)
	_, err := s.db.Exec(query, streamID, playlist, url)
	if err != nil {
		return fmt.Errorf("failed to update stream playlist: %w", err)
	}

	return nil
}

// AddStreamQuality adds a quality variant to a stream
func (s *StreamingService) AddStreamQuality(streamID string, quality Quality) error {
	query := `
		INSERT INTO stream_qualities (stream_id, name, resolution, bitrate, url)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := s.db.Exec(query, streamID, quality.Name, quality.Resolution, quality.Bitrate, quality.URL)
	if err != nil {
		return fmt.Errorf("failed to add stream quality: %w", err)
	}

	return nil
}

// getStreamQualities gets all quality variants for a stream
func (s *StreamingService) getStreamQualities(streamID string) ([]Quality, error) {
	query := `
		SELECT name, resolution, bitrate, url
		FROM stream_qualities
		WHERE stream_id = $1
		ORDER BY bitrate DESC
	`

	rows, err := s.db.Query(query, streamID)
	if err != nil {
		return nil, fmt.Errorf("failed to query stream qualities: %w", err)
	}
	defer rows.Close()

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
func (s *StreamingService) DeleteStream(streamID string) error {
	// Delete qualities first
	_, err := s.db.Exec("DELETE FROM stream_qualities WHERE stream_id = $1", streamID)
	if err != nil {
		return fmt.Errorf("failed to delete stream qualities: %w", err)
	}

	// Delete stream
	_, err = s.db.Exec("DELETE FROM streams WHERE id = $1", streamID)
	if err != nil {
		return fmt.Errorf("failed to delete stream: %w", err)
	}

	return nil
}

// GetStreamByID gets stream by ID
func (s *StreamingService) GetStreamByID(streamID string) (*StreamInfo, error) {
	query := `
		SELECT id, content_id, type, url, playlist, duration, status, created_at, expires_at
		FROM streams
		WHERE id = $1
	`

	var streamInfo StreamInfo
	err := s.db.QueryRow(query, streamID).Scan(
		&streamInfo.ID,
		&streamInfo.ContentID,
		&streamInfo.Type,
		&streamInfo.URL,
		&streamInfo.Playlist,
		&streamInfo.Duration,
		&streamInfo.Status,
		&streamInfo.CreatedAt,
		&streamInfo.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("stream not found: %s", streamID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query stream: %w", err)
	}

	// Get qualities
	qualities, err := s.getStreamQualities(streamInfo.ID)
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

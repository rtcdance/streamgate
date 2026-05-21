package streamingsvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/cachetypes"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/service/serviceerrors"
	"github.com/rtcdance/streamgate/pkg/storage"

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
	if db == nil {
		l.Warn("StreamingService created without database, all DB operations will return errors")
	}
	return &StreamingService{
		db:       db,
		objStore: objStorage,
		cache:    cache,
		baseURL:  baseURL,
		logger:   l,
	}
}

func (s *StreamingService) Close() {
}

func (s *StreamingService) checkDB() error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	return nil
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
				cp := *streamInfo
				cp.Qualities = make([]Quality, len(streamInfo.Qualities))
				copy(cp.Qualities, streamInfo.Qualities)
				return &cp, nil
			}
		}
	}

	v, err, _ := s.sf.Do(cacheKey, func() (interface{}, error) {
		if err := s.checkDB(); err != nil {
			return nil, fmt.Errorf("stream not found for content %s: %w", contentID, serviceerrors.ErrNotFound)
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
			return nil, fmt.Errorf("stream not found for content %s: %w", contentID, serviceerrors.ErrNotFound)
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
	si, ok := v.(*StreamInfo)
	if !ok {
		return nil, fmt.Errorf("unexpected cache value type: %T", v)
	}
	return si, nil
}

// CreateStream creates a new stream
func (s *StreamingService) CreateStream(ctx context.Context, contentID, streamType string) (string, error) {
	if err := s.checkDB(); err != nil {
		return "", err
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

// GenerateHLSPlaylist generates an HLS master playlist with inline segments
// for multi-quality streaming. The playbackToken parameter is a placeholder
// (e.g. "{{PLAYBACK_TOKEN}}") that the handler replaces per-request.
func (s *StreamingService) GenerateHLSPlaylist(contentID string, qualitySegments map[string][]string, playbackToken string) (string, error) {
	if len(qualitySegments) == 0 {
		return "", fmt.Errorf("no segments available for content %s", contentID)
	}
	if len(qualitySegments) == 1 {
		for _, segs := range qualitySegments {
			return BuildSimplePlaylist(contentID, segs, playbackToken), nil
		}
	}
	return BuildMasterPlaylist(contentID, qualitySegments, playbackToken), nil
}

var qualityBandwidth = map[string]int{
	"1080p": 5000,
	"720p":  2800,
	"480p":  1400,
	"360p":  800,
}

func BuildSimplePlaylist(contentID string, segments []string, playbackToken string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:0\n")
	for _, seg := range segments {
		name := seg
		if idx := strings.LastIndex(seg, "/"); idx >= 0 {
			name = seg[idx+1:]
		}
		b.WriteString(fmt.Sprintf("#EXTINF:6.0,\n/api/v1/streaming/%s/segment/%s?playback_token=%s\n", contentID, name, playbackToken))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func BuildMasterPlaylist(contentID string, qualitySegments map[string][]string, playbackToken string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for quality := range qualitySegments {
		bw := qualityBandwidth[quality]
		if bw == 0 {
			bw = 1500
		}
		resolution := qualityToResolution(quality)
		b.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bw*1000, resolution))
		b.WriteString(fmt.Sprintf("/api/v1/streaming/%s/manifest.m3u8?quality=%s&playback_token=%s\n", contentID, quality, playbackToken))
	}
	return b.String()
}

func BuildMediaPlaylist(contentID string, quality string, segments []string, playbackToken string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:0\n")
	for _, seg := range segments {
		name := seg
		if idx := strings.LastIndex(seg, "/"); idx >= 0 {
			name = seg[idx+1:]
		}
		b.WriteString(fmt.Sprintf("#EXTINF:6.0,\n/api/v1/streaming/%s/segment/%s?quality=%s&playback_token=%s\n", contentID, name, quality, playbackToken))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func qualityToResolution(quality string) string {
	switch quality {
	case "1080p":
		return "1920x1080"
	case "720p":
		return "1280x720"
	case "480p":
		return "854x480"
	case "360p":
		return "640x360"
	default:
		return "1280x720"
	}
}

// GenerateDASHManifest generates a DASH manifest with playback token for segment access.
func (s *StreamingService) GenerateDASHManifest(contentID string, qualities []Quality, playbackToken string) (string, error) {
	var manifest strings.Builder

	manifest.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	manifest.WriteString("\n")
	manifest.WriteString(`<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static">`)
	manifest.WriteString("\n")
	manifest.WriteString(`  <Period>`)
	manifest.WriteString("\n")
	manifest.WriteString(`    <AdaptationSet mimeType="video/mp4">`)
	manifest.WriteString("\n")

	for _, quality := range qualities {
		manifest.WriteString(fmt.Sprintf(`      <Representation bandwidth="%d" width=%q>`, //nolint:gocritic // "%s" is XML attribute syntax, not Go quoting
			quality.Bitrate*1000, strings.Split(quality.Resolution, "x")[0]))
		manifest.WriteString("\n")
		manifest.WriteString(fmt.Sprintf(`        <BaseURL>%s/%s/%s.mp4?playback_token=%s</BaseURL>`,
			s.baseURL, contentID, quality.Name, playbackToken))
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
	if err := s.checkDB(); err != nil {
		return err
	}

	var currentStatus, contentID string
	if err := s.db.QueryRow(ctx, "SELECT status, content_id FROM streams WHERE id = $1", streamID).Scan(&currentStatus, &contentID); err != nil {
		return fmt.Errorf("stream not found: %s: %w", streamID, err)
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
	if err := s.checkDB(); err != nil {
		return err
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
	if err := s.checkDB(); err != nil {
		return err
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
	if err := s.checkDB(); err != nil {
		return nil, err
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
	if err := s.checkDB(); err != nil {
		return err
	}

	_, err := s.db.Exec(ctx, "DELETE FROM streams WHERE id = $1", streamID)
	if err != nil {
		return fmt.Errorf("failed to delete stream: %w", err)
	}
	return nil
}

// GetStreamByID gets stream by ID
func (s *StreamingService) GetStreamByID(ctx context.Context, streamID string) (*StreamInfo, error) {
	if err := s.checkDB(); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("stream not found: %s: %w", streamID, serviceerrors.ErrNotFound)
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

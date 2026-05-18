package service

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"streamgate/pkg/models"
	"streamgate/pkg/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type PlaybackStatsService struct {
	db         storage.DB
	logger     *zap.Logger
	debounceMu sync.Mutex
	pending    map[string]*time.Timer
	debounceDelay time.Duration
}

func NewPlaybackStatsService(db storage.DB, logger *zap.Logger) *PlaybackStatsService {
	return &PlaybackStatsService{
		db:            db,
		logger:        logger,
		pending:       make(map[string]*time.Timer),
		debounceDelay: 5 * time.Second,
	}
}

func (s *PlaybackStatsService) RecordEvent(ctx context.Context, event *models.PlaybackEvent) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	query := `
		INSERT INTO playback_events (id, content_id, wallet_address, event_type, duration_seconds, playback_token_jti, user_agent, ip_address, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := s.db.Exec(ctx, query,
		event.ID, event.ContentID, event.WalletAddress, event.EventType,
		event.DurationSeconds, event.PlaybackTokenJTI, event.UserAgent,
		event.IPAddress, event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to record playback event: %w", err)
	}

	s.scheduleAggregation(event.ContentID)

	return nil
}

func (s *PlaybackStatsService) scheduleAggregation(contentID string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()

	if t, ok := s.pending[contentID]; ok {
		t.Stop()
	}

	s.pending[contentID] = time.AfterFunc(s.debounceDelay, func() {
		s.debounceMu.Lock()
		delete(s.pending, contentID)
		s.debounceMu.Unlock()

		s.updateStatsAggregation(contentID)
	})
}

func (s *PlaybackStatsService) updateStatsAggregation(contentID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `
		INSERT INTO content_stats (content_id, total_plays, unique_viewers, total_watch_seconds, avg_watch_seconds, updated_at)
		SELECT
			$1,
			COUNT(*),
			COUNT(DISTINCT wallet_address),
			COALESCE(SUM(duration_seconds), 0),
			COALESCE(AVG(duration_seconds)::INT, 0),
			NOW()
		FROM playback_events WHERE content_id = $1
		ON CONFLICT (content_id) DO UPDATE SET
			total_plays = EXCLUDED.total_plays,
			unique_viewers = EXCLUDED.unique_viewers,
			total_watch_seconds = EXCLUDED.total_watch_seconds,
			avg_watch_seconds = EXCLUDED.avg_watch_seconds,
			updated_at = EXCLUDED.updated_at
	`
	_, err := s.db.Exec(ctx, query, contentID)
	if err != nil {
		s.logger.Warn("failed to update content stats aggregation", zap.String("content_id", contentID), zap.Error(err))
	}
}

func (s *PlaybackStatsService) GetContentStats(ctx context.Context, contentID string) (*models.ContentStats, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `
		SELECT content_id, total_plays, unique_viewers, total_watch_seconds, avg_watch_seconds, updated_at
		FROM content_stats WHERE content_id = $1
	`
	var stats models.ContentStats
	err := s.db.QueryRow(ctx, query, contentID).Scan(
		&stats.ContentID, &stats.TotalPlays, &stats.UniqueViewers,
		&stats.TotalWatchSeconds, &stats.AvgWatchSeconds, &stats.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return &models.ContentStats{
			ContentID: contentID,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query content stats: %w", err)
	}
	return &stats, nil
}

func (s *PlaybackStatsService) ListTopContent(ctx context.Context, limit int) ([]*models.ContentStats, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	if limit <= 0 {
		limit = 20
	}
	query := `
		SELECT content_id, total_plays, unique_viewers, total_watch_seconds, avg_watch_seconds, updated_at
		FROM content_stats ORDER BY total_plays DESC LIMIT $1
	`
	rows, err := s.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list top content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*models.ContentStats
	for rows.Next() {
		var cs models.ContentStats
		if err := rows.Scan(
			&cs.ContentID, &cs.TotalPlays, &cs.UniqueViewers,
			&cs.TotalWatchSeconds, &cs.AvgWatchSeconds, &cs.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan content stats: %w", err)
		}
		result = append(result, &cs)
	}
	return result, nil
}

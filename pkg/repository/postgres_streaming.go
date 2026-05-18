package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"streamgate/pkg/service/streaming"
	"streamgate/pkg/storage"
)

type PostgresStreamingRepository struct {
	db storage.DB
}

func NewPostgresStreamingRepository(db storage.DB) *PostgresStreamingRepository {
	return &PostgresStreamingRepository{db: db}
}

func (r *PostgresStreamingRepository) GetStreamByContentID(ctx context.Context, contentID string) (*streaming.StreamInfo, error) {
	query := `
		SELECT id, content_id, type, url, playlist, duration, status, created_at, expires_at
		FROM streams
		WHERE content_id = $1 AND status = 'ready'
		ORDER BY created_at DESC
		LIMIT 1
	`

	var info streaming.StreamInfo
	var url, playlist, status sql.NullString
	var duration sql.NullInt64
	var expiresAt sql.NullTime

	err := r.db.QueryRow(ctx, query, contentID).Scan(
		&info.ID,
		&info.ContentID,
		&info.Type,
		&url,
		&playlist,
		&duration,
		&status,
		&info.CreatedAt,
		&expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("stream not found for content: %s", contentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query stream: %w", err)
	}

	info.URL = url.String
	info.Playlist = playlist.String
	info.Duration = int(duration.Int64)
	info.Status = status.String
	if expiresAt.Valid {
		info.ExpiresAt = expiresAt.Time
	}

	qualities, err := r.GetStreamQualities(ctx, info.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream qualities: %w", err)
	}
	info.Qualities = qualities

	return &info, nil
}

func (r *PostgresStreamingRepository) GetStreamByID(ctx context.Context, streamID string) (*streaming.StreamInfo, error) {
	query := `
		SELECT id, content_id, type, url, playlist, duration, status, created_at, expires_at
		FROM streams
		WHERE id = $1
	`

	var info streaming.StreamInfo
	var url, playlist, status sql.NullString
	var duration sql.NullInt64
	var expiresAt sql.NullTime

	err := r.db.QueryRow(ctx, query, streamID).Scan(
		&info.ID,
		&info.ContentID,
		&info.Type,
		&url,
		&playlist,
		&duration,
		&status,
		&info.CreatedAt,
		&expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("stream not found: %s", streamID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query stream: %w", err)
	}

	info.URL = url.String
	info.Playlist = playlist.String
	info.Duration = int(duration.Int64)
	info.Status = status.String
	if expiresAt.Valid {
		info.ExpiresAt = expiresAt.Time
	}

	qualities, err := r.GetStreamQualities(ctx, info.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream qualities: %w", err)
	}
	info.Qualities = qualities

	return &info, nil
}

func (r *PostgresStreamingRepository) GetStreamQualities(ctx context.Context, streamID string) ([]streaming.Quality, error) {
	query := `
		SELECT name, resolution, bitrate, url
		FROM stream_qualities
		WHERE stream_id = $1
		ORDER BY bitrate DESC
	`

	rows, err := r.db.Query(ctx, query, streamID)
	if err != nil {
		return nil, fmt.Errorf("failed to query stream qualities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	qualities := make([]streaming.Quality, 0)
	for rows.Next() {
		var q streaming.Quality
		if err := rows.Scan(&q.Name, &q.Resolution, &q.Bitrate, &q.URL); err != nil {
			return nil, fmt.Errorf("failed to scan quality: %w", err)
		}
		qualities = append(qualities, q)
	}

	return qualities, nil
}

func (r *PostgresStreamingRepository) CreateStream(ctx context.Context, contentID, streamType string) (string, error) {
	streamID := fmt.Sprintf("stream_%s_%d", contentID, time.Now().Unix())

	query := `
		INSERT INTO streams (id, content_id, type, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	_, err := r.db.Exec(ctx, query, streamID, contentID, streamType, "pending", now, expiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to create stream: %w", err)
	}

	return streamID, nil
}

func (r *PostgresStreamingRepository) UpdateStreamStatus(ctx context.Context, streamID, status string) (currentStatus, contentID string, err error) {
	if err := r.db.QueryRow(ctx, "SELECT status, content_id FROM streams WHERE id = $1", streamID).Scan(&currentStatus, &contentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("stream not found: %s", streamID)
		}
		return "", "", fmt.Errorf("failed to query stream status: %w", err)
	}

	query := "UPDATE streams SET status = $2 WHERE id = $1 AND status = $3"
	result, err := r.db.Exec(ctx, query, streamID, status, currentStatus)
	if err != nil {
		return "", "", fmt.Errorf("failed to update stream status: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return "", "", fmt.Errorf("stream status changed concurrently, please retry")
	}

	return currentStatus, contentID, nil
}

func (r *PostgresStreamingRepository) UpdateStreamPlaylist(ctx context.Context, streamID, playlist, url string) (contentID string, err error) {
	query := "UPDATE streams SET playlist = $2, url = $3 WHERE id = $1 RETURNING content_id"

	if err := r.db.QueryRow(ctx, query, streamID, playlist, url).Scan(&contentID); err != nil {
		return "", fmt.Errorf("failed to update stream playlist: %w", err)
	}

	return contentID, nil
}

func (r *PostgresStreamingRepository) AddStreamQuality(ctx context.Context, streamID string, q streaming.Quality) (contentID string, err error) {
	query := `
		INSERT INTO stream_qualities (stream_id, name, resolution, bitrate, url)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING (SELECT content_id FROM streams WHERE id = $1)
	`

	var cid sql.NullString
	if err := r.db.QueryRow(ctx, query, streamID, q.Name, q.Resolution, q.Bitrate, q.URL).Scan(&cid); err != nil {
		return "", fmt.Errorf("failed to add stream quality: %w", err)
	}

	return cid.String, nil
}

func (r *PostgresStreamingRepository) DeleteStream(ctx context.Context, streamID string) error {
	return r.db.InTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM stream_qualities WHERE stream_id = $1", streamID); err != nil {
			return fmt.Errorf("failed to delete stream qualities: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM streams WHERE id = $1", streamID); err != nil {
			return fmt.Errorf("failed to delete stream: %w", err)
		}
		return nil
	})
}

func (r *PostgresStreamingRepository) GetStreamContentID(ctx context.Context, streamID string) (string, error) {
	var contentID string
	err := r.db.QueryRow(ctx, "SELECT content_id FROM streams WHERE id = $1", streamID).Scan(&contentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("stream not found: %s", streamID)
	}
	if err != nil {
		return "", fmt.Errorf("failed to query stream content_id: %w", err)
	}
	return contentID, nil
}

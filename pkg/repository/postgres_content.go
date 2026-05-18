package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"streamgate/pkg/service/content"
	"streamgate/pkg/storage"
)

type PostgresContentRepository struct {
	db storage.DB
}

func NewPostgresContentRepository(db storage.DB) *PostgresContentRepository {
	return &PostgresContentRepository{db: db}
}

func (r *PostgresContentRepository) GetContentByID(ctx context.Context, id string) (*content.Content, error) {
	query := `
		SELECT id, title, description, type, url, thumbnail_url,
		       duration, size, status, owner_id, created_at, updated_at, metadata
		FROM contents
		WHERE id = $1
	`

	var c content.Content
	var metadataJSON []byte
	var desc, url, thumbURL, status, ownerID sql.NullString
	var duration sql.NullInt64
	var size sql.NullInt64

	err := r.db.QueryRow(ctx, query, id).Scan(
		&c.ID,
		&c.Title,
		&desc,
		&c.Type,
		&url,
		&thumbURL,
		&duration,
		&size,
		&status,
		&ownerID,
		&c.CreatedAt,
		&c.UpdatedAt,
		&metadataJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("content not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query content: %w", err)
	}

	c.Description = desc.String
	c.URL = url.String
	c.ThumbnailURL = thumbURL.String
	c.Duration = int(duration.Int64)
	c.Size = size.Int64
	c.Status = status.String
	c.OwnerID = ownerID.String

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &c.Metadata); err != nil {
			return nil, fmt.Errorf("failed to parse metadata: %w", err)
		}
	}

	return &c, nil
}

func (r *PostgresContentRepository) ListContent(ctx context.Context, limit, offset int) ([]*content.Content, error) {
	query := `
		SELECT id, title, description, type, url, thumbnail_url,
		       duration, size, status, owner_id, created_at, updated_at, metadata
		FROM contents
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query contents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	contents := make([]*content.Content, 0)
	for rows.Next() {
		var c content.Content
		var metadataJSON []byte
		var desc, url, thumbURL, status, ownerID sql.NullString
		var duration sql.NullInt64
		var size sql.NullInt64

		if err := rows.Scan(
			&c.ID,
			&c.Title,
			&desc,
			&c.Type,
			&url,
			&thumbURL,
			&duration,
			&size,
			&status,
			&ownerID,
			&c.CreatedAt,
			&c.UpdatedAt,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan content: %w", err)
		}

		c.Description = desc.String
		c.URL = url.String
		c.ThumbnailURL = thumbURL.String
		c.Duration = int(duration.Int64)
		c.Size = size.Int64
		c.Status = status.String
		c.OwnerID = ownerID.String

		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &c.Metadata); err != nil {
				return nil, fmt.Errorf("failed to parse metadata: %w", err)
			}
		}

		contents = append(contents, &c)
	}

	return contents, nil
}

func (r *PostgresContentRepository) CreateContent(ctx context.Context, c *content.Content) error {
	metadataJSON, err := json.Marshal(c.Metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	query := `
		INSERT INTO contents (id, title, description, type, url, thumbnail_url,
		                     duration, size, status, owner_id, created_at, updated_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = r.db.Exec(ctx, query,
		c.ID,
		c.Title,
		c.Description,
		c.Type,
		c.URL,
		c.ThumbnailURL,
		c.Duration,
		c.Size,
		c.Status,
		c.OwnerID,
		c.CreatedAt,
		c.UpdatedAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to insert content: %w", err)
	}

	return nil
}

func (r *PostgresContentRepository) UpdateContent(ctx context.Context, c *content.Content) error {
	metadataJSON, err := json.Marshal(c.Metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	query := `
		UPDATE contents
		SET title = $2, description = $3, type = $4, url = $5, thumbnail_url = $6,
		    duration = $7, size = $8, status = $9, updated_at = $10, metadata = $11
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query,
		c.ID,
		c.Title,
		c.Description,
		c.Type,
		c.URL,
		c.ThumbnailURL,
		c.Duration,
		c.Size,
		c.Status,
		c.UpdatedAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to update content: %w", err)
	}

	rowsAffected, errRA := result.RowsAffected()
	if errRA != nil {
		return errRA
	}
	if rowsAffected == 0 {
		return fmt.Errorf("content not found: %s", c.ID)
	}

	return nil
}

func (r *PostgresContentRepository) DeleteContent(ctx context.Context, id string) (string, error) {
	query := "DELETE FROM contents WHERE id = $1 RETURNING url"

	var url sql.NullString
	if err := r.db.QueryRow(ctx, query, id).Scan(&url); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("content not found: %s", id)
		}
		return "", fmt.Errorf("failed to delete content: %w", err)
	}

	return url.String, nil
}

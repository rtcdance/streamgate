package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"streamgate/pkg/service/upload"
	"streamgate/pkg/storage"
)

type PostgresUploadRepository struct {
	db storage.DB
}

func NewPostgresUploadRepository(db storage.DB) *PostgresUploadRepository {
	return &PostgresUploadRepository{db: db}
}

func (r *PostgresUploadRepository) GetUploadByID(ctx context.Context, uploadID string) (*upload.UploadInfo, error) {
	query := `
		SELECT id, filename, size, content_type, hash, status, url, owner_id, created_at, updated_at
		FROM uploads
		WHERE id = $1
	`

	var info upload.UploadInfo
	var contentType, hash, status, url, ownerID sql.NullString

	err := r.db.QueryRow(ctx, query, uploadID).Scan(
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
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query upload: %w", err)
	}

	info.ContentType = contentType.String
	info.Hash = hash.String
	info.Status = status.String
	info.URL = url.String
	info.OwnerID = ownerID.String

	return &info, nil
}

func (r *PostgresUploadRepository) ListUploadsByOwner(ctx context.Context, ownerID string, limit, offset int) ([]*upload.UploadInfo, error) {
	query := `
		SELECT id, filename, size, content_type, hash, status, url, owner_id, created_at, updated_at
		FROM uploads
		WHERE owner_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, ownerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query uploads: %w", err)
	}
	defer func() { _ = rows.Close() }()

	uploads := make([]*upload.UploadInfo, 0)
	for rows.Next() {
		var info upload.UploadInfo
		var contentType, hash, status, url, oid sql.NullString

		if err := rows.Scan(
			&info.ID,
			&info.Filename,
			&info.Size,
			&contentType,
			&hash,
			&status,
			&url,
			&oid,
			&info.CreatedAt,
			&info.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan upload: %w", err)
		}

		info.ContentType = contentType.String
		info.Hash = hash.String
		info.Status = status.String
		info.URL = url.String
		info.OwnerID = oid.String
		uploads = append(uploads, &info)
	}

	return uploads, nil
}

func (r *PostgresUploadRepository) CreateUpload(ctx context.Context, info *upload.UploadInfo) error {
	query := `
		INSERT INTO uploads (id, filename, size, content_type, hash, status, url, owner_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := r.db.Exec(ctx, query,
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
	if err != nil {
		return fmt.Errorf("failed to insert upload: %w", err)
	}

	return nil
}

func (r *PostgresUploadRepository) UpdateUploadStatus(ctx context.Context, uploadID, status string) error {
	query := "UPDATE uploads SET status = $2, updated_at = $3 WHERE id = $1"
	_, err := r.db.Exec(ctx, query, uploadID, status, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update upload status: %w", err)
	}
	return nil
}

func (r *PostgresUploadRepository) UpdateUploadURL(ctx context.Context, uploadID, url string) error {
	query := "UPDATE uploads SET url = $2, updated_at = $3 WHERE id = $1"
	_, err := r.db.Exec(ctx, query, uploadID, url, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update upload url: %w", err)
	}
	return nil
}

func (r *PostgresUploadRepository) DeleteUpload(ctx context.Context, uploadID string) error {
	_, err := r.db.Exec(ctx, "DELETE FROM uploads WHERE id = $1", uploadID)
	if err != nil {
		return fmt.Errorf("failed to delete upload: %w", err)
	}
	return nil
}

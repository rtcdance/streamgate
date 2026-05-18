package repository

import (
	"context"

	"streamgate/pkg/service/upload"
)

type UploadRepository interface {
	GetUploadByID(ctx context.Context, uploadID string) (*upload.UploadInfo, error)
	ListUploadsByOwner(ctx context.Context, ownerID string, limit, offset int) ([]*upload.UploadInfo, error)
	CreateUpload(ctx context.Context, info *upload.UploadInfo) error
	UpdateUploadStatus(ctx context.Context, uploadID, status string) error
	UpdateUploadURL(ctx context.Context, uploadID, url string) error
	DeleteUpload(ctx context.Context, uploadID string) error
}

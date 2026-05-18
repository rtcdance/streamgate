package upload

import (
	"context"
	"io"
	"time"
)

//go:generate mockgen -destination=mocks/mock_upload_object_storage.go -package=mocks streamgate/pkg/service/upload UploadObjectStorage
type UploadObjectStorage interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
	UploadStream(ctx context.Context, bucket, key string, reader io.Reader, size int64) error
	Download(ctx context.Context, bucket, key string) ([]byte, error)
	DownloadStream(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, bucket, key string) error
	DeleteObjects(ctx context.Context, bucket string, keys []string) error
	Exists(ctx context.Context, bucket, key string) (bool, error)
	ListObjects(ctx context.Context, bucket, prefix string) ([]string, error)
}

type PostUploadHook func(ctx context.Context, uploadID, contentID, ownerID string)

type UploadInfo struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	Hash        string    `json:"hash"`
	Status      string    `json:"status"`
	URL         string    `json:"url"`
	OwnerID     string    `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ChunkInfo struct {
	UploadID    string `json:"upload_id"`
	ChunkIndex  int    `json:"chunk_index"`
	TotalChunks int    `json:"total_chunks"`
	ChunkSize   int64  `json:"chunk_size"`
	Uploaded    bool   `json:"uploaded"`
}

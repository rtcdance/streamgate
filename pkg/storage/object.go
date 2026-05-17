package storage

import (
	"context"
	"io"
	"time"
)

// maxDownloadSize caps the amount of data read into memory during Download.
// Objects larger than this must use DownloadStream for incremental processing.
const maxDownloadSize int64 = 1 << 30 // 1 GB

// ObjectStorage defines the interface for object storage operations.
// Implemented by MinIOStorage and can be mocked for testing.
// All methods accept a context.Context for timeout/cancellation propagation.
//
//go:generate mockgen -destination=mocks/mock_object_storage.go -package=mocks streamgate/pkg/storage ObjectStorage
type ObjectStorage interface {
	Upload(ctx context.Context, bucket, objectName string, data []byte) error
	UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error
	UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error
	UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, bucket, objectName string) ([]byte, error)
	DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error)
	Delete(ctx context.Context, bucket, objectName string) error
	ListObjects(ctx context.Context, bucket, prefix string) ([]string, error)
	Exists(ctx context.Context, bucket, objectName string) (bool, error)
	CreateBucket(ctx context.Context, bucket string) error
	PresignedURL(ctx context.Context, bucket, objectName string, expiry time.Duration) (string, error)
}

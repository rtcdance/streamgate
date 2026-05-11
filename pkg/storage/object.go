package storage

import (
	"context"
	"io"
)

// ObjectStorage defines the interface for object storage operations.
// Implemented by MinIOStorage and can be mocked for testing.
// All methods accept a context.Context for timeout/cancellation propagation.
//go:generate mockgen -destination=mocks/mock_object_storage.go -package=mocks streamgate/pkg/storage ObjectStorage
type ObjectStorage interface {
	// Upload stores data with default content type
	Upload(ctx context.Context, bucket, objectName string, data []byte) error
	// UploadStream stores data from an io.Reader without full buffering
	UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error
	// UploadWithContentType stores data with a specific content type
	UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error
	// Download retrieves data by bucket and object name
	Download(ctx context.Context, bucket, objectName string) ([]byte, error)
	// Delete removes an object from the bucket
	Delete(ctx context.Context, bucket, objectName string) error
	// ListObjects returns object keys in a bucket with the given prefix
	ListObjects(ctx context.Context, bucket, prefix string) ([]string, error)
	// Exists checks whether an object exists
	Exists(ctx context.Context, bucket, objectName string) (bool, error)
	// CreateBucket creates a bucket if it does not exist
	CreateBucket(ctx context.Context, bucket string) error
}

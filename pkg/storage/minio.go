package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOStorage handles MinIO storage
type MinIOStorage struct {
	client *minio.Client
}

// Close releases MinIO resources. The minio-go client has no explicit Close,
// but implementing io.Closer allows AppResources to manage it uniformly.
func (ms *MinIOStorage) Close() error {
	return nil
}

// MinIOConfig holds MinIO configuration
type MinIOConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
}

// NewMinIOStorage creates a new MinIO storage instance
func NewMinIOStorage(config MinIOConfig) (*MinIOStorage, error) {
	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.UseSSL,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	return &MinIOStorage{
		client: client,
	}, nil
}

// Upload uploads to MinIO
func (ms *MinIOStorage) Upload(ctx context.Context, bucket, objectName string, data []byte) error {
	return ms.UploadStream(ctx, bucket, objectName, bytes.NewReader(data), int64(len(data)))
}

func (ms *MinIOStorage) UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	_, err := ms.client.PutObject(ctx, bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: detectContentTypeByExt(objectName),
	})
	if err != nil {
		return fmt.Errorf("failed to upload to MinIO: %w", err)
	}

	return nil
}

// UploadWithContentType uploads to MinIO with specific content type
func (ms *MinIOStorage) UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Ensure bucket exists
	exists, err := ms.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		if err := ms.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	// Upload object
	reader := bytes.NewReader(data)
	_, err = ms.client.PutObject(ctx, bucket, objectName, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})

	if err != nil {
		return fmt.Errorf("failed to upload to MinIO: %w", err)
	}

	return nil
}

func (ms *MinIOStorage) UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	exists, err := ms.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		if err := ms.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	_, err = ms.client.PutObject(ctx, bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("failed to upload stream to MinIO: %w", err)
	}
	return nil
}

// Download downloads from MinIO and returns the entire content as a byte slice.
// Only safe for objects smaller than maxDownloadSize (1 GB). For larger objects
// use DownloadStream to process the data incrementally.
func (ms *MinIOStorage) Download(ctx context.Context, bucket, objectName string) ([]byte, error) {
	rc, err := ms.DownloadStream(ctx, bucket, objectName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	buf := new(bytes.Buffer)
	n, err := io.Copy(buf, io.LimitReader(rc, maxDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read MinIO object: %w", err)
	}
	if n > maxDownloadSize {
		return nil, errors.New("MinIO object exceeds maximum download size (1 GB)")
	}

	return buf.Bytes(), nil
}

// DownloadStream returns an io.ReadCloser for streaming an object from MinIO.
// The caller must close the reader when done.
func (ms *MinIOStorage) DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)

	object, err := ms.client.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get object from MinIO: %w", err)
	}

	return &readCloserWithCancel{ReadCloser: object, cancel: cancel}, nil
}

// readCloserWithCancel wraps an io.ReadCloser with a context cancel func
// so that the context timeout is released when the reader is closed.
type readCloserWithCancel struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *readCloserWithCancel) Close() error {
	defer r.cancel()
	return r.ReadCloser.Close()
}

// Delete deletes from MinIO
func (ms *MinIOStorage) Delete(ctx context.Context, bucket, objectName string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := ms.client.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete from MinIO: %w", err)
	}

	return nil
}

// Exists checks if an object exists in MinIO
func (ms *MinIOStorage) Exists(ctx context.Context, bucket, objectName string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := ms.client.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		// Check if error is "not found"
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return false, nil
		}
		return false, fmt.Errorf("failed to check object existence: %w", err)
	}

	return true, nil
}

// ListObjects lists objects in a bucket with a prefix
func (ms *MinIOStorage) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	objectCh := ms.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	keys := make([]string, 0)
	for object := range objectCh {
		if object.Err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", object.Err)
		}
		keys = append(keys, object.Key)
	}

	return keys, nil
}

// PresignedURL generates a presigned URL for downloading
func (ms *MinIOStorage) PresignedURL(ctx context.Context, bucket, objectName string, expiration time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url, err := ms.client.PresignedGetObject(ctx, bucket, objectName, expiration, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return url.String(), nil
}

func (ms *MinIOStorage) PresignedUploadURL(ctx context.Context, bucket, objectName string, expiration time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url, err := ms.client.PresignedPutObject(ctx, bucket, objectName, expiration)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned upload URL: %w", err)
	}

	return url.String(), nil
}

// CreateBucket creates a new bucket
func (ms *MinIOStorage) CreateBucket(ctx context.Context, bucket string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	exists, err := ms.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if exists {
		return nil
	}

	if err := ms.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	return nil
}

// detectContentTypeByExt returns a MIME type based on the object name extension.
func detectContentTypeByExt(objectName string) string {
	if mt := mime.TypeByExtension(objectName); mt != "" {
		return mt
	}
	switch {
	case len(objectName) < 4:
		return "application/octet-stream"
	case string(objectName[len(objectName)-4:]) == ".ts":
		return "video/mp2t"
	case string(objectName[len(objectName)-5:]) == ".m3u8":
		return "application/vnd.apple.mpegurl"
	default:
		return "application/octet-stream"
	}
}

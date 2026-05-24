package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewS3Storage_NoCredentials(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region: "us-east-1",
	})
	require.NoError(t, err)
	assert.NotNil(t, s3s)
	assert.Equal(t, "us-east-1", s3s.region)
}

func TestNewS3Storage_WithCredentials(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
	})
	require.NoError(t, err)
	assert.NotNil(t, s3s)
}

func TestNewS3Storage_WithEndpoint(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)
	assert.NotNil(t, s3s)
}

func TestS3Config_Fields(t *testing.T) {
	cfg := S3Config{
		Region:          "us-west-2",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
		Endpoint:        "http://minio:9000",
	}
	assert.Equal(t, "us-west-2", cfg.Region)
	assert.Equal(t, "key", cfg.AccessKeyID)
	assert.Equal(t, "secret", cfg.SecretAccessKey)
	assert.Equal(t, "http://minio:9000", cfg.Endpoint)
}

func TestS3Storage_DeleteObjects_EmptyList(t *testing.T) {
	s3s := &S3Storage{}
	err := s3s.DeleteObjects(context.Background(), "bucket", []string{})
	assert.NoError(t, err)
}

func TestS3Storage_Upload_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil, uploader: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = s3s.Upload(ctx, "bucket", "key.mp4", []byte("data"))
	})
}

func TestS3Storage_DownloadStream_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = s3s.DownloadStream(ctx, "bucket", "key")
	})
}

func TestS3Storage_Download_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = s3s.Download(ctx, "bucket", "key")
	})
}

func TestS3Storage_Delete_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = s3s.Delete(ctx, "bucket", "key")
	})
}

func TestS3Storage_Exists_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = s3s.Exists(ctx, "bucket", "key")
	})
}

func TestS3Storage_ListObjects_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = s3s.ListObjects(ctx, "bucket", "prefix")
	})
}

func TestS3Storage_CreateBucket_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = s3s.CreateBucket(ctx, "bucket")
	})
}

func TestS3Storage_UploadWithMetadata_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = s3s.UploadWithMetadata(ctx, "bucket", "key", []byte("data"), nil)
	})
}

func TestS3Storage_UploadWithContentType_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = s3s.UploadWithContentType(ctx, "bucket", "key.mp4", []byte("data"), "video/mp4")
	})
}

func TestS3Storage_UploadStreamWithContentType_NilClient(t *testing.T) {
	s3s := &S3Storage{uploader: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = s3s.UploadStreamWithContentType(ctx, "bucket", "key.mp4", bytes.NewReader([]byte("data")), 4, "video/mp4")
	})
}

func TestS3Storage_PresignedURL_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = s3s.PresignedURL(ctx, "bucket", "key", time.Hour)
	})
}

func TestS3Storage_PresignedUploadURL_NilClient(t *testing.T) {
	s3s := &S3Storage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = s3s.PresignedUploadURL(ctx, "bucket", "key", time.Hour)
	})
}

func TestReadCloserWithCancelS3(t *testing.T) {
	content := []byte("s3 content")
	readCloser := io.NopCloser(bytes.NewReader(content))
	cancelCalled := false
	cancel := func() { cancelCalled = true }

	rc := &readCloserWithCancelS3{
		ReadCloser: readCloser,
		cancel:     cancel,
	}

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, content, data)

	err = rc.Close()
	require.NoError(t, err)
	assert.True(t, cancelCalled)
}

func TestMaxDownloadSize(t *testing.T) {
	assert.Equal(t, int64(1<<30), maxDownloadSize)
}

func TestS3Storage_UploadStream_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.UploadStream(ctx, "bucket", "key.mp4", bytes.NewReader([]byte("data")), 4)
	assert.Error(t, err)
}

func TestS3Storage_UploadWithMetadata_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.UploadWithMetadata(ctx, "bucket", "key", []byte("data"), nil)
	assert.Error(t, err)
}

func TestS3Storage_DownloadStream_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = s3s.DownloadStream(ctx, "bucket", "key")
	assert.Error(t, err)
}

func TestS3Storage_Download_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = s3s.Download(ctx, "bucket", "key")
	assert.Error(t, err)
}

func TestS3Storage_Delete_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.Delete(ctx, "bucket", "key")
	assert.Error(t, err)
}

func TestS3Storage_DeleteObjects_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.DeleteObjects(ctx, "bucket", []string{"key1", "key2"})
	assert.Error(t, err)
}

func TestS3Storage_Exists_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = s3s.Exists(ctx, "bucket", "key")
	assert.Error(t, err)
}

func TestS3Storage_ListObjects_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = s3s.ListObjects(ctx, "bucket", "prefix")
	assert.Error(t, err)
}

func TestS3Storage_UploadWithContentType_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.UploadWithContentType(ctx, "bucket", "key.mp4", []byte("data"), "video/mp4")
	assert.Error(t, err)
}

func TestS3Storage_UploadStreamWithContentType_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.UploadStreamWithContentType(ctx, "bucket", "key.mp4", bytes.NewReader([]byte("data")), 4, "video/mp4")
	assert.Error(t, err)
}

func TestS3Storage_CreateBucket_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.CreateBucket(ctx, "bucket")
	assert.Error(t, err)
}

func TestS3Storage_PresignedURL_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx := context.Background()

	url, err := s3s.PresignedURL(ctx, "bucket", "key", time.Hour)
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.NotEmpty(t, url)
	}
}

func TestS3Storage_PresignedUploadURL_WithRealClient(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx := context.Background()

	url, err := s3s.PresignedUploadURL(ctx, "bucket", "key", time.Hour)
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.NotEmpty(t, url)
	}
}

func TestS3Storage_Upload_DelegatesToUploadStream(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.Upload(ctx, "bucket", "key.mp4", []byte("data"))
	assert.Error(t, err)
}

func TestS3Storage_Download_ReadError(t *testing.T) {
	errReader := &errorReadCloserS3{err: errors.New("read failure")}

	rc := &readCloserWithCancelS3{ReadCloser: errReader, cancel: func() {}}
	buf := new(bytes.Buffer)
	_, err := io.Copy(buf, io.LimitReader(rc, maxDownloadSize+1))
	assert.Error(t, err)
}

func TestS3Storage_DeleteObjects_WithKeys(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.DeleteObjects(ctx, "bucket", []string{"key1", "key2"})
	assert.Error(t, err)
}

func TestS3Storage_CreateBucket_ErrorNotAlreadyExists(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s3s.CreateBucket(ctx, "bucket")
	assert.Error(t, err)
}

func TestS3Storage_Exists_ErrorPath(t *testing.T) {
	s3s, err := NewS3Storage(S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Endpoint:        "http://localhost:9000",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = s3s.Exists(ctx, "bucket", "key")
	assert.Error(t, err)
}

type errorReadCloserS3 struct {
	err error
}

func (e *errorReadCloserS3) Read(p []byte) (n int, err error) {
	return 0, e.err
}

func (e *errorReadCloserS3) Close() error {
	return nil
}

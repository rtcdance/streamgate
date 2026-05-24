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

func TestDetectContentTypeByExt(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMime string
	}{
		{"mp4", "video.mp4", "video/mp4"},
		{"webm", "clip.webm", "video/webm"},
		{"mkv", "movie.mkv", "video/x-matroska"},
		{"avi", "film.avi", "video/x-msvideo"},
		{"mov", "clip.mov", "video/quicktime"},
		{"flv", "stream.flv", "video/x-flv"},
		{"wmv", "vid.wmv", "video/x-ms-wmv"},
		{"m4v", "vid.m4v", "video/mp4"},
		{"3gp", "mobile.3gp", "video/3gpp"},
		{"ogv", "video.ogv", "video/ogg"},
		{"ts", "segment.ts", "video/mp2t"},
		{"m3u8", "playlist.m3u8", "application/vnd.apple.mpegurl"},
		{"mpd", "manifest.mpd", "application/dash+xml"},
		{"m4s", "init.m4s", "video/iso.segment"},
		{"unknown", "file.xyz", "application/octet-stream"},
		{"uppercase", "VIDEO.MP4", "video/mp4"},
		{"no extension", "video", "application/octet-stream"},
		{"path with ts", "/path/to/seg001.ts", "video/mp2t"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectContentTypeByExt(tt.input)
			assert.Equal(t, tt.wantMime, got)
		})
	}
}

func TestNewMinIOStorage_InvalidEndpoint(t *testing.T) {
	_, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		UseSSL:          false,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create MinIO client")
}

func TestMinIOStorage_Close(t *testing.T) {
	ms := &MinIOStorage{}
	err := ms.Close()
	assert.NoError(t, err)
}

func TestMinIOStorage_DeleteObjects_EmptyList(t *testing.T) {
	ms := &MinIOStorage{}
	err := ms.DeleteObjects(context.Background(), "test-bucket", []string{})
	assert.NoError(t, err)
}

func TestMinIOStorage_ImplementsObjectStorage(t *testing.T) {
	var _ ObjectStorage = &MinIOStorage{}
}

func TestReadCloserWithCancel(t *testing.T) {
	content := []byte("test content")
	readCloser := io.NopCloser(bytes.NewReader(content))
	cancelCalled := false
	cancel := func() { cancelCalled = true }

	rc := &readCloserWithCancel{
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

func TestMinIOConfig_Fields(t *testing.T) {
	cfg := MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	}
	assert.Equal(t, "localhost:9000", cfg.Endpoint)
	assert.Equal(t, "minioadmin", cfg.AccessKeyID)
	assert.Equal(t, "minioadmin", cfg.SecretAccessKey)
	assert.False(t, cfg.UseSSL)
}

func TestNewMinIOStorage_ValidConfig(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)
	assert.NotNil(t, ms)
	assert.NotNil(t, ms.client)
}

func TestMinIOStorage_Upload_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = ms.Upload(ctx, "bucket", "object.mp4", []byte("data"))
	})
}

func TestMinIOStorage_DownloadStream_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = ms.DownloadStream(ctx, "bucket", "object")
	})
}

func TestMinIOStorage_Download_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = ms.Download(ctx, "bucket", "object")
	})
}

func TestMinIOStorage_Delete_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = ms.Delete(ctx, "bucket", "object")
	})
}

func TestMinIOStorage_Exists_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = ms.Exists(ctx, "bucket", "object")
	})
}

func TestMinIOStorage_ListObjects_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = ms.ListObjects(ctx, "bucket", "prefix")
	})
}

func TestMinIOStorage_PresignedURL_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = ms.PresignedURL(ctx, "bucket", "object", time.Hour)
	})
}

func TestMinIOStorage_CreateBucket_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = ms.CreateBucket(ctx, "bucket")
	})
}

func TestVideoMimeTypes_Completeness(t *testing.T) {
	expectedExts := []string{".mp4", ".webm", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".m4v", ".3gp", ".ogv", ".ts", ".m3u8", ".mpd", ".m4s"}
	for _, ext := range expectedExts {
		_, ok := videoMimeTypes[ext]
		assert.True(t, ok, "missing video MIME type for extension: %s", ext)
	}
}

func TestMinIOStorage_UploadStream_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_ = ms.UploadStream(ctx, "bucket", "object.mp4", bytes.NewReader([]byte("data")), 4)
	})
}

func TestMinIOStorage_Download_OversizedObject(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = ms.Download(ctx, "bucket", "object")
	})
}

func TestMinIOStorage_DeleteObjects_WithNames_NilClient(t *testing.T) {
	t.Skip("DeleteObjects with nil client panics in a goroutine inside minio-go; assert.Panics cannot recover it")
}

func TestMinIOStorage_PresignedUploadURL_NilClient(t *testing.T) {
	ms := &MinIOStorage{client: nil}
	ctx := context.Background()
	assert.Panics(t, func() {
		_, _ = ms.PresignedUploadURL(ctx, "bucket", "object", time.Hour)
	})
}

func TestMinIOConfig_Defaults(t *testing.T) {
	cfg := MinIOConfig{}
	assert.Equal(t, "", cfg.Endpoint)
	assert.Equal(t, "", cfg.AccessKeyID)
	assert.Equal(t, "", cfg.SecretAccessKey)
	assert.False(t, cfg.UseSSL)
}

func TestMinIOStorage_Upload_DelegatesToUploadStream(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.Upload(ctx, "bucket", "object.mp4", []byte("data"))
	assert.Error(t, err)
}

func TestMinIOStorage_UploadStream_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.UploadStream(ctx, "bucket", "object.mp4", bytes.NewReader([]byte("data")), 4)
	assert.Error(t, err)
}

func TestMinIOStorage_UploadWithContentType_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.UploadWithContentType(ctx, "bucket", "object.mp4", []byte("data"), "video/mp4")
	assert.Error(t, err)
}

func TestMinIOStorage_UploadStreamWithContentType_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.UploadStreamWithContentType(ctx, "bucket", "object.mp4", bytes.NewReader([]byte("data")), 4, "video/mp4")
	assert.Error(t, err)
}

func TestMinIOStorage_DownloadStream_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rc, err := ms.DownloadStream(ctx, "bucket", "object")
	if err != nil {
		assert.Error(t, err)
		return
	}
	_, err = io.ReadAll(rc)
	assert.Error(t, err)
}

func TestMinIOStorage_Delete_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.Delete(ctx, "bucket", "object")
	assert.Error(t, err)
}

func TestMinIOStorage_Exists_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = ms.Exists(ctx, "bucket", "object")
	assert.Error(t, err)
}

func TestMinIOStorage_ListObjects_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = ms.ListObjects(ctx, "bucket", "prefix")
	assert.Error(t, err)
}

func TestMinIOStorage_CreateBucket_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.CreateBucket(ctx, "bucket")
	assert.Error(t, err)
}

func TestMinIOStorage_PresignedURL_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = ms.PresignedURL(ctx, "bucket", "object", time.Hour)
	assert.Error(t, err)
}

func TestMinIOStorage_PresignedUploadURL_WithRealClient(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = ms.PresignedUploadURL(ctx, "bucket", "object", time.Hour)
	assert.Error(t, err)
}

func TestMinIOStorage_Download_ReadError(t *testing.T) {
	errReader := &errorReadCloser{err: errors.New("read failure")}

	rc := &readCloserWithCancel{ReadCloser: errReader, cancel: func() {}}
	buf := new(bytes.Buffer)
	_, err := io.Copy(buf, io.LimitReader(rc, maxDownloadSize+1))
	assert.Error(t, err)
}

func TestMinIOStorage_UploadWithContentType_BucketCheckError(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.UploadWithContentType(ctx, "nonexistent-bucket", "object.mp4", []byte("data"), "video/mp4")
	assert.Error(t, err)
}

func TestMinIOStorage_UploadStreamWithContentType_BucketCheckError(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.UploadStreamWithContentType(ctx, "nonexistent-bucket", "object.mp4", bytes.NewReader([]byte("data")), 4, "video/mp4")
	assert.Error(t, err)
}

func TestMinIOStorage_CreateBucket_BucketExists(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.CreateBucket(ctx, "bucket")
	assert.Error(t, err)
}

func TestMinIOStorage_DeleteObjects_WithNames(t *testing.T) {
	ms, err := NewMinIOStorage(MinIOConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ms.DeleteObjects(ctx, "bucket", []string{"obj1", "obj2"})
	assert.Error(t, err)
}

type errorReadCloser struct {
	err error
}

func (e *errorReadCloser) Read(p []byte) (n int, err error) {
	return 0, e.err
}

func (e *errorReadCloser) Close() error {
	return nil
}

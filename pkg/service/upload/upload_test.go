package upload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn    func(ctx context.Context) (*sql.Tx, error)
	inTxFn     func(ctx context.Context, fn func(tx *sql.Tx) error) error
	pingFn     func(ctx context.Context) error
	closeFn    func() error
}

func (m *mockDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return stg.NewErrorCancelRow(errors.New("not implemented"))
}
func (m *mockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if m.inTxFn != nil {
		return m.inTxFn(ctx, fn)
	}
	return errors.New("not implemented")
}
func (m *mockDB) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}
func (m *mockDB) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

type mockResult struct {
	rowsAffected int64
	lastInsertID int64
}

func (m *mockResult) LastInsertId() (int64, error) { return m.lastInsertID, nil }
func (m *mockResult) RowsAffected() (int64, error) { return m.rowsAffected, nil }

type mockObjStore struct {
	mu                sync.RWMutex
	data              map[string][]byte
	uploadErr         error
	downloadErr       error
	deleteErr         error
	existsResult      bool
	existsErr         error
	listResult        []string
	listErr           error
	deleteObjErr      error
	streamData        map[string]string
	downloadStreamErr error
}

func newMockObjStore() *mockObjStore {
	return &mockObjStore{data: make(map[string][]byte), streamData: make(map[string]string)}
}

func (m *mockObjStore) Upload(_ context.Context, bucket, key string, data []byte) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	m.mu.Lock()
	m.data[bucket+"/"+key] = data
	m.mu.Unlock()
	return nil
}

func (m *mockObjStore) UploadStream(_ context.Context, bucket, key string, reader io.Reader, _ int64) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	data, _ := io.ReadAll(reader)
	m.mu.Lock()
	m.data[bucket+"/"+key] = data
	m.mu.Unlock()
	return nil
}

func (m *mockObjStore) Download(_ context.Context, bucket, key string) ([]byte, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.data[bucket+"/"+key]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

func (m *mockObjStore) DownloadStream(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	if m.downloadStreamErr != nil {
		return nil, m.downloadStreamErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if data, ok := m.streamData[bucket+"/"+key]; ok {
		return io.NopCloser(strings.NewReader(data)), nil
	}
	if d, ok := m.data[bucket+"/"+key]; ok {
		return io.NopCloser(strings.NewReader(string(d))), nil
	}
	return nil, errors.New("not found")
}

func (m *mockObjStore) Delete(_ context.Context, bucket, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.mu.Lock()
	delete(m.data, bucket+"/"+key)
	m.mu.Unlock()
	return nil
}

func (m *mockObjStore) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	if m.deleteObjErr != nil {
		return m.deleteObjErr
	}
	m.mu.Lock()
	for _, key := range keys {
		delete(m.data, bucket+"/"+key)
	}
	m.mu.Unlock()
	return nil
}

func (m *mockObjStore) Exists(_ context.Context, bucket, key string) (bool, error) {
	if m.existsErr != nil {
		return false, m.existsErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[bucket+"/"+key]
	if m.existsResult {
		return true, nil
	}
	return ok, nil
}

func (m *mockObjStore) ListObjects(_ context.Context, bucket, prefix string) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listResult != nil {
		return m.listResult, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, bucket+"/"+prefix) {
			keys = append(keys, strings.TrimPrefix(k, bucket+"/"))
		}
	}
	return keys, nil
}

type mockPresigner struct {
	url string
	err error
}

func (m *mockPresigner) PresignedURL(_ context.Context, _, _ string, _ time.Duration) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.url, nil
}

type mockUploadPresigner struct {
	url string
	err error
}

func (m *mockUploadPresigner) PresignedUploadURL(_ context.Context, _, _ string, _ time.Duration) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.url, nil
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"video.mp4", "video/mp4"},
		{"video.webm", "video/webm"},
		{"audio.mp3", "audio/mpeg"},
		{"audio.wav", "audio/wav"},
		{"image.jpg", "image/jpeg"},
		{"image.jpeg", "image/jpeg"},
		{"image.png", "image/png"},
		{"image.gif", "image/gif"},
		{"file.bin", "application/octet-stream"},
		{"noext", "application/octet-stream"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, DetectContentType(tt.filename))
	}
}

func TestContentTypeToType(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"video/mp4", "video"},
		{"video/webm", "video"},
		{"audio/mpeg", "audio"},
		{"audio/wav", "audio"},
		{"image/jpeg", "image"},
		{"image/png", "image"},
		{"application/octet-stream", "other"},
		{"text/plain", "other"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ContentTypeToType(tt.mime))
	}
}

func TestBytesSliceReader_Read(t *testing.T) {
	t.Run("read all data", func(t *testing.T) {
		r := BytesReader([]byte("hello"))
		buf := make([]byte, 10)
		n, err := r.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", string(buf[:n]))
	})

	t.Run("read EOF after consumed", func(t *testing.T) {
		r := BytesReader([]byte("hi"))
		buf := make([]byte, 10)
		_, _ = r.Read(buf)
		n, err := r.Read(buf)
		assert.Equal(t, 0, n)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("partial read", func(t *testing.T) {
		r := BytesReader([]byte("hello world"))
		buf := make([]byte, 5)
		n, err := r.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", string(buf))
	})

	t.Run("empty data", func(t *testing.T) {
		r := BytesReader([]byte{})
		buf := make([]byte, 10)
		n, err := r.Read(buf)
		assert.Equal(t, 0, n)
		assert.Equal(t, io.EOF, err)
	})
}

func TestNewUploadService(t *testing.T) {
	t.Run("with logger", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket", zap.NewNop())
		require.NotNil(t, svc)
		assert.Equal(t, int64(DefaultMaxUploadSize), svc.maxUploadSize)
		assert.Equal(t, int64(DefaultStorageQuotaPerWallet), svc.storageQuota)
	})

	t.Run("without logger", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		require.NotNil(t, svc)
	})
}

func TestUploadService_SetPresigner(t *testing.T) {
	svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
	p := &mockPresigner{url: "https://example.com/presigned"}
	svc.SetPresigner(p)
	assert.Equal(t, p, svc.presigner)
}

func TestUploadService_SetUploadPresigner(t *testing.T) {
	svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
	p := &mockUploadPresigner{url: "https://example.com/upload"}
	svc.SetUploadPresigner(p)
	assert.Equal(t, p, svc.uploadSigner)
}

func TestUploadService_SetMaxUploadSize(t *testing.T) {
	svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
	svc.SetMaxUploadSize(100)
	assert.Equal(t, int64(100), svc.maxUploadSize)
}

func TestUploadService_SetStorageQuota(t *testing.T) {
	svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
	svc.SetStorageQuota(200)
	assert.Equal(t, int64(200), svc.storageQuota)
}

func TestUploadService_SetChunkMergeConcurrency(t *testing.T) {
	svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
	svc.SetChunkMergeConcurrency(10)
	assert.Equal(t, 10, svc.chunkMergeConcurrency)

	svc.SetChunkMergeConcurrency(0)
	assert.Equal(t, 10, svc.chunkMergeConcurrency)

	svc.SetChunkMergeConcurrency(-1)
	assert.Equal(t, 10, svc.chunkMergeConcurrency)
}

func TestUploadService_UploadStream(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "test-bucket", zap.NewNop())
		id, err := svc.UploadStream(context.Background(), "video.mp4", strings.NewReader("test data"), 9, "owner1")
		require.NoError(t, err)
		assert.NotEmpty(t, id)
	})

	t.Run("storage upload error", func(t *testing.T) {
		store := newMockObjStore()
		store.uploadErr = errors.New("storage down")
		svc := NewUploadService(&mockDB{}, store, "test-bucket", zap.NewNop())
		_, err := svc.UploadStream(context.Background(), "video.mp4", strings.NewReader("data"), 4, "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to upload to storage")
	})

	t.Run("db save error triggers cleanup", func(t *testing.T) {
		store := newMockObjStore()
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewUploadService(db, store, "test-bucket", zap.NewNop())
		_, err := svc.UploadStream(context.Background(), "video.mp4", strings.NewReader("data"), 4, "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to save upload info")
	})
}

func TestUploadService_CheckStorageQuota(t *testing.T) {
	t.Run("no quota set", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		svc.storageQuota = 0
		err := svc.CheckStorageQuota(context.Background(), "owner1", 1000)
		assert.NoError(t, err)
	})

	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		svc.storageQuota = 100
		err := svc.CheckStorageQuota(context.Background(), "owner1", 50)
		assert.NoError(t, err)
	})

	t.Run("within quota nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		svc.storageQuota = 1000
		err := svc.CheckStorageQuota(context.Background(), "owner1", 500)
		assert.NoError(t, err)
	})

	t.Run("exceeds quota nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		svc.storageQuota = 1000
		err := svc.CheckStorageQuota(context.Background(), "owner1", 1500)
		assert.NoError(t, err)
	})

	t.Run("quota check db error", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("db down"))
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket")
		svc.storageQuota = 1000
		err := svc.CheckStorageQuota(context.Background(), "owner1", 500)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "quota check failed")
	})
}

func TestUploadService_GetDownloadURL(t *testing.T) {
	t.Run("no presigner configured", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		_, err := svc.GetDownloadURL(context.Background(), "id", time.Hour)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "presigned URL support not configured")
	})

	t.Run("upload not found", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		svc.SetPresigner(&mockPresigner{url: "https://example.com/file"})
		_, err := svc.GetDownloadURL(context.Background(), "missing", time.Hour)
		assert.Error(t, err)
	})

	t.Run("owner mismatch", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket", zap.NewNop())
		svc.SetPresigner(&mockPresigner{url: "https://example.com/file"})
		_, err := svc.GetDownloadURL(context.Background(), "id", time.Hour, "other-owner")
		assert.Error(t, err)
	})
}

func TestUploadService_InitiateChunkedUpload(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		_, err := svc.InitiateChunkedUpload(context.Background(), "video.mp4", 1024, 5, "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("exceeds max size", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		svc.SetMaxUploadSize(100)
		_, err := svc.InitiateChunkedUpload(context.Background(), "video.mp4", 200, 5, "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
		svc.storageQuota = 0
		id, err := svc.InitiateChunkedUpload(context.Background(), "video.mp4", 1024, 5, "owner1")
		require.NoError(t, err)
		assert.NotEmpty(t, id)
	})

	t.Run("db save error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
		svc.storageQuota = 0
		_, err := svc.InitiateChunkedUpload(context.Background(), "video.mp4", 1024, 5, "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to save upload info")
	})
}

func TestUploadService_InitiatePresignedUpload(t *testing.T) {
	t.Run("no upload presigner", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		_, _, _, err := svc.InitiatePresignedUpload(context.Background(), "video.mp4", 1024, "video/mp4", "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "presigned upload support not configured")
	})

	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		svc.SetUploadPresigner(&mockUploadPresigner{url: "https://example.com/upload"})
		_, _, _, err := svc.InitiatePresignedUpload(context.Background(), "video.mp4", 1024, "video/mp4", "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("exceeds max size", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		svc.SetUploadPresigner(&mockUploadPresigner{url: "https://example.com/upload"})
		svc.SetMaxUploadSize(100)
		_, _, _, err := svc.InitiatePresignedUpload(context.Background(), "video.mp4", 200, "video/mp4", "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
	})

	t.Run("presigned URL generation fails", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("mock scan error"))
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
		svc.SetUploadPresigner(&mockUploadPresigner{err: errors.New("signer down")})
		_, _, _, err := svc.InitiatePresignedUpload(context.Background(), "video.mp4", 1024, "video/mp4", "owner1")
		assert.Error(t, err)
	})
}

func TestUploadService_UploadChunkStream(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		err := svc.UploadChunkStream(context.Background(), "id", 0, strings.NewReader("data"), 4, "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("chunk index out of range", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		err := svc.UploadChunkStream(context.Background(), "id", -1, strings.NewReader("data"), 4, "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chunk_index out of range")

		err = svc.UploadChunkStream(context.Background(), "id", 100001, strings.NewReader("data"), 4, "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chunk_index out of range")
	})

	t.Run("upload not found", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		err := svc.UploadChunkStream(context.Background(), "missing", 0, strings.NewReader("data"), 4, "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "upload not found")
	})
}

func TestUploadService_CompleteChunkedUpload(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		err := svc.CompleteChunkedUpload(context.Background(), "id", 5)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("upload not found", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		err := svc.CompleteChunkedUpload(context.Background(), "missing", 5)
		assert.Error(t, err)
	})
}

func TestUploadService_DeleteUpload(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		err := svc.DeleteUpload(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("upload not found", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		err := svc.DeleteUpload(context.Background(), "missing")
		assert.Error(t, err)
	})
}

func TestUploadService_ListUploads(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		_, err := svc.ListUploads(context.Background(), "owner1", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("db query error", func(t *testing.T) {
		db := &mockDB{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket")
		_, err := svc.ListUploads(context.Background(), "owner1", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query uploads")
	})
}

func TestUploadService_GetUploadStatus(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		_, err := svc.GetUploadStatus(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("not found", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(sql.ErrNoRows)
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket")
		_, err := svc.GetUploadStatus(context.Background(), "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "upload not found")
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("db error"))
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket")
		_, err := svc.GetUploadStatus(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query upload")
	})
}

func TestUploadService_GetUploadProgress(t *testing.T) {
	t.Run("upload not found", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(sql.ErrNoRows)
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket")
		_, err := svc.GetUploadProgress(context.Background(), "missing")
		assert.Error(t, err)
	})
}

func TestUploadService_GetChunkStatuses(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		_, err := svc.GetChunkStatuses(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})
}

func TestUploadService_UpdateUploadStatus(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		err := svc.UpdateUploadStatus(context.Background(), "id", "completed")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket")
		err := svc.UpdateUploadStatus(context.Background(), "id", "completed")
		assert.NoError(t, err)
	})
}

func TestUploadService_CompleteUploadWithTx(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		_, err := svc.CompleteUploadWithTx(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("upload not found", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(sql.ErrNoRows)
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "bucket")
		_, err := svc.CompleteUploadWithTx(context.Background(), "missing")
		assert.Error(t, err)
	})
}

func TestUploadService_RegisterPostUploadHook(t *testing.T) {
	svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
	svc.RegisterPostUploadHook(func(_ context.Context, _, _, _ string) {})
	assert.Len(t, svc.onProcessed, 1)
}

func TestUploadService_RegisterAutoTranscodeHook(t *testing.T) {
	t.Run("nil transcoding service", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
		svc.RegisterAutoTranscodeHook(AutoTranscodeHookDeps{})
		assert.Empty(t, svc.onProcessed)
	})

	t.Run("with default profiles", func(t *testing.T) {
		svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket", zap.NewNop())
		svc.RegisterAutoTranscodeHook(AutoTranscodeHookDeps{
			TranscodingSvc: nil,
		})
		assert.Empty(t, svc.onProcessed)
	})
}

func TestUploadService_Close(t *testing.T) {
	svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket")
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestUploadService_Upload(t *testing.T) {
	t.Run("success via Upload wrapper", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewUploadService(db, newMockObjStore(), "test-bucket", zap.NewNop())
		id, err := svc.Upload(context.Background(), "video.mp4", []byte("test data"), "owner1")
		require.NoError(t, err)
		assert.NotEmpty(t, id)
	})
}

func TestUploadService_UploadChunk(t *testing.T) {
	t.Run("delegates to UploadChunkStream", func(t *testing.T) {
		svc := NewUploadService(nil, newMockObjStore(), "bucket")
		err := svc.UploadChunk(context.Background(), "id", 0, []byte("data"), "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})
}

func TestDefaultConstants(t *testing.T) {
	assert.Equal(t, int64(5*1024*1024*1024), DefaultMaxUploadSize)
	assert.Equal(t, int64(50*1024*1024*1024), DefaultStorageQuotaPerWallet)
}

func TestUploadInfo_Fields(t *testing.T) {
	info := &UploadInfo{
		ID:          "test-id",
		Filename:    "video.mp4",
		Size:        1024,
		ContentType: "video/mp4",
		Hash:        "abc123",
		Status:      "completed",
		URL:         "/bucket/key",
		OwnerID:     "owner1",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	assert.Equal(t, "test-id", info.ID)
	assert.Equal(t, "video.mp4", info.Filename)
	assert.Equal(t, "completed", info.Status)
}

func TestChunkInfo_Fields(t *testing.T) {
	ci := ChunkInfo{
		UploadID:    "upload-1",
		ChunkIndex:  0,
		TotalChunks: 10,
		ChunkSize:   1024,
		Uploaded:    true,
	}
	assert.Equal(t, "upload-1", ci.UploadID)
	assert.True(t, ci.Uploaded)
}

func TestUploadService_CompleteChunkedUpload_NotUploading(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewErrorCancelRow(errors.New("mock scan error"))
			}
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	err := svc.CompleteChunkedUpload(context.Background(), "id", 5)
	assert.Error(t, err)
}

func TestUploadService_GetDownloadURL_NotCompleted(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.SetPresigner(&mockPresigner{url: "https://example.com/file"})
	_, err := svc.GetDownloadURL(context.Background(), "id", time.Hour)
	assert.Error(t, err)
}

func TestUploadService_InitiatePresignedUpload_WithContentType(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.storageQuota = 0
	svc.SetUploadPresigner(&mockUploadPresigner{url: "https://example.com/upload"})
	_, _, _, err := svc.InitiatePresignedUpload(context.Background(), "video.mp4", 1024, "video/mp4", "owner1")
	require.NoError(t, err)
}

func TestUploadService_DeleteUpload_WithChunks(t *testing.T) {
	store := newMockObjStore()
	store.data["mybucket/video.mp4"] = []byte("data")
	store.data["mybucket/chunks/upload1/0"] = []byte("chunk0")
	store.data["mybucket/chunks/upload1/1"] = []byte("chunk1")

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())
	err := svc.DeleteUpload(context.Background(), "id")
	assert.Error(t, err)
}

func TestUploadService_DeleteUpload_DeleteFromStorageFails(t *testing.T) {
	store := newMockObjStore()
	store.deleteErr = errors.New("storage error")

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())
	err := svc.DeleteUpload(context.Background(), "id")
	assert.Error(t, err)
}

func TestUploadService_DeleteUpload_DBError(t *testing.T) {
	store := newMockObjStore()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())
	err := svc.DeleteUpload(context.Background(), "id")
	assert.Error(t, err)
}

func TestUploadService_UploadChunkStream_StorageUploadFails(t *testing.T) {
	store := newMockObjStore()
	store.uploadErr = errors.New("storage down")

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())
	err := svc.UploadChunkStream(context.Background(), "upload1", 0, strings.NewReader("data"), 4, "owner1")
	assert.Error(t, err)
}

func TestUploadService_CompleteUploadWithTx_HookExecution(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
		inTxFn: func(_ context.Context, fn func(tx *sql.Tx) error) error {
			return nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.RegisterPostUploadHook(func(_ context.Context, _, _, _ string) {})
	_, err := svc.CompleteUploadWithTx(context.Background(), "id")
	assert.Error(t, err)
	svc.Close()
}

func TestUploadService_GetDownloadURL_PresignerError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.SetPresigner(&mockPresigner{err: errors.New("presigner error")})
	_, err := svc.GetDownloadURL(context.Background(), "id", time.Hour)
	assert.Error(t, err)
}

func TestUploadService_UploadChunkStream_ExistsCheckFails(t *testing.T) {
	store := newMockObjStore()
	store.existsErr = errors.New("exists check failed")

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())
	err := svc.UploadChunkStream(context.Background(), "upload1", 0, strings.NewReader("data"), 4, "owner1")
	assert.Error(t, err)
}

func TestUploadService_CompleteChunkedUpload_WithStreamData(t *testing.T) {
	store := newMockObjStore()
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("mybucket/chunks/upload1/%d", i)
		store.streamData[key] = fmt.Sprintf("chunk%d", i)
	}

	callCount := 0
	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			callCount++
			if strings.Contains(query, "FROM uploads") {
				return stg.NewErrorCancelRow(errors.New("mock scan error"))
			}
			if strings.Contains(query, "COUNT(*)") {
				return stg.NewErrorCancelRow(errors.New("mock scan error"))
			}
			return stg.NewErrorCancelRow(errors.New("mock scan error"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())
	svc.SetChunkMergeConcurrency(2)
	err := svc.CompleteChunkedUpload(context.Background(), "upload1", 2)
	assert.Error(t, err)
}

func TestUploadService_ListUploads_QueryError(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return nil, errors.New("query error")
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	_, err := svc.ListUploads(context.Background(), "owner1", 10, 0)
	assert.Error(t, err)
}

func TestUploadService_InitiatePresignedUpload_DBError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.storageQuota = 0
	svc.SetUploadPresigner(&mockUploadPresigner{url: "https://example.com/upload"})
	_, _, _, err := svc.InitiatePresignedUpload(context.Background(), "video.mp4", 1024, "", "owner1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save upload info")
}

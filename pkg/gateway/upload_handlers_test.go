package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/service"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type uploadMockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn    func(ctx context.Context) (*sql.Tx, error)
	inTxFn     func(ctx context.Context, fn func(tx *sql.Tx) error) error
}

func (m *uploadMockDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *uploadMockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return stg.NewErrorCancelRow(errors.New("not implemented"))
}
func (m *uploadMockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *uploadMockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return nil, errors.New("not implemented")
}
func (m *uploadMockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if m.inTxFn != nil {
		return m.inTxFn(ctx, fn)
	}
	return errors.New("not implemented")
}
func (m *uploadMockDB) Ping(ctx context.Context) error { return nil }
func (m *uploadMockDB) Close() error                   { return nil }

type uploadMockResult struct {
	rowsAffected int64
}

func (m *uploadMockResult) LastInsertId() (int64, error) { return 0, nil }
func (m *uploadMockResult) RowsAffected() (int64, error) { return m.rowsAffected, nil }

type uploadMockObjStore struct {
	data map[string][]byte
}

func newUploadMockObjStore() *uploadMockObjStore {
	return &uploadMockObjStore{data: make(map[string][]byte)}
}

func (m *uploadMockObjStore) Upload(_ context.Context, bucket, key string, data []byte) error {
	m.data[bucket+"/"+key] = data
	return nil
}
func (m *uploadMockObjStore) UploadStream(_ context.Context, bucket, key string, reader io.Reader, size int64) error {
	data, _ := io.ReadAll(reader)
	m.data[bucket+"/"+key] = data
	return nil
}
func (m *uploadMockObjStore) Download(_ context.Context, bucket, key string) ([]byte, error) {
	d, ok := m.data[bucket+"/"+key]
	if !ok {
		return nil, errors.New("not found")
	}
	return d, nil
}
func (m *uploadMockObjStore) DownloadStream(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	d, ok := m.data[bucket+"/"+key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(d)), nil
}
func (m *uploadMockObjStore) Delete(_ context.Context, bucket, key string) error {
	delete(m.data, bucket+"/"+key)
	return nil
}
func (m *uploadMockObjStore) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	for _, k := range keys {
		delete(m.data, bucket+"/"+k)
	}
	return nil
}
func (m *uploadMockObjStore) Exists(_ context.Context, bucket, key string) (bool, error) {
	_, ok := m.data[bucket+"/"+key]
	return ok, nil
}
func (m *uploadMockObjStore) ListObjects(_ context.Context, bucket, prefix string) ([]string, error) {
	return nil, nil
}

func setupUploadRouter(uploadSvc *service.UploadService, wallet string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if wallet != "" {
			c.Set("wallet_address", wallet)
		}
		c.Next()
	})
	RegisterUploadRoutes(r, zap.NewNop(), uploadSvc)
	return r
}

func createMultipartUpload(filename string, content []byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", filename)
	_, _ = part.Write(content)
	_ = writer.Close()
	return &buf, writer.FormDataContentType()
}

func createMultipartChunk(uploadID string, chunkIndex int, content []byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("upload_id", uploadID)
	_ = writer.WriteField("chunk_index", fmt.Sprintf("%d", chunkIndex))
	part, _ := writer.CreateFormFile("chunk", fmt.Sprintf("chunk_%d", chunkIndex))
	_, _ = part.Write(content)
	_ = writer.Close()
	return &buf, writer.FormDataContentType()
}

func TestUploadHandlers_NilService(t *testing.T) {
	r := setupUploadRouter(nil, "0xOwner")

	t.Run("POST /upload returns 503", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("POST /upload/init returns 503", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"filename":"test.mp4","total_size":1000,"total_chunks":1}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("GET /upload/list returns 503", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/list", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("GET /upload/:id/status returns 503", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/status", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("GET /upload/:id/download-url returns 503", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/download-url", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("DELETE /upload/:id returns 503", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodDelete, "/api/v1/upload/test-id", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestUploadHandlers_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	t.Run("POST /upload returns 401", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("POST /upload/init returns 401", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"filename":"test.mp4","total_size":1000,"total_chunks":1}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("GET /upload/list returns 401", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/list", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestUploadHandlers_ChunkedInit_Validation(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	t.Run("missing fields", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"filename":"test.mp4"}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid body", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader("not json"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("negative total_size", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"filename":"test.mp4","total_size":-1,"total_chunks":1}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("negative total_chunks", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"filename":"test.mp4","total_size":1000,"total_chunks":-1}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("too many chunks", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"filename":"test.mp4","total_size":1000,"total_chunks":10001}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid extension", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"filename":"test.exe","total_size":1000,"total_chunks":1}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestUploadHandlers_ChunkedInit_Success(t *testing.T) {
	db := &uploadMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &uploadMockResult{}, nil
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	svc.SetStorageQuota(0)
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	body := `{"filename":"test.mp4","total_size":1000,"total_chunks":5}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp["upload_id"])
	assert.Equal(t, "uploading", resp["status"])
	assert.Equal(t, float64(5), resp["total_chunks"])
}

func TestUploadHandlers_UploadStatus_NotFound(t *testing.T) {
	db := &uploadMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/status", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_UploadStatus_InvalidID(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/../etc/status", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_UploadStatus_NotOwner(t *testing.T) {
	db := &uploadMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOtherOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/status", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_DeleteUpload_InvalidID(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/upload/../etc", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_DeleteUpload_NotFound(t *testing.T) {
	db := &uploadMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/upload/test-id", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_ChunkStatuses_InvalidID(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/../etc/chunks", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_ChunkStatuses_NotFound(t *testing.T) {
	db := &uploadMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/chunks", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_ChunkedUploadComplete_InvalidID(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	body := `{"total_chunks":5}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/../etc/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_ChunkedUploadComplete_NotFound(t *testing.T) {
	db := &uploadMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	body := `{"total_chunks":5}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/test-id/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_CompleteUpload_InvalidID(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/../etc/complete-upload", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_CompleteUpload_NotFound(t *testing.T) {
	db := &uploadMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/test-id/complete-upload", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_DownloadURL_InvalidID(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/../etc/download-url", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_DownloadURL_NoPresigner(t *testing.T) {
	db := &uploadMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/download-url", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUploadHandlers_PresignedInit_InvalidBody(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/presigned-init", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_PresignedInit_NegativeSize(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	body := `{"filename":"test.mp4","total_size":-1}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/presigned-init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_PresignedInit_NoPresigner(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	body := `{"filename":"test.mp4","total_size":1000}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/presigned-init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUploadHandlers_CompletePresigned_InvalidID(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/../etc/complete-presigned", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_UploadList_DBError(t *testing.T) {
	db := &uploadMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/list", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUploadHandlers_UploadList_QueryParams(t *testing.T) {
	db := &uploadMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/list?limit=10&offset=5", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUploadHandlers_WholeFile_NoFile(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload", http.NoBody)
	req.Header.Set("Content-Type", "multipart/form-data")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_ChunkUpload_MissingFields(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("upload_id", "test-id")
	_ = writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/chunk", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_ChunkUpload_InvalidObjectKey(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("upload_id", "../etc/passwd")
	_ = writer.WriteField("chunk_index", "0")
	_ = writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/chunk", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_ChunkUpload_InvalidChunkIndex(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("upload_id", "test-id")
	_ = writer.WriteField("chunk_index", "abc")
	_ = writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/chunk", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_ChunkUpload_NegativeChunkIndex(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("upload_id", "test-id")
	_ = writer.WriteField("chunk_index", "-1")
	_ = writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/chunk", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_BatchChunkUpload_InvalidID(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/a..b/batch-chunks", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_BatchChunkUpload_InvalidMultipart(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/test-id/batch-chunks", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFormatAllowedExtensions(t *testing.T) {
	result := formatAllowedExtensions()
	assert.Contains(t, result, ".mp4")
	assert.Contains(t, result, ".webm")
	assert.Contains(t, result, ".avi")
}

func TestByteReader(t *testing.T) {
	t.Run("read all", func(t *testing.T) {
		br := bytesReader([]byte{1, 2, 3})
		buf := make([]byte, 3)
		n, err := br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 3, n)
		assert.Equal(t, []byte{1, 2, 3}, buf)
	})

	t.Run("read past end", func(t *testing.T) {
		br := bytesReader([]byte{1})
		buf := make([]byte, 2)
		n, _ := br.Read(buf)
		assert.Equal(t, 1, n)
		n, err := br.Read(buf)
		assert.Equal(t, 0, n)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("empty", func(t *testing.T) {
		br := bytesReader([]byte{})
		buf := make([]byte, 1)
		_, err := br.Read(buf)
		assert.Equal(t, io.EOF, err)
	})
}

func TestUploadHandlers_PresignedInit_InvalidExtension(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	body := `{"filename":"test.exe","total_size":1000}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/presigned-init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_ChunkedUploadComplete_MissingBody(t *testing.T) {
	db := &uploadMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/test-id/complete", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUploadHandlers_DownloadURL_ExpiryParam(t *testing.T) {
	db := &uploadMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewUploadService(db, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/download-url?expiry_minutes=30", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUploadHandlers_UploadList_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/list", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_WholeFile_InvalidExtension(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	mp4Header := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D}
	buf, contentType := createMultipartUpload("test.exe", mp4Header)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload", buf)
	req.Header.Set("Content-Type", contentType)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_WholeFile_UnsupportedFormat(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	textContent := []byte("this is not a video file at all")
	buf, contentType := createMultipartUpload("test.mp4", textContent)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload", buf)
	req.Header.Set("Content-Type", contentType)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadHandlers_PresignedInit_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	body := `{"filename":"test.mp4","total_size":1000}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/presigned-init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_CompletePresigned_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/test-id/complete-presigned", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_CompleteUpload_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/test-id/complete-upload", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_DeleteUpload_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/upload/test-id", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_ChunkStatuses_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/chunks", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_BatchChunkUpload_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/test-id/batch-chunks", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_ChunkUpload_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/chunk", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_DownloadURL_NoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/download-url", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_StatusNoWallet(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/upload/test-id/status", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandlers_PresignedInit_SizeTooLarge(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"filename":"test.mp4","total_size":%d}`, int64(600*1024*1024))
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/presigned-init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestUploadHandlers_ChunkedInit_SizeTooLarge(t *testing.T) {
	svc := service.NewUploadService(&uploadMockDB{}, newUploadMockObjStore(), "bucket")
	r := setupUploadRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"filename":"test.mp4","total_size":%d,"total_chunks":1}`, int64(600*1024*1024))
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/upload/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

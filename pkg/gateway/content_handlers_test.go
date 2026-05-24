package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/service"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type contentMockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn    func(ctx context.Context) (*sql.Tx, error)
	inTxFn     func(ctx context.Context, fn func(tx *sql.Tx) error) error
}

func (m *contentMockDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *contentMockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return stg.NewErrorCancelRow(errors.New("not implemented"))
}
func (m *contentMockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *contentMockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return nil, errors.New("not implemented")
}
func (m *contentMockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if m.inTxFn != nil {
		return m.inTxFn(ctx, fn)
	}
	return errors.New("not implemented")
}
func (m *contentMockDB) Ping(ctx context.Context) error { return nil }
func (m *contentMockDB) Close() error                   { return nil }

type contentMockResult struct {
	rowsAffected int64
}

func (m *contentMockResult) LastInsertId() (int64, error) { return 0, nil }
func (m *contentMockResult) RowsAffected() (int64, error) { return m.rowsAffected, nil }

type contentMockCache struct {
	data map[string]interface{}
}

func newContentMockCache() *contentMockCache {
	return &contentMockCache{data: make(map[string]interface{})}
}

func (m *contentMockCache) Get(key string) (interface{}, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("cache miss: %s", key)
	}
	return v, nil
}
func (m *contentMockCache) Set(key string, value interface{}) error {
	m.data[key] = value
	return nil
}
func (m *contentMockCache) SetWithExpiration(key string, value interface{}, _ time.Duration) error {
	return m.Set(key, value)
}
func (m *contentMockCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

type contentMockObjStore struct {
	data map[string][]byte
}

func newContentMockObjStore() *contentMockObjStore {
	return &contentMockObjStore{data: make(map[string][]byte)}
}

func (m *contentMockObjStore) Upload(_ context.Context, bucket, key string, data []byte) error {
	m.data[bucket+"/"+key] = data
	return nil
}
func (m *contentMockObjStore) Download(_ context.Context, bucket, key string) ([]byte, error) {
	d, ok := m.data[bucket+"/"+key]
	if !ok {
		return nil, errors.New("not found")
	}
	return d, nil
}
func (m *contentMockObjStore) Delete(_ context.Context, bucket, key string) error {
	delete(m.data, bucket+"/"+key)
	return nil
}
func (m *contentMockObjStore) Exists(_ context.Context, bucket, key string) (bool, error) {
	_, ok := m.data[bucket+"/"+key]
	return ok, nil
}

func setupContentRouter(contentSvc interface{}, wallet string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if wallet != "" {
			c.Set("wallet_address", wallet)
		}
		c.Next()
	})
	var svc *service.ContentService
	if cs, ok := contentSvc.(*service.ContentService); ok {
		svc = cs
	}
	RegisterContentRoutes(r, zap.NewNop(), svc)
	return r
}

func TestHandleListContents_NilService(t *testing.T) {
	r := setupContentRouter(nil, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleListContents_NoWallet(t *testing.T) {
	db := &contentMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("should not be called")
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleListContents_DBError(t *testing.T) {
	db := &contentMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleListContents_QueryParams(t *testing.T) {
	var capturedArgs []interface{}
	db := &contentMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			capturedArgs = args
			return nil, errors.New("db error")
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content?limit=50&offset=10", http.NoBody)
	r.ServeHTTP(w, req)
	assert.NotNil(t, capturedArgs)
}

func TestHandleListContents_InvalidLimit(t *testing.T) {
	db := &contentMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content?limit=abc&offset=-5", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetContent_NilService(t *testing.T) {
	r := setupContentRouter(nil, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/test-id", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleGetContent_NotFound(t *testing.T) {
	db := &contentMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/missing-id", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleGetContent_NotOwner(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "0xDifferentOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/c1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleGetContent_Owner(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "0xOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/c1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	contentMap, ok := resp["content"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "c1", contentMap["id"])
}

func TestHandleCreateContent_NilService(t *testing.T) {
	r := setupContentRouter(nil, "0xOwner")
	body := `{"title":"test","type":"video"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleCreateContent_InvalidBody(t *testing.T) {
	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateContent_MissingTitle(t *testing.T) {
	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"type":"video"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateContent_TitleTooLong(t *testing.T) {
	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	longTitle := strings.Repeat("a", 256)
	body := fmt.Sprintf(`{"title":"%s","type":"video"}`, longTitle)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateContent_MissingType(t *testing.T) {
	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"test"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateContent_InvalidType(t *testing.T) {
	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"test","type":"invalid"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateContent_NegativeDuration(t *testing.T) {
	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"test","type":"video","duration":-1}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateContent_NegativeSize(t *testing.T) {
	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"test","type":"video","size":-1}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateContent_ValidTypes(t *testing.T) {
	tests := []struct {
		name string
		typ  string
	}{
		{"video", "video"},
		{"audio", "audio"},
		{"image", "image"},
		{"document", "document"},
		{"livestream", "livestream"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &contentMockDB{
				execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
					return &contentMockResult{}, nil
				},
			}
			svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
			r := setupContentRouter(svc, "0xOwner")
			body := fmt.Sprintf(`{"title":"test","type":"%s"}`, tt.typ)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusCreated, w.Code)
		})
	}
}

func TestHandleCreateContent_DBError(t *testing.T) {
	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"test","type":"video"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleCreateContent_WithMetadata(t *testing.T) {
	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &contentMockResult{}, nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"test","type":"video","metadata":{"codec":"h264"}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandleCreateContent_NilMetadataDefaultsToEmpty(t *testing.T) {
	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &contentMockResult{}, nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"test","type":"video"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	contentMap, ok := resp["content"].(map[string]interface{})
	require.True(t, ok)
	metadata, ok := contentMap["metadata"].(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, metadata)
}

func TestHandleUpdateContent_NilService(t *testing.T) {
	r := setupContentRouter(nil, "0xOwner")
	body := `{"title":"updated"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleUpdateContent_NotFound(t *testing.T) {
	db := &contentMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"updated"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/missing", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleUpdateContent_NotOwner(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "0xDifferentOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"updated"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleUpdateContent_InvalidBody(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "0xOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateContent_MetadataTooLarge(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "0xOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	largeMeta := fmt.Sprintf(`{"metadata":{"key":"%s"}}`, strings.Repeat("a", 65537))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", strings.NewReader(largeMeta))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateContent_Success(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "0xOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &contentMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"updated"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleUpdateContent_PartialUpdate(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:           "c1",
		Title:        "Test",
		Description:  "desc",
		Type:         "video",
		URL:          "http://example.com/video.mp4",
		ThumbnailURL: "http://example.com/thumb.jpg",
		Duration:     120,
		Size:         1024,
		OwnerID:      "0xOwner",
		Metadata:     map[string]interface{}{"key": "value"},
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &contentMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"new title","description":"new desc"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	contentMap, ok := resp["content"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "new title", contentMap["title"])
	assert.Equal(t, "new desc", contentMap["description"])
}

func TestHandleUpdateContent_DBError(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "0xOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"updated"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleDeleteContent_NilService(t *testing.T) {
	r := setupContentRouter(nil, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/content/c1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleDeleteContent_NotFound(t *testing.T) {
	db := &contentMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/content/missing", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleDeleteContent_NotOwner(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "0xDifferentOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/content/c1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleDeleteContent_Success(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		URL:     "/content/c1",
		OwnerID: "0xOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			return nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/content/c1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestRequireContentOwner_CopyMetadata(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "c1"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set("wallet_address", "0xOwner")

	cache := newContentMockCache()
	originalMeta := map[string]interface{}{"key": "value"}
	ownedContent := &service.Content{
		ID:       "c1",
		Title:    "Test",
		Type:     "video",
		OwnerID:  "0xOwner",
		Metadata: originalMeta,
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)

	content, ok := requireContentOwner(c, svc)
	require.True(t, ok)
	assert.Equal(t, "c1", content.ID)

	content.Metadata["newkey"] = "newvalue"
	cached, err := cache.Get("content:c1")
	require.NoError(t, err)
	cachedContent := cached.(*service.Content)
	_, hasNewKey := cachedContent.Metadata["newkey"]
	assert.False(t, hasNewKey)
}

func TestHandleCreateContent_NoWallet(t *testing.T) {
	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &contentMockResult{}, nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "")
	body := `{"title":"test","type":"video"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandleCreateContent_WithAllFields(t *testing.T) {
	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &contentMockResult{}, nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), newContentMockCache())
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"Full Content","description":"desc","type":"video","url":"http://example.com/v.mp4","thumbnail_url":"http://example.com/thumb.jpg","duration":120,"size":1024,"metadata":{"codec":"h264"}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp["id"])
	contentMap, ok := resp["content"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Full Content", contentMap["title"])
	assert.Equal(t, "video", contentMap["type"])
}

func TestHandleUpdateContent_UpdateAllFields(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:           "c1",
		Title:        "Old",
		Description:  "old desc",
		Type:         "video",
		URL:          "http://old.com",
		ThumbnailURL: "http://old.com/thumb",
		Duration:     60,
		Size:         512,
		OwnerID:      "0xOwner",
		Metadata:     map[string]interface{}{"old": true},
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &contentMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	body := `{"title":"New","description":"new desc","type":"audio","url":"http://new.com","thumbnail_url":"http://new.com/thumb","duration":120,"size":2048,"metadata":{"new":true}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	contentMap, ok := resp["content"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "New", contentMap["title"])
	assert.Equal(t, "audio", contentMap["type"])
}

func TestHandleUpdateContent_EmptyMetadata(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:       "c1",
		Title:    "Test",
		Type:     "video",
		OwnerID:  "0xOwner",
		Metadata: map[string]interface{}{"key": "value"},
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &contentMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleDeleteContent_NilDBDelete(t *testing.T) {
	cache := newContentMockCache()
	ownedContent := &service.Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "0xOwner",
	}
	_ = cache.Set("content:c1", ownedContent)

	db := &contentMockDB{
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			return nil
		},
	}
	svc := service.NewContentService(db, newContentMockObjStore(), cache)
	r := setupContentRouter(svc, "0xOwner")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/content/c1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandlePutDelete_NilService(t *testing.T) {
	r := setupContentRouter(nil, "0xOwner")

	t.Run("PUT nil service", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPut, "/api/v1/content/c1", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("DELETE nil service", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodDelete, "/api/v1/content/c1", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

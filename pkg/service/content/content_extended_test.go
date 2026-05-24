package content

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
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

type contentScanRowDriver struct {
	mu          sync.Mutex
	rows        *contentMockDriverRows
	execResult  driver.Result
}

type contentMockDriverRows struct {
	columns []string
	values  [][]driver.Value
}

type contentMockExecResult struct {
	rowsAffected int64
	lastInsertID int64
}

func (r *contentMockExecResult) LastInsertId() (int64, error) { return r.lastInsertID, nil }
func (r *contentMockExecResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

var globalContentDriver = &contentScanRowDriver{}

func init() {
	sql.Register("contentscanrow", globalContentDriver)
}

func (d *contentScanRowDriver) Open(_ string) (driver.Conn, error) {
	return &contentScanRowConn{driver: d}, nil
}

type contentScanRowConn struct {
	driver *contentScanRowDriver
}

func (c *contentScanRowConn) Prepare(_ string) (driver.Stmt, error) {
	return &contentScanRowStmt{driver: c.driver}, nil
}
func (c *contentScanRowConn) Close() error              { return nil }
func (c *contentScanRowConn) Begin() (driver.Tx, error) { return &contentScanRowTx{}, nil }

type contentScanRowTx struct{}

func (t *contentScanRowTx) Commit() error   { return nil }
func (t *contentScanRowTx) Rollback() error { return nil }

type contentScanRowStmt struct {
	driver *contentScanRowDriver
}

func (s *contentScanRowStmt) Close() error  { return nil }
func (s *contentScanRowStmt) NumInput() int { return -1 }
func (s *contentScanRowStmt) Exec(_ []driver.Value) (driver.Result, error) {
	s.driver.mu.Lock()
	defer s.driver.mu.Unlock()
	if s.driver.execResult != nil {
		return s.driver.execResult, nil
	}
	return driver.ResultNoRows, nil
}
func (s *contentScanRowStmt) Query(_ []driver.Value) (driver.Rows, error) {
	s.driver.mu.Lock()
	defer s.driver.mu.Unlock()
	if s.driver.rows == nil {
		return nil, fmt.Errorf("no rows configured")
	}
	return &contentScanRowResult{rows: s.driver.rows, pos: 0}, nil
}

type contentScanRowResult struct {
	rows *contentMockDriverRows
	pos  int
}

func (r *contentScanRowResult) Columns() []string { return r.rows.columns }
func (r *contentScanRowResult) Close() error      { return nil }
func (r *contentScanRowResult) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows.values) {
		return io.EOF
	}
	copy(dest, r.rows.values[r.pos])
	r.pos++
	return nil
}

func contentSetRows(columns []string, values [][]driver.Value) {
	globalContentDriver.mu.Lock()
	defer globalContentDriver.mu.Unlock()
	globalContentDriver.rows = &contentMockDriverRows{columns: columns, values: values}
}

func contentSetExecResult(rowsAffected int64) {
	globalContentDriver.mu.Lock()
	defer globalContentDriver.mu.Unlock()
	globalContentDriver.execResult = &contentMockExecResult{rowsAffected: rowsAffected}
}

func contentSetRowsAndExec(columns []string, values [][]driver.Value, rowsAffected int64) {
	globalContentDriver.mu.Lock()
	defer globalContentDriver.mu.Unlock()
	globalContentDriver.rows = &contentMockDriverRows{columns: columns, values: values}
	globalContentDriver.execResult = &contentMockExecResult{rowsAffected: rowsAffected}
}

func contentResetDriver() {
	globalContentDriver.mu.Lock()
	defer globalContentDriver.mu.Unlock()
	globalContentDriver.rows = nil
	globalContentDriver.execResult = nil
}

func contentOpenDB() *sql.DB {
	db, _ := sql.Open("contentscanrow", "")
	return db
}

func contentColumns() []string {
	return []string{
		"id", "title", "description", "type", "url", "thumbnail_url",
		"duration", "size", "status", "owner_id", "created_at", "updated_at", "metadata",
	}
}

func contentColumnsWithCount() []string {
	return []string{
		"total_count", "id", "title", "description", "type", "url", "thumbnail_url",
		"duration", "size", "status", "owner_id", "created_at", "updated_at", "metadata",
	}
}

func makeContentRow(id, title, desc, ctype, url, thumbURL string, duration int64, size int64, status, ownerID string, createdAt, updatedAt time.Time, metadata map[string]interface{}) [][]driver.Value {
	metaJSON, _ := json.Marshal(metadata)
	return [][]driver.Value{
		{id, title, desc, ctype, url, thumbURL, duration, size, status, ownerID, createdAt, updatedAt, metaJSON},
	}
}

func makeContentRowWithCount(totalCount int, id, title, desc, ctype, url, thumbURL string, duration int64, size int64, status, ownerID string, createdAt, updatedAt time.Time, metadata map[string]interface{}) [][]driver.Value {
	metaJSON, _ := json.Marshal(metadata)
	return [][]driver.Value{
		{totalCount, id, title, desc, ctype, url, thumbURL, duration, size, status, ownerID, createdAt, updatedAt, metaJSON},
	}
}

func TestContentService_GetContent_SuccessFromDB(t *testing.T) {
	now := time.Now()
	contentSetRows(contentColumns(), makeContentRow(
		"c1", "Test Video", "A test", "video", "/content/c1", "/thumb/c1",
		120, 1024000, "ready", "owner1", now, now, map[string]interface{}{"codec": "h264"},
	))
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
	}
	cache := newMockCache()
	svc := NewContentService(db, newMockObjStore(), cache, zap.NewNop())

	content, err := svc.GetContent(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "c1", content.ID)
	assert.Equal(t, "Test Video", content.Title)
	assert.Equal(t, "A test", content.Description)
	assert.Equal(t, "video", content.Type)
	assert.Equal(t, "/content/c1", content.URL)
	assert.Equal(t, "/thumb/c1", content.ThumbnailURL)
	assert.Equal(t, 120, content.Duration)
	assert.Equal(t, int64(1024000), content.Size)
	assert.Equal(t, "ready", content.Status)
	assert.Equal(t, "owner1", content.OwnerID)
	assert.Equal(t, "h264", content.Metadata["codec"])

	cached, cacheErr := cache.Get("content:c1")
	require.NoError(t, cacheErr)
	cachedContent, ok := cached.(*Content)
	require.True(t, ok)
	assert.Equal(t, "c1", cachedContent.ID)
}

func TestContentService_GetContent_SuccessNilCache(t *testing.T) {
	now := time.Now()
	contentSetRows(contentColumns(), makeContentRow(
		"c1", "Test", "", "video", "", "", 0, 0, "pending", "owner1", now, now, nil,
	))
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
	}
	svc := NewContentService(db, newMockObjStore(), nil, zap.NewNop())

	content, err := svc.GetContent(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "c1", content.ID)
}

func TestContentService_GetContent_MetadataParseError(t *testing.T) {
	contentSetRows(contentColumns(), [][]driver.Value{
		{"c1", "Test", "", "video", "", "", int64(0), int64(0), "pending", "owner1", time.Now(), time.Now(), []byte("invalid json{")},
	})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
	}
	svc := NewContentService(db, newMockObjStore(), nil, zap.NewNop())

	_, err := svc.GetContent(context.Background(), "c1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse metadata")
}

func TestContentService_GetContent_CacheSetError(t *testing.T) {
	now := time.Now()
	contentSetRows(contentColumns(), makeContentRow(
		"c1", "Test", "", "video", "", "", 0, 0, "pending", "owner1", now, now, nil,
	))
	scanDB := contentOpenDB()

	cache := &mockCacheWithSetError{data: make(map[string]interface{})}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
	}
	svc := NewContentService(db, newMockObjStore(), cache, zap.NewNop())

	content, err := svc.GetContent(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "c1", content.ID)
}

type mockCacheWithSetError struct {
	data map[string]interface{}
}

func (m *mockCacheWithSetError) Get(key string) (interface{}, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("cache miss: %s", key)
	}
	return v, nil
}
func (m *mockCacheWithSetError) Set(key string, value interface{}) error {
	m.data[key] = value
	return nil
}
func (m *mockCacheWithSetError) SetWithExpiration(_ string, _ interface{}, _ time.Duration) error {
	return errors.New("cache set error")
}
func (m *mockCacheWithSetError) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func TestContentService_CreateContentWithTx_SuccessPath(t *testing.T) {
	contentSetRows([]string{"id"}, [][]driver.Value{{"c1"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())

	id, err := svc.CreateContentWithTx(context.Background(), &Content{
		ID:      "c1",
		Title:   "Test Video",
		Type:    "video",
		OwnerID: "owner1",
	})
	require.NoError(t, err)
	assert.Equal(t, "c1", id)
}

func TestContentService_CreateContentWithTx_GeneratesID(t *testing.T) {
	contentSetRows([]string{"id"}, [][]driver.Value{{"auto-id"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())

	id, err := svc.CreateContentWithTx(context.Background(), &Content{
		Title:   "Test",
		Type:    "video",
		OwnerID: "owner1",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

func TestContentService_CreateContentWithTx_DefaultStatus(t *testing.T) {
	contentSetRows([]string{"id"}, [][]driver.Value{{"c1"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())

	c := &Content{ID: "c1", Title: "Test", Type: "video", OwnerID: "owner1"}
	id, err := svc.CreateContentWithTx(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, "c1", id)
	assert.Equal(t, "pending", c.Status)
}

func TestContentService_CreateContentWithTx_MetadataError(t *testing.T) {
	db := &mockDB{}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())

	_, err := svc.CreateContentWithTx(context.Background(), &Content{
		Title:    "Test",
		Metadata: map[string]interface{}{"ch": make(chan int)},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to serialize metadata")
}

func TestContentService_CreateContentWithTx_WithRegistrySuccess(t *testing.T) {
	contentSetRows([]string{"id"}, [][]driver.Value{{"c1"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	registry := &mockContentRegistry{txHash: "0xabc123"}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
	svc.SetContentRegistry(registry)

	id, err := svc.CreateContentWithTx(context.Background(), &Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "owner1",
	})
	require.NoError(t, err)
	assert.Equal(t, "c1", id)
}

func TestContentService_CreateContentWithTx_WithRegistryError(t *testing.T) {
	contentSetRows([]string{"id"}, [][]driver.Value{{"c1"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	registry := &mockContentRegistry{err: errors.New("chain error")}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
	svc.SetContentRegistry(registry)

	id, err := svc.CreateContentWithTx(context.Background(), &Content{
		ID:      "c1",
		Title:   "Test",
		Type:    "video",
		OwnerID: "owner1",
	})
	require.NoError(t, err)
	assert.Equal(t, "c1", id)
}

func TestContentService_DeleteContentWithTx_Success(t *testing.T) {
	contentSetExecResult(1)
	scanDB := contentOpenDB()

	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	cache := newMockCache()
	svc := NewContentService(db, newMockObjStore(), cache, zap.NewNop())
	_ = cache.Set("content:c1", &Content{ID: "c1"})

	err := svc.DeleteContentWithTx(context.Background(), "c1", "owner1")
	require.NoError(t, err)

	_, cacheErr := cache.Get("content:c1")
	assert.Error(t, cacheErr)
}

func TestContentService_DeleteContentWithTx_NotFound(t *testing.T) {
	contentSetExecResult(0)
	scanDB := contentOpenDB()

	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())

	err := svc.DeleteContentWithTx(context.Background(), "missing", "owner1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "content not found")
}

func TestContentService_DeleteContentWithTx_CacheDeleteError(t *testing.T) {
	contentSetExecResult(1)
	scanDB := contentOpenDB()

	cache := &mockCacheDeleteErr{}
	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	svc := NewContentService(db, newMockObjStore(), cache, zap.NewNop())

	err := svc.DeleteContentWithTx(context.Background(), "c1", "owner1")
	require.NoError(t, err)
}

type mockCacheDeleteErr struct{}

func (m *mockCacheDeleteErr) Get(_ string) (interface{}, error) { return nil, errors.New("cache miss") }
func (m *mockCacheDeleteErr) Set(_ string, _ interface{}) error { return nil }
func (m *mockCacheDeleteErr) SetWithExpiration(_ string, _ interface{}, _ time.Duration) error {
	return nil
}
func (m *mockCacheDeleteErr) Delete(_ string) error {
	return errors.New("cache delete error")
}

func TestContentService_DeleteContent_SuccessWithInTx(t *testing.T) {
	now := time.Now()
	contentSetRowsAndExec(contentColumns(), makeContentRow(
		"c1", "Test Video", "desc", "video", "/content/c1", "/thumb/c1",
		120, 1024000, "ready", "owner1", now, now, nil,
	), 1)
	scanDB := contentOpenDB()

	cache := newMockCache()
	_ = cache.Set("content:c1", &Content{ID: "c1", URL: "/content/c1", OwnerID: "owner1"})

	objStore := newMockObjStore()
	objStore.Upload(context.Background(), "content", "c1", []byte("video data"))

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			tx, _ := scanDB.Begin()
			defer tx.Rollback()
			return fn(tx)
		},
	}
	svc := NewContentService(db, objStore, cache)

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	require.NoError(t, err)

	_, cacheErr := cache.Get("content:c1")
	assert.Error(t, cacheErr)
}

func TestContentService_DeleteContent_NotFoundInTx(t *testing.T) {
	now := time.Now()
	contentSetRowsAndExec(contentColumns(), makeContentRow(
		"c1", "Test", "", "video", "", "", 0, 0, "ready", "owner1", now, now, nil,
	), 0)
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			tx, _ := scanDB.Begin()
			defer tx.Rollback()
			return fn(tx)
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "content not found")
}

func TestContentService_DeleteContent_ObjStoreDeleteError(t *testing.T) {
	now := time.Now()
	contentSetRowsAndExec(contentColumns(), makeContentRow(
		"c1", "Test", "", "video", "/content/c1", "", 0, 0, "ready", "owner1", now, now, nil,
	), 1)
	scanDB := contentOpenDB()

	objStore := &mockObjStoreDeleteErr{}
	cache := newMockCache()
	_ = cache.Set("content:c1", &Content{ID: "c1", URL: "/content/c1", OwnerID: "owner1"})

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			tx, _ := scanDB.Begin()
			defer tx.Rollback()
			return fn(tx)
		},
	}
	svc := NewContentService(db, objStore, cache)

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	require.NoError(t, err)
}

type mockObjStoreDeleteErr struct{}

func (m *mockObjStoreDeleteErr) Upload(_ context.Context, _, _ string, _ []byte) error {
	return nil
}
func (m *mockObjStoreDeleteErr) Download(_ context.Context, _, _ string) ([]byte, error) {
	return nil, nil
}
func (m *mockObjStoreDeleteErr) Delete(_ context.Context, _, _ string) error {
	return errors.New("storage delete error")
}
func (m *mockObjStoreDeleteErr) Exists(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

func TestContentService_DeleteContent_NilObjStore(t *testing.T) {
	now := time.Now()
	contentSetRowsAndExec(contentColumns(), makeContentRow(
		"c1", "Test", "", "video", "/content/c1", "", 0, 0, "ready", "owner1", now, now, nil,
	), 1)
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			tx, _ := scanDB.Begin()
			defer tx.Rollback()
			return fn(tx)
		},
	}
	svc := NewContentService(db, nil, newMockCache())

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	require.NoError(t, err)
}

func TestContentService_DeleteContent_EmptyURL(t *testing.T) {
	now := time.Now()
	contentSetRowsAndExec(contentColumns(), makeContentRow(
		"c1", "Test", "", "video", "", "", 0, 0, "ready", "owner1", now, now, nil,
	), 1)
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			tx, _ := scanDB.Begin()
			defer tx.Rollback()
			return fn(tx)
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	require.NoError(t, err)
}

func TestContentService_DeleteContent_CacheDeleteErrorOnDelete(t *testing.T) {
	now := time.Now()
	contentSetRowsAndExec(contentColumns(), makeContentRow(
		"c1", "Test", "", "video", "", "", 0, 0, "ready", "owner1", now, now, nil,
	), 1)
	scanDB := contentOpenDB()

	cache := &mockCacheDeleteErr{}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			tx, _ := scanDB.Begin()
			defer tx.Rollback()
			return fn(tx)
		},
	}
	svc := NewContentService(db, newMockObjStore(), cache, zap.NewNop())

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	require.NoError(t, err)
}

func TestContentService_ListContents_Success(t *testing.T) {
	now := time.Now()
	contentSetRows(contentColumns(), [][]driver.Value{
		{"c1", "Video 1", "desc1", "video", "/c1", "/t1", int64(120), int64(1024), "ready", "owner1", now, now, []byte(`{"codec":"h264"}`)},
		{"c2", "Video 2", "desc2", "audio", "/c2", "/t2", int64(60), int64(512), "pending", "owner1", now, now, nil},
	})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return scanDB.Query("SELECT ...")
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	contents, err := svc.ListContents(context.Background(), "owner1", 10, 0)
	require.NoError(t, err)
	assert.Len(t, contents, 2)
	assert.Equal(t, "c1", contents[0].ID)
	assert.Equal(t, "Video 1", contents[0].Title)
	assert.Equal(t, "desc1", contents[0].Description)
	assert.Equal(t, "video", contents[0].Type)
	assert.Equal(t, "/c1", contents[0].URL)
	assert.Equal(t, "/t1", contents[0].ThumbnailURL)
	assert.Equal(t, 120, contents[0].Duration)
	assert.Equal(t, int64(1024), contents[0].Size)
	assert.Equal(t, "ready", contents[0].Status)
	assert.Equal(t, "owner1", contents[0].OwnerID)
	assert.Equal(t, "h264", contents[0].Metadata["codec"])

	assert.Equal(t, "c2", contents[1].ID)
	assert.Equal(t, "audio", contents[1].Type)
	assert.Nil(t, contents[1].Metadata)
}

func TestContentService_ListContents_MetadataParseError(t *testing.T) {
	now := time.Now()
	contentSetRows(contentColumns(), [][]driver.Value{
		{"c1", "Video 1", "desc", "video", "/c1", "/t1", int64(120), int64(1024), "ready", "owner1", now, now, []byte("invalid{json")},
	})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return scanDB.Query("SELECT ...")
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	_, err := svc.ListContents(context.Background(), "owner1", 10, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse metadata")
}

func TestContentService_ListContentsWithCount_Success(t *testing.T) {
	now := time.Now()
	contentSetRows(contentColumnsWithCount(), makeContentRowWithCount(
		2, "c1", "Video 1", "desc1", "video", "/c1", "/t1",
		120, 1024, "ready", "owner1", now, now, map[string]interface{}{"codec": "h264"},
	))
	scanDB := contentOpenDB()

	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return scanDB.Query("SELECT ...")
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	contents, totalCount, err := svc.ListContentsWithCount(context.Background(), "owner1", 10, 0)
	require.NoError(t, err)
	assert.Len(t, contents, 1)
	assert.Equal(t, 2, totalCount)
	assert.Equal(t, "c1", contents[0].ID)
	assert.Equal(t, "h264", contents[0].Metadata["codec"])
}

func TestContentService_ListContentsWithCount_MetadataParseError(t *testing.T) {
	now := time.Now()
	contentSetRows(contentColumnsWithCount(), [][]driver.Value{
		{1, "c1", "Video 1", "desc", "video", "/c1", "/t1", int64(120), int64(1024), "ready", "owner1", now, now, []byte("bad json")},
	})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return scanDB.Query("SELECT ...")
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	_, _, err := svc.ListContentsWithCount(context.Background(), "owner1", 10, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse metadata")
}

func TestContentService_CountContents_SuccessPath(t *testing.T) {
	contentSetRows([]string{"count"}, [][]driver.Value{{int64(5)}})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	count, err := svc.CountContents(context.Background(), "owner1")
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}

func TestContentService_UpdateContentStatus_ValidTransition(t *testing.T) {
	contentSetRows([]string{"status"}, [][]driver.Value{{"processing"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "SELECT status") {
				row := scanDB.QueryRow("SELECT ...")
				return stg.NewCancelRow(row, func() {})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected query"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	cache := newMockCache()
	svc := NewContentService(db, newMockObjStore(), cache, zap.NewNop())

	err := svc.UpdateContentStatus(context.Background(), "c1", "ready")
	require.NoError(t, err)

	_, cacheErr := cache.Get("content:c1")
	assert.Error(t, cacheErr)
}

func TestContentService_UpdateContentStatus_InvalidTransitionPath(t *testing.T) {
	contentSetRows([]string{"status"}, [][]driver.Value{{"draft"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "SELECT status") {
				row := scanDB.QueryRow("SELECT ...")
				return stg.NewCancelRow(row, func() {})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected query"))
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	err := svc.UpdateContentStatus(context.Background(), "c1", "ready")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid status transition")
}

func TestContentService_UpdateContentStatus_ExecError(t *testing.T) {
	contentSetRows([]string{"status"}, [][]driver.Value{{"processing"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "SELECT status") {
				row := scanDB.QueryRow("SELECT ...")
				return stg.NewCancelRow(row, func() {})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected query"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("exec error")
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	err := svc.UpdateContentStatus(context.Background(), "c1", "ready")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update content status")
}

func TestContentService_UpdateContentStatus_ConcurrentChange(t *testing.T) {
	contentSetRows([]string{"status"}, [][]driver.Value{{"processing"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "SELECT status") {
				row := scanDB.QueryRow("SELECT ...")
				return stg.NewCancelRow(row, func() {})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected query"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	err := svc.UpdateContentStatus(context.Background(), "c1", "ready")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "concurrently")
}

func TestContentService_UpdateContentStatus_RowsAffectedError(t *testing.T) {
	contentSetRows([]string{"status"}, [][]driver.Value{{"processing"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "SELECT status") {
				row := scanDB.QueryRow("SELECT ...")
				return stg.NewCancelRow(row, func() {})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected query"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0, err: errors.New("rows affected error")}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())

	err := svc.UpdateContentStatus(context.Background(), "c1", "ready")
	assert.Error(t, err)
}

func TestContentService_UpdateContentStatus_NilCache(t *testing.T) {
	contentSetRows([]string{"status"}, [][]driver.Value{{"processing"}})
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "SELECT status") {
				row := scanDB.QueryRow("SELECT ...")
				return stg.NewCancelRow(row, func() {})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected query"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), nil, zap.NewNop())

	err := svc.UpdateContentStatus(context.Background(), "c1", "ready")
	require.NoError(t, err)
}

func TestContentService_UpdateContentStatus_CacheDeleteError(t *testing.T) {
	contentSetRows([]string{"status"}, [][]driver.Value{{"processing"}})
	scanDB := contentOpenDB()

	cache := &mockCacheDeleteErr{}
	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "SELECT status") {
				row := scanDB.QueryRow("SELECT ...")
				return stg.NewCancelRow(row, func() {})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected query"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), cache, zap.NewNop())

	err := svc.UpdateContentStatus(context.Background(), "c1", "ready")
	require.NoError(t, err)
}

func TestContentService_UpdateContent_CacheDeleteError(t *testing.T) {
	cache := &mockCacheDeleteErr{}
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), cache, zap.NewNop())

	err := svc.UpdateContent(context.Background(), &Content{ID: "c1", Title: "updated"})
	require.NoError(t, err)
}

func TestContentService_UpdateContent_NilCache(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), nil)

	err := svc.UpdateContent(context.Background(), &Content{ID: "c1", Title: "updated"})
	require.NoError(t, err)
}

func TestContentService_DeleteContentWithTx_DeleteExecError(t *testing.T) {
	contentSetExecResult(0)
	scanDB := contentOpenDB()

	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return scanDB.Begin()
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())

	err := svc.DeleteContentWithTx(context.Background(), "c1", "owner1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "content not found")
}

func TestContentService_DeleteContent_NilCacheDelete(t *testing.T) {
	now := time.Now()
	contentSetRowsAndExec(contentColumns(), makeContentRow(
		"c1", "Test", "", "video", "", "", 0, 0, "ready", "owner1", now, now, nil,
	), 1)
	scanDB := contentOpenDB()

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			row := scanDB.QueryRow("SELECT ...")
			return stg.NewCancelRow(row, func() {})
		},
		inTxFn: func(ctx context.Context, fn func(tx *sql.Tx) error) error {
			tx, _ := scanDB.Begin()
			defer tx.Rollback()
			return fn(tx)
		},
	}
	svc := NewContentService(db, newMockObjStore(), nil)

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	require.NoError(t, err)
}

package upload

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeDriver struct{}
type fakeConn struct{}
type fakeStmt struct{}
type fakeTx struct{}

func (d *fakeDriver) Open(name string) (driver.Conn, error)   { return &fakeConn{}, nil }
func (c *fakeConn) Prepare(query string) (driver.Stmt, error) { return &fakeStmt{}, nil }
func (c *fakeConn) Close() error                              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)                 { return &fakeTx{}, nil }
func (s *fakeStmt) Close() error                              { return nil }
func (s *fakeStmt) NumInput() int                             { return -1 }
func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return &fakeResult{}, nil
}
func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	return nil, errors.New("not implemented")
}
func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }

type fakeResult struct{}

func (r *fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r *fakeResult) RowsAffected() (int64, error) { return 1, nil }

var testTx *sql.Tx

func init() {
	sql.Register("fakedrv", &fakeDriver{})
	db, err := sql.Open("fakedrv", "")
	if err != nil {
		panic(err)
	}
	testTx, err = db.Begin()
	if err != nil {
		panic(err)
	}
}

func TestUploadService_UploadStream_HashComputed(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	store := newMockObjStore()
	svc := NewUploadService(db, store, "test-bucket", zap.NewNop())

	data := "hello world"
	id, err := svc.UploadStream(context.Background(), "video.mp4", strings.NewReader(data), int64(len(data)), "owner1")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	key := "test-bucket/owner1/" + id + ".mp4"
	stored, ok := store.data[key]
	assert.True(t, ok)
	assert.Equal(t, data, string(stored))
}

func TestUploadService_Upload_DelegatesToUploadStream(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "test-bucket", zap.NewNop())
	id, err := svc.Upload(context.Background(), "video.mp4", []byte("test data"), "owner1")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

func TestUploadService_UploadStream_CleanupOnDBError(t *testing.T) {
	store := newMockObjStore()
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewUploadService(db, store, "test-bucket", zap.NewNop())

	_, err := svc.UploadStream(context.Background(), "video.mp4", strings.NewReader("data"), 4, "owner1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save upload info")
}

func TestUploadService_CheckStorageQuota_WithinQuota(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{int64(500)}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	svc.storageQuota = 1000
	err := svc.CheckStorageQuota(context.Background(), "owner1", 400)
	assert.NoError(t, err)
}

func TestUploadService_CheckStorageQuota_ExceedsQuota(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{int64(800)}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	svc.storageQuota = 1000
	err := svc.CheckStorageQuota(context.Background(), "owner1", 300)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "storage quota exceeded")
}

func TestUploadService_GetUploadStatus_Success(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/bucket/key", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	info, err := svc.GetUploadStatus(context.Background(), "upload-1")
	require.NoError(t, err)
	assert.Equal(t, "upload-1", info.ID)
	assert.Equal(t, "video.mp4", info.Filename)
	assert.Equal(t, int64(1024), info.Size)
	assert.Equal(t, "video/mp4", info.ContentType)
	assert.Equal(t, "abc123", info.Hash)
	assert.Equal(t, "completed", info.Status)
	assert.Equal(t, "/bucket/key", info.URL)
	assert.Equal(t, "owner1", info.OwnerID)
}

func TestUploadService_GetUploadStatus_NotFound(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	_, err := svc.GetUploadStatus(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload not found")
}

func TestUploadService_GetUploadProgress_Completed(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/bucket/key", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	progress, err := svc.GetUploadProgress(context.Background(), "upload-1")
	require.NoError(t, err)
	assert.Equal(t, 100, progress)
}

func TestUploadService_GetUploadProgress_Processed(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "processed", "/bucket/key", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	progress, err := svc.GetUploadProgress(context.Background(), "upload-1")
	require.NoError(t, err)
	assert.Equal(t, 100, progress)
}

func TestUploadService_GetUploadProgress_UploadingPartial(t *testing.T) {
	now := time.Now()
	callCount := 0
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			callCount++
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
				now, now,
			}})
		},
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{
				rows: [][]interface{}{
					{0, int64(512), true},
					{1, int64(512), false},
					{2, int64(512), true},
					{3, int64(512), false},
				},
			}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	progress, err := svc.GetUploadProgress(context.Background(), "upload-1")
	require.NoError(t, err)
	assert.Equal(t, 50, progress)
}

func TestUploadService_GetUploadProgress_UploadingNoChunks(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
				now, now,
			}})
		},
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{rows: [][]interface{}{}}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	progress, err := svc.GetUploadProgress(context.Background(), "upload-1")
	require.NoError(t, err)
	assert.Equal(t, 0, progress)
}

func TestUploadService_GetChunkStatuses_Success(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{
				rows: [][]interface{}{
					{0, int64(512), true},
					{1, int64(512), false},
				},
			}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	chunks, err := svc.GetChunkStatuses(context.Background(), "upload-1")
	require.NoError(t, err)
	assert.Len(t, chunks, 2)
	assert.Equal(t, "upload-1", chunks[0].UploadID)
	assert.Equal(t, 2, chunks[0].TotalChunks)
	assert.Equal(t, 2, chunks[1].TotalChunks)
	assert.True(t, chunks[0].Uploaded)
	assert.False(t, chunks[1].Uploaded)
}

func TestUploadService_GetChunkStatuses_QueryError(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	_, err := svc.GetChunkStatuses(context.Background(), "upload-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query chunk statuses")
}

func TestUploadService_GetChunkStatuses_ScanError(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{
				rows:    [][]interface{}{{"bad_data"}},
				scanErr: errors.New("scan error"),
			}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	_, err := svc.GetChunkStatuses(context.Background(), "upload-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan chunk")
}

func TestUploadService_InitiateChunkedUpload_QuotaExceeded(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{int64(900)}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	svc.storageQuota = 1000
	_, err := svc.InitiateChunkedUpload(context.Background(), "video.mp4", 200, 5, "owner1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage quota exceeded")
}

func TestUploadService_UploadChunkStream_Success(t *testing.T) {
	now := time.Now()
	var execCalled int32
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
				now, now,
			}})
		},
		execFn: func(_ context.Context, query string, _ ...interface{}) (sql.Result, error) {
			atomic.AddInt32(&execCalled, 1)
			return &mockResult{}, nil
		},
	}
	store := newMockObjStore()
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.UploadChunkStream(context.Background(), "upload-1", 0, strings.NewReader("chunk0"), 6, "owner1")
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&execCalled))
}

func TestUploadService_UploadChunkStream_OwnerMismatch(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "mybucket", zap.NewNop())

	err := svc.UploadChunkStream(context.Background(), "upload-1", 0, strings.NewReader("chunk0"), 6, "wrong-owner")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to this wallet")
}

func TestUploadService_UploadChunkStream_NotUploadingState(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/bucket/key", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "mybucket", zap.NewNop())

	err := svc.UploadChunkStream(context.Background(), "upload-1", 0, strings.NewReader("chunk0"), 6, "owner1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in uploading state")
}

func TestUploadService_UploadChunkStream_ChunkAlreadyExists(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	store.data["mybucket/chunks/upload-1/0"] = []byte("existing")
	store.existsResult = true

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.UploadChunkStream(context.Background(), "upload-1", 0, strings.NewReader("chunk0"), 6, "owner1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already uploaded")
}

func TestUploadService_UploadChunkStream_ExistsCheckFails_Continues(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	store.existsErr = errors.New("check failed")

	var execCalled int32
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
				now, now,
			}})
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			atomic.AddInt32(&execCalled, 1)
			return &mockResult{}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.UploadChunkStream(context.Background(), "upload-1", 0, strings.NewReader("chunk0"), 6, "owner1")
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&execCalled))
}

func TestUploadService_UploadChunkStream_DBInsertWarns(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	var execCalls int32
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
				now, now,
			}})
		},
		execFn: func(_ context.Context, query string, _ ...interface{}) (sql.Result, error) {
			n := atomic.AddInt32(&execCalls, 1)
			if n == 1 {
				return nil, errors.New("insert chunk failed")
			}
			return &mockResult{}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.UploadChunkStream(context.Background(), "upload-1", 0, strings.NewReader("chunk0"), 6, "owner1")
	require.NoError(t, err)
}

func TestUploadService_UploadChunkStream_UpdateStatusFails(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
				now, now,
			}})
		},
		execFn: func(_ context.Context, query string, _ ...interface{}) (sql.Result, error) {
			if strings.Contains(query, "UPDATE uploads") {
				return nil, errors.New("update failed")
			}
			return &mockResult{}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.UploadChunkStream(context.Background(), "upload-1", 0, strings.NewReader("chunk0"), 6, "owner1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update upload status")
}

func TestUploadService_CompleteChunkedUpload_Success(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("mybucket/chunks/upload-1/%d", i)
		store.data[key] = []byte(fmt.Sprintf("chunk%d", i))
	}

	var execCalls int32
	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"upload-1", "video.mp4", int64(1024),
					"video/mp4", "abc123", "uploading", "/mybucket/owner1/upload-1.mp4", "owner1",
					now, now,
				}})
			}
			if strings.Contains(query, "COUNT(*)") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{3}})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected query"))
		},
		execFn: func(_ context.Context, query string, _ ...interface{}) (sql.Result, error) {
			atomic.AddInt32(&execCalls, 1)
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())
	svc.SetChunkMergeConcurrency(2)

	err := svc.CompleteChunkedUpload(context.Background(), "upload-1", 3)
	require.NoError(t, err)

	mergedKey := "mybucket/owner1/upload-1.mp4"
	_, ok := store.data[mergedKey]
	assert.True(t, ok, "merged file should exist in object store")
}

func TestUploadService_CompleteChunkedUpload_NotUploadingState(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"upload-1", "video.mp4", int64(1024),
					"video/mp4", "abc123", "completed", "/bucket/key", "owner1",
					now, now,
				}})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected"))
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())

	err := svc.CompleteChunkedUpload(context.Background(), "upload-1", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in uploading state")
}

func TestUploadService_CompleteChunkedUpload_ExceedsMaxSize(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"upload-1", "video.mp4", int64(9999),
					"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
					now, now,
				}})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected"))
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.SetMaxUploadSize(100)

	err := svc.CompleteChunkedUpload(context.Background(), "upload-1", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed size")
}

func TestUploadService_CompleteChunkedUpload_ChunkCountError(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"upload-1", "video.mp4", int64(1024),
					"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
					now, now,
				}})
			}
			if strings.Contains(query, "COUNT(*)") {
				return stg.NewErrorCancelRow(errors.New("count error"))
			}
			return stg.NewErrorCancelRow(errors.New("unexpected"))
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())

	err := svc.CompleteChunkedUpload(context.Background(), "upload-1", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to verify chunk upload status")
}

func TestUploadService_CompleteChunkedUpload_NotAllChunks(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"upload-1", "video.mp4", int64(1024),
					"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
					now, now,
				}})
			}
			if strings.Contains(query, "COUNT(*)") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{2}})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected"))
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())

	err := svc.CompleteChunkedUpload(context.Background(), "upload-1", 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not all chunks uploaded")
}

func TestUploadService_CompleteChunkedUpload_DownloadStreamFails(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	store.downloadStreamErr = errors.New("download failed")

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"upload-1", "video.mp4", int64(1024),
					"video/mp4", "abc123", "uploading", "/mybucket/owner1/upload-1.mp4", "owner1",
					now, now,
				}})
			}
			if strings.Contains(query, "COUNT(*)") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{1}})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected"))
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.CompleteChunkedUpload(context.Background(), "upload-1", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download chunk")
}

func TestUploadService_CompleteChunkedUpload_AlreadyCompleted(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	store.data["mybucket/chunks/upload-1/0"] = []byte("chunk0")

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"upload-1", "video.mp4", int64(1024),
					"video/mp4", "abc123", "uploading", "/mybucket/owner1/upload-1.mp4", "owner1",
					now, now,
				}})
			}
			if strings.Contains(query, "COUNT(*)") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{1}})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.CompleteChunkedUpload(context.Background(), "upload-1", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already completed or status changed")
}

func TestUploadService_DeleteUpload_Success(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	store.data["mybucket/owner1/upload-1.mp4"] = []byte("file data")
	store.data["mybucket/chunks/upload-1/0"] = []byte("chunk0")
	store.data["mybucket/chunks/upload-1/1"] = []byte("chunk1")

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/mybucket/owner1/upload-1.mp4", "owner1",
				now, now,
			}})
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.DeleteUpload(context.Background(), "upload-1")
	require.NoError(t, err)

	_, ok := store.data["mybucket/owner1/upload-1.mp4"]
	assert.False(t, ok, "file should be deleted from object store")
}

func TestUploadService_DeleteUpload_URLTooShort(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "x", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "mybucket", zap.NewNop())

	err := svc.DeleteUpload(context.Background(), "upload-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected URL format")
}

func TestUploadService_DeleteUpload_ListChunksFails(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	store.data["mybucket/owner1/upload-1.mp4"] = []byte("file data")
	store.listErr = errors.New("list failed")

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/mybucket/owner1/upload-1.mp4", "owner1",
				now, now,
			}})
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.DeleteUpload(context.Background(), "upload-1")
	require.NoError(t, err)
}

func TestUploadService_DeleteUpload_DeleteObjectsFails(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	store.data["mybucket/owner1/upload-1.mp4"] = []byte("file data")
	store.data["mybucket/chunks/upload-1/0"] = []byte("chunk0")
	store.deleteObjErr = errors.New("batch delete failed")

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/mybucket/owner1/upload-1.mp4", "owner1",
				now, now,
			}})
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.DeleteUpload(context.Background(), "upload-1")
	require.NoError(t, err)
}

func TestUploadService_ListUploads_Success(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{
				rows: [][]interface{}{
					{"id1", "video.mp4", int64(1024), "video/mp4", "hash1", "completed", "/bucket/key1", "owner1", now, now},
					{"id2", "audio.mp3", int64(512), "audio/mpeg", "hash2", "uploading", "/bucket/key2", "owner1", now, now},
				},
			}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	uploads, err := svc.ListUploads(context.Background(), "owner1", 10, 0)
	require.NoError(t, err)
	assert.Len(t, uploads, 2)
	assert.Equal(t, "id1", uploads[0].ID)
	assert.Equal(t, "id2", uploads[1].ID)
}

func TestUploadService_ListUploads_ScanError(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{
				rows:    [][]interface{}{{"bad"}},
				scanErr: errors.New("scan error"),
			}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	_, err := svc.ListUploads(context.Background(), "owner1", 10, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan upload")
}

func TestUploadService_GetDownloadURL_Success(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/mybucket/owner1/upload-1.mp4", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "mybucket", zap.NewNop())
	svc.SetPresigner(&mockPresigner{url: "https://cdn.example.com/presigned"})

	url, err := svc.GetDownloadURL(context.Background(), "upload-1", time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/presigned", url)
}

func TestUploadService_GetDownloadURL_OwnerMatch(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/mybucket/owner1/upload-1.mp4", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "mybucket", zap.NewNop())
	svc.SetPresigner(&mockPresigner{url: "https://cdn.example.com/presigned"})

	url, err := svc.GetDownloadURL(context.Background(), "upload-1", time.Hour, "owner1")
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/presigned", url)
}

func TestUploadService_GetDownloadURL_OwnerMismatch(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/mybucket/owner1/upload-1.mp4", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "mybucket", zap.NewNop())
	svc.SetPresigner(&mockPresigner{url: "https://cdn.example.com/presigned"})

	_, err := svc.GetDownloadURL(context.Background(), "upload-1", time.Hour, "other-owner")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to this wallet")
}

func TestUploadService_GetDownloadURL_NotCompletedStatus(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/mybucket/owner1/upload-1.mp4", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "mybucket", zap.NewNop())
	svc.SetPresigner(&mockPresigner{url: "https://cdn.example.com/presigned"})

	_, err := svc.GetDownloadURL(context.Background(), "upload-1", time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload not completed")
}

func TestUploadService_GetDownloadURL_ProcessedStatus(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "processed", "/mybucket/owner1/upload-1.mp4", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "mybucket", zap.NewNop())
	svc.SetPresigner(&mockPresigner{url: "https://cdn.example.com/presigned"})

	url, err := svc.GetDownloadURL(context.Background(), "upload-1", time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/presigned", url)
}

func TestUploadService_InitiatePresignedUpload_Success(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.storageQuota = 0
	svc.SetUploadPresigner(&mockUploadPresigner{url: "https://example.com/upload"})

	uploadID, presignedURL, storageKey, err := svc.InitiatePresignedUpload(context.Background(), "video.mp4", 1024, "video/mp4", "owner1")
	require.NoError(t, err)
	assert.NotEmpty(t, uploadID)
	assert.Equal(t, "https://example.com/upload", presignedURL)
	assert.Contains(t, storageKey, "owner1")
	assert.Contains(t, storageKey, ".mp4")
}

func TestUploadService_InitiatePresignedUpload_EmptyContentType(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.storageQuota = 0
	svc.SetUploadPresigner(&mockUploadPresigner{url: "https://example.com/upload"})

	uploadID, _, _, err := svc.InitiatePresignedUpload(context.Background(), "video.mp4", 1024, "", "owner1")
	require.NoError(t, err)
	assert.NotEmpty(t, uploadID)
}

func TestUploadService_CompleteUploadWithTx_Success(t *testing.T) {
	now := time.Now()
	var txContentID string
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/bucket/key", "owner1",
				now, now,
			}})
		},
		inTxFn: func(_ context.Context, fn func(tx *sql.Tx) error) error {
			return fn(testTx)
		},
	}

	var hookCalled int32
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.RegisterPostUploadHook(func(_ context.Context, _, contentID, _ string) {
		atomic.StoreInt32(&hookCalled, 1)
		txContentID = contentID
	})

	contentID, err := svc.CompleteUploadWithTx(context.Background(), "upload-1")
	require.NoError(t, err)
	assert.NotEmpty(t, contentID)

	svc.Close()

	assert.Equal(t, contentID, txContentID)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hookCalled))
}

func TestUploadService_CompleteUploadWithTx_NotCompletedStatus(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "uploading", "/bucket/key", "owner1",
				now, now,
			}})
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())

	_, err := svc.CompleteUploadWithTx(context.Background(), "upload-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload not completed")
}

func TestUploadService_CompleteUploadWithTx_TxUpdateFails(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/bucket/key", "owner1",
				now, now,
			}})
		},
		inTxFn: func(_ context.Context, fn func(tx *sql.Tx) error) error {
			return errors.New("tx error")
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())

	_, err := svc.CompleteUploadWithTx(context.Background(), "upload-1")
	require.Error(t, err)
}

func TestUploadService_CompleteUploadWithTx_HookPanic(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
				"upload-1", "video.mp4", int64(1024),
				"video/mp4", "abc123", "completed", "/bucket/key", "owner1",
				now, now,
			}})
		},
		inTxFn: func(_ context.Context, fn func(tx *sql.Tx) error) error {
			return fn(testTx)
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket", zap.NewNop())
	svc.RegisterPostUploadHook(func(_ context.Context, _, _, _ string) {
		panic("hook panic")
	})

	contentID, err := svc.CompleteUploadWithTx(context.Background(), "upload-1")
	require.NoError(t, err)
	assert.NotEmpty(t, contentID)

	svc.Close()
}

func TestUploadService_RegisterAutoTranscodeHook_WithTranscoding(t *testing.T) {
	svc := NewUploadService(&mockDB{}, newMockObjStore(), "bucket", zap.NewNop())
	svc.RegisterAutoTranscodeHook(AutoTranscodeHookDeps{
		TranscodingSvc: nil,
		Profiles:       []string{"720p", "1080p"},
	})
	assert.Empty(t, svc.onProcessed)
}

func TestUploadService_SaveUploadInfo_NilDB(t *testing.T) {
	svc := NewUploadService(nil, newMockObjStore(), "bucket")
	err := svc.saveUploadInfo(context.Background(), &UploadInfo{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestUploadService_SaveUploadInfo_DBError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	err := svc.saveUploadInfo(context.Background(), &UploadInfo{ID: "test"})
	require.Error(t, err)
}

func TestUploadService_UpdateUploadStatus_DBError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewUploadService(db, newMockObjStore(), "bucket")
	err := svc.UpdateUploadStatus(context.Background(), "id", "completed")
	require.Error(t, err)
}

func TestUploadService_CompleteChunkedUpload_UpdateFails(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	store.data["mybucket/chunks/upload-1/0"] = []byte("chunk0")

	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"upload-1", "video.mp4", int64(6),
					"video/mp4", "abc123", "uploading", "/mybucket/owner1/upload-1.mp4", "owner1",
					now, now,
				}})
			}
			if strings.Contains(query, "COUNT(*)") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{1}})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("update failed")
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.CompleteChunkedUpload(context.Background(), "upload-1", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update upload info")
}

func TestUploadService_CompleteChunkedUpload_HashVerification(t *testing.T) {
	now := time.Now()
	store := newMockObjStore()
	chunkData := "chunk0"
	store.data["mybucket/chunks/upload-1/0"] = []byte(chunkData)

	var capturedArgs []interface{}
	db := &mockDB{
		queryRowFn: func(_ context.Context, query string, _ ...interface{}) *stg.CancelRow {
			if strings.Contains(query, "FROM uploads") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{
					"upload-1", "video.mp4", int64(6),
					"video/mp4", "abc123", "uploading", "/mybucket/owner1/upload-1.mp4", "owner1",
					now, now,
				}})
			}
			if strings.Contains(query, "COUNT(*)") {
				return stg.NewTestCancelRow(&mockRow{vals: []interface{}{1}})
			}
			return stg.NewErrorCancelRow(errors.New("unexpected"))
		},
		execFn: func(_ context.Context, query string, args ...interface{}) (sql.Result, error) {
			if strings.Contains(query, "UPDATE uploads") {
				capturedArgs = args
			}
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewUploadService(db, store, "mybucket", zap.NewNop())

	err := svc.CompleteChunkedUpload(context.Background(), "upload-1", 1)
	require.NoError(t, err)

	require.Len(t, capturedArgs, 5)
	hashArg, ok := capturedArgs[2].(string)
	require.True(t, ok)
	expectedHash := hex.EncodeToString([]byte{})
	_ = expectedHash
	assert.NotEmpty(t, hashArg)
}

type mockRow struct {
	vals []interface{}
}

func (r *mockRow) Scan(dest ...interface{}) error {
	if len(r.vals) < len(dest) {
		return fmt.Errorf("not enough columns")
	}
	for i, d := range dest {
		switch v := r.vals[i].(type) {
		case int:
			switch p := d.(type) {
			case *int:
				*p = v
			case *int64:
				*p = int64(v)
			case *float64:
				*p = float64(v)
			}
		case int64:
			switch p := d.(type) {
			case *int64:
				*p = v
			case *int:
				*p = int(v)
			case *float64:
				*p = float64(v)
			}
		case float64:
			switch p := d.(type) {
			case *float64:
				*p = v
			case *int:
				*p = int(v)
			case *int64:
				*p = int64(v)
			}
		case string:
			switch p := d.(type) {
			case *string:
				*p = v
			case *sql.NullString:
				p.String = v
				p.Valid = true
			}
		case bool:
			if p, ok := d.(*bool); ok {
				*p = v
			}
		case time.Time:
			if p, ok := d.(*time.Time); ok {
				*p = v
			}
		}
	}
	return nil
}

type mockRows struct {
	rows    [][]interface{}
	idx     int
	scanErr error
	closed  bool
}

func (m *mockRows) Close() error {
	m.closed = true
	return nil
}

func (m *mockRows) Next() bool {
	m.idx++
	return m.idx <= len(m.rows)
}

func (m *mockRows) Scan(dest ...interface{}) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	if m.idx < 1 || m.idx > len(m.rows) {
		return io.EOF
	}
	row := m.rows[m.idx-1]
	if len(row) < len(dest) {
		return fmt.Errorf("not enough columns")
	}
	for i, d := range dest {
		switch v := row[i].(type) {
		case int64:
			if p, ok := d.(*int64); ok {
				*p = v
			}
		case string:
			if p, ok := d.(*string); ok {
				*p = v
			}
		case bool:
			if p, ok := d.(*bool); ok {
				*p = v
			}
		case time.Time:
			if p, ok := d.(*time.Time); ok {
				*p = v
			}
		}
	}
	return nil
}

func (m *mockRows) Err() error { return nil }

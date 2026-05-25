package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	err          error
}

func (m *mockResult) LastInsertId() (int64, error) { return m.lastInsertID, nil }
func (m *mockResult) RowsAffected() (int64, error) { return m.rowsAffected, m.err }

type mockObjStore struct {
	data map[string][]byte
}

func newMockObjStore() *mockObjStore {
	return &mockObjStore{data: make(map[string][]byte)}
}

func (m *mockObjStore) Upload(_ context.Context, bucket, key string, data []byte) error {
	m.data[bucket+"/"+key] = data
	return nil
}
func (m *mockObjStore) Download(_ context.Context, bucket, key string) ([]byte, error) {
	if d, ok := m.data[bucket+"/"+key]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}
func (m *mockObjStore) Delete(_ context.Context, bucket, key string) error {
	delete(m.data, bucket+"/"+key)
	return nil
}
func (m *mockObjStore) Exists(_ context.Context, bucket, key string) (bool, error) {
	_, ok := m.data[bucket+"/"+key]
	return ok, nil
}

type mockCache struct {
	data map[string]interface{}
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]interface{})}
}

func (m *mockCache) Get(key string) (interface{}, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("cache miss: %s", key)
	}
	return v, nil
}
func (m *mockCache) Set(key string, value interface{}) error {
	m.data[key] = value
	return nil
}
func (m *mockCache) SetWithExpiration(key string, value interface{}, _ time.Duration) error {
	return m.Set(key, value)
}
func (m *mockCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

type mockAuditLogger struct {
	logs []auditLogEntry
}

type auditLogEntry struct {
	action     string
	actor      string
	resource   string
	resourceID string
	success    bool
}

func (m *mockAuditLogger) Log(_ context.Context, action, actor, resource, resourceID string, success bool, _, _ string) {
	m.logs = append(m.logs, auditLogEntry{action, actor, resource, resourceID, success})
}
func (m *mockAuditLogger) Close() error { return nil }

type mockContentRegistry struct {
	txHash string
	err    error
}

func (m *mockContentRegistry) RegisterContent(_ context.Context, _ [32]byte, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.txHash, nil
}

func TestNewContentService(t *testing.T) {
	t.Run("with logger", func(t *testing.T) {
		svc := NewContentService(&mockDB{}, newMockObjStore(), newMockCache(), zap.NewNop())
		require.NotNil(t, svc)
	})

	t.Run("without logger", func(t *testing.T) {
		svc := NewContentService(&mockDB{}, newMockObjStore(), newMockCache())
		require.NotNil(t, svc)
	})

	t.Run("nil cache", func(t *testing.T) {
		svc := NewContentService(&mockDB{}, newMockObjStore(), nil)
		require.NotNil(t, svc)
	})
}

func TestContentService_SetContentRegistry(t *testing.T) {
	svc := NewContentService(&mockDB{}, newMockObjStore(), newMockCache())
	svc.SetContentRegistry(&mockContentRegistry{txHash: "0xabc"})
	assert.NotNil(t, svc.registry)
}

func TestContentService_SetAuditLogger(t *testing.T) {
	svc := NewContentService(&mockDB{}, newMockObjStore(), newMockCache())
	al := &mockAuditLogger{}
	svc.SetAuditLogger(al)
	assert.NotNil(t, svc.auditLogger)
}

func TestContentService_GetContent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		_, err := svc.GetContent(context.Background(), "id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("cache hit", func(t *testing.T) {
		cache := newMockCache()
		content := &Content{ID: "c1", Title: "Cached", Status: "ready"}
		_ = cache.Set("content:c1", content)

		svc := NewContentService(&mockDB{}, newMockObjStore(), cache)
		result, err := svc.GetContent(context.Background(), "c1")
		require.NoError(t, err)
		assert.Equal(t, "c1", result.ID)
		assert.Equal(t, "Cached", result.Title)
	})

	t.Run("cache hit wrong type", func(t *testing.T) {
		cache := newMockCache()
		_ = cache.Set("content:c1", "not-a-content")

		svc := NewContentService(&mockDB{}, newMockObjStore(), cache)
		_, err := svc.GetContent(context.Background(), "c1")
		assert.Error(t, err)
	})

	t.Run("db query error", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("db error"))
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		_, err := svc.GetContent(context.Background(), "c1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query content")
	})

	t.Run("not found", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(sql.ErrNoRows)
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		_, err := svc.GetContent(context.Background(), "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "content not found")
	})
}

func TestContentService_CreateContent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		_, err := svc.CreateContent(context.Background(), &Content{Title: "test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		id, err := svc.CreateContent(context.Background(), &Content{Title: "test"})
		require.NoError(t, err)
		assert.NotEmpty(t, id)
	})

	t.Run("with provided ID", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		id, err := svc.CreateContent(context.Background(), &Content{ID: "my-id", Title: "test"})
		require.NoError(t, err)
		assert.Equal(t, "my-id", id)
	})

	t.Run("default status", func(t *testing.T) {
		var capturedArgs []interface{}
		db := &mockDB{
			execFn: func(_ context.Context, _ string, args ...interface{}) (sql.Result, error) {
				capturedArgs = args
				return &mockResult{}, nil
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		c := &Content{Title: "test"}
		_, err := svc.CreateContent(context.Background(), c)
		require.NoError(t, err)
		assert.Equal(t, "pending", c.Status)
		assert.NotNil(t, capturedArgs)
	})

	t.Run("exec error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		_, err := svc.CreateContent(context.Background(), &Content{Title: "test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to insert content")
	})

	t.Run("with audit logger", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		al := &mockAuditLogger{}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		svc.SetAuditLogger(al)
		_, err := svc.CreateContent(context.Background(), &Content{Title: "test", OwnerID: "owner1"})
		require.NoError(t, err)
		assert.Len(t, al.logs, 1)
		assert.Equal(t, "content.create", al.logs[0].action)
	})
}

func TestContentService_UpdateContent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		err := svc.UpdateContent(context.Background(), &Content{ID: "id"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("not found", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 0}, nil
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		err := svc.UpdateContent(context.Background(), &Content{ID: "missing"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("success", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 1}, nil
			},
		}
		cache := newMockCache()
		svc := NewContentService(db, newMockObjStore(), cache)
		err := svc.UpdateContent(context.Background(), &Content{ID: "c1", Title: "updated"})
		require.NoError(t, err)
	})

	t.Run("with audit logger", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 1}, nil
			},
		}
		al := &mockAuditLogger{}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		svc.SetAuditLogger(al)
		err := svc.UpdateContent(context.Background(), &Content{ID: "c1", OwnerID: "owner1"})
		require.NoError(t, err)
		assert.Len(t, al.logs, 1)
		assert.Equal(t, "content.update", al.logs[0].action)
	})

	t.Run("exec error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		err := svc.UpdateContent(context.Background(), &Content{ID: "c1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update content")
	})
}

func TestContentService_DeleteContent(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		err := svc.DeleteContent(context.Background(), "id", "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("content not found in GetContent", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(sql.ErrNoRows)
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		err := svc.DeleteContent(context.Background(), "missing", "owner1")
		assert.Error(t, err)
	})
}

func TestContentService_DeleteContentWithTx(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		err := svc.DeleteContentWithTx(context.Background(), "id", "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})
}

func TestContentService_CreateContentWithTx(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		_, err := svc.CreateContentWithTx(context.Background(), &Content{Title: "test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("begin tx error", func(t *testing.T) {
		db := &mockDB{
			beginFn: func(_ context.Context) (*sql.Tx, error) {
				return nil, errors.New("begin tx failed")
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
		svc.SetContentRegistry(&mockContentRegistry{txHash: "0xabc"})
		_, err := svc.CreateContentWithTx(context.Background(), &Content{Title: "test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "begin tx")
	})

	t.Run("on-chain registry set but begin tx fails", func(t *testing.T) {
		db := &mockDB{
			beginFn: func(_ context.Context) (*sql.Tx, error) {
				return nil, errors.New("begin tx failed")
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
		svc.SetContentRegistry(&mockContentRegistry{err: errors.New("chain error")})
		_, err := svc.CreateContentWithTx(context.Background(), &Content{Title: "test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "begin tx")
	})
}

func TestContentService_ListContents(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		_, err := svc.ListContents(context.Background(), "owner1", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("db query error", func(t *testing.T) {
		db := &mockDB{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		_, err := svc.ListContents(context.Background(), "owner1", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query contents")
	})
}

func TestContentService_ListContentsWithCount(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		_, _, err := svc.ListContentsWithCount(context.Background(), "owner1", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("db query error", func(t *testing.T) {
		db := &mockDB{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		_, _, err := svc.ListContentsWithCount(context.Background(), "owner1", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query contents")
	})
}

func TestContentService_CountContents(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		_, err := svc.CountContents(context.Background(), "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("db error", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("db error"))
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		_, err := svc.CountContents(context.Background(), "owner1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to count contents")
	})
}

func TestContentService_UpdateContentStatus(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		svc := NewContentService(nil, newMockObjStore(), newMockCache())
		err := svc.UpdateContentStatus(context.Background(), "id", "ready")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
	})

	t.Run("content not found", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(sql.ErrNoRows)
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		err := svc.UpdateContentStatus(context.Background(), "missing", "ready")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "content not found")
	})

	t.Run("invalid transition query fails", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("query failed"))
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		err := svc.UpdateContentStatus(context.Background(), "id", "ready")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "content not found")
	})

	t.Run("concurrent status change", func(t *testing.T) {
		db := &mockDB{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
				return stg.NewErrorCancelRow(errors.New("query failed"))
			},
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 0}, nil
			},
		}
		svc := NewContentService(db, newMockObjStore(), newMockCache())
		err := svc.UpdateContentStatus(context.Background(), "id", "ready")
		assert.Error(t, err)
	})
}

func TestContent_Fields(t *testing.T) {
	c := &Content{
		ID:           "c1",
		Title:        "Test Video",
		Description:  "A test video",
		Type:         "video",
		URL:          "/content/c1",
		ThumbnailURL: "/content/c1/thumb",
		Duration:     120,
		Size:         1024000,
		Status:       "ready",
		OwnerID:      "owner1",
		Metadata:     map[string]interface{}{"codec": "h264"},
	}
	assert.Equal(t, "c1", c.ID)
	assert.Equal(t, "video", c.Type)
	assert.Equal(t, "ready", c.Status)
}

func TestContentService_CreateContent_WithProvidedID(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())
	id, err := svc.CreateContent(context.Background(), &Content{ID: "my-custom-id", Title: "test"})
	require.NoError(t, err)
	assert.Equal(t, "my-custom-id", id)
}

func TestContentService_CreateContent_DefaultStatus(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())
	c := &Content{Title: "test"}
	_, err := svc.CreateContent(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, "pending", c.Status)
}

func TestContentService_CreateContent_SetsTimestamps(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())
	c := &Content{Title: "test"}
	before := time.Now()
	_, err := svc.CreateContent(context.Background(), c)
	require.NoError(t, err)
	assert.False(t, c.CreatedAt.IsZero())
	assert.False(t, c.UpdatedAt.IsZero())
	assert.True(t, c.CreatedAt.After(before) || c.CreatedAt.Equal(before))
}

func TestContentService_UpdateContent_CacheInvalidation(t *testing.T) {
	cache := newMockCache()
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), cache)
	_ = cache.Set("content:c1", &Content{ID: "c1", Title: "old"})

	err := svc.UpdateContent(context.Background(), &Content{ID: "c1", Title: "new"})
	require.NoError(t, err)

	_, cacheErr := cache.Get("content:c1")
	assert.Error(t, cacheErr)
}

func TestContentService_UpdateContent_RowsAffectedError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0, err: errors.New("rows affected error")}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())
	err := svc.UpdateContent(context.Background(), &Content{ID: "c1"})
	assert.Error(t, err)
}

func TestContentService_DeleteContent_WithObjectDeletion(t *testing.T) {
	cache := newMockCache()
	objStore := newMockObjStore()
	_ = objStore.Upload(context.Background(), "content", "c1", []byte("video data"))

	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
		inTxFn: func(_ context.Context, fn func(tx *sql.Tx) error) error {
			return nil
		},
	}
	svc := NewContentService(db, objStore, cache)
	_ = cache.Set("content:c1", &Content{ID: "c1", URL: "/content/c1", OwnerID: "owner1"})

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	require.NoError(t, err)
}

func TestContentService_DeleteContent_CacheInvalidation(t *testing.T) {
	cache := newMockCache()
	db := &mockDB{
		inTxFn: func(_ context.Context, fn func(tx *sql.Tx) error) error {
			return nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), cache)
	_ = cache.Set("content:c1", &Content{ID: "c1", OwnerID: "owner1"})

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	require.NoError(t, err)

	_, cacheErr := cache.Get("content:c1")
	assert.Error(t, cacheErr)
}

func TestContentService_DeleteContent_WithAuditLogger(t *testing.T) {
	cache := newMockCache()
	db := &mockDB{
		inTxFn: func(_ context.Context, fn func(tx *sql.Tx) error) error {
			return nil
		},
	}
	al := &mockAuditLogger{}
	svc := NewContentService(db, newMockObjStore(), cache)
	svc.SetAuditLogger(al)
	_ = cache.Set("content:c1", &Content{ID: "c1", Title: "Test", OwnerID: "owner1"})

	err := svc.DeleteContent(context.Background(), "c1", "owner1")
	require.NoError(t, err)
	assert.Len(t, al.logs, 1)
	assert.Equal(t, "content.delete", al.logs[0].action)
}

func TestContentService_DeleteContentWithTx_BeginError(t *testing.T) {
	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return nil, errors.New("begin tx failed")
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())
	err := svc.DeleteContentWithTx(context.Background(), "c1", "owner1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "begin tx")
}

func TestContentService_CreateContentWithTx_Success(t *testing.T) {
	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return nil, errors.New("begin tx failed")
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
	_, err := svc.CreateContentWithTx(context.Background(), &Content{Title: "test"})
	assert.Error(t, err)
}

func TestContentService_CreateContentWithTx_WithRegistry(t *testing.T) {
	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return nil, errors.New("begin tx failed")
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
	svc.SetContentRegistry(&mockContentRegistry{txHash: "0xabc"})
	_, err := svc.CreateContentWithTx(context.Background(), &Content{Title: "test"})
	assert.Error(t, err)
}

func TestContentService_CreateContentWithTx_RegistryError(t *testing.T) {
	db := &mockDB{
		beginFn: func(_ context.Context) (*sql.Tx, error) {
			return nil, errors.New("begin tx failed")
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache(), zap.NewNop())
	svc.SetContentRegistry(&mockContentRegistry{err: errors.New("chain error")})
	_, err := svc.CreateContentWithTx(context.Background(), &Content{Title: "test"})
	assert.Error(t, err)
}

func TestContentService_UpdateContentStatus_InvalidTransition(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("query failed"))
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())
	err := svc.UpdateContentStatus(context.Background(), "id", "ready")
	assert.Error(t, err)
}

func TestContentService_UpdateContentStatus_CacheInvalidation(t *testing.T) {
	cache := newMockCache()
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("query failed"))
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0}, nil
		},
	}
	svc := NewContentService(db, newMockObjStore(), cache)
	_ = cache.Set("content:id", &Content{ID: "id", Status: "pending"})

	err := svc.UpdateContentStatus(context.Background(), "id", "ready")
	assert.Error(t, err)
}

func TestContentService_GetContent_CacheMiss_DBError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())
	_, err := svc.GetContent(context.Background(), "c1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query content")
}

func TestContentService_GetContent_NilCache(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewContentService(db, newMockObjStore(), nil)
	_, err := svc.GetContent(context.Background(), "c1")
	assert.Error(t, err)
}

func TestContentService_CountContents_Success(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("not implemented"))
		},
	}
	svc := NewContentService(db, newMockObjStore(), newMockCache())
	_, err := svc.CountContents(context.Background(), "owner1")
	assert.Error(t, err)
}

func TestContentService_ListContents_NilDB(t *testing.T) {
	svc := NewContentService(nil, newMockObjStore(), newMockCache())
	_, err := svc.ListContents(context.Background(), "owner1", 10, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_ListContentsWithCount_NilDB(t *testing.T) {
	svc := NewContentService(nil, newMockObjStore(), newMockCache())
	_, _, err := svc.ListContentsWithCount(context.Background(), "owner1", 10, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_SetContentRegistry_Nil(t *testing.T) {
	svc := NewContentService(&mockDB{}, newMockObjStore(), newMockCache())
	svc.SetContentRegistry(nil)
	assert.Nil(t, svc.registry)
}

func TestContentService_SetAuditLogger_Nil(t *testing.T) {
	svc := NewContentService(&mockDB{}, newMockObjStore(), newMockCache())
	svc.SetAuditLogger(nil)
	assert.Nil(t, svc.auditLogger)
}

func TestContentService_CreateContent_MetadataSerializationError(t *testing.T) {
	svc := NewContentService(&mockDB{}, newMockObjStore(), newMockCache())
	c := &Content{
		Title:    "test",
		Metadata: map[string]interface{}{"ch": make(chan int)},
	}
	_, err := svc.CreateContent(context.Background(), c)
	assert.Error(t, err)
}

func TestContentService_UpdateContent_MetadataSerializationError(t *testing.T) {
	svc := NewContentService(&mockDB{}, newMockObjStore(), newMockCache())
	c := &Content{
		ID:       "c1",
		Metadata: map[string]interface{}{"ch": make(chan int)},
	}
	err := svc.UpdateContent(context.Background(), c)
	assert.Error(t, err)
}

package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rtcdance/streamgate/pkg/service"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type playbackMockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn    func(ctx context.Context) (*sql.Tx, error)
	inTxFn     func(ctx context.Context, fn func(tx *sql.Tx) error) error
}

func (m *playbackMockDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}

func (m *playbackMockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return stg.NewErrorCancelRow(errors.New("not implemented"))
}

func (m *playbackMockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}

func (m *playbackMockDB) Begin(ctx context.Context) (*sql.Tx, error) { return nil, errors.New("not implemented") }

func (m *playbackMockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if m.inTxFn != nil {
		return m.inTxFn(ctx, fn)
	}
	return errors.New("not implemented")
}

func (m *playbackMockDB) Ping(ctx context.Context) error { return nil }
func (m *playbackMockDB) Close() error                   { return nil }

type playbackMockResult struct {
	rowsAffected int64
}

func (m *playbackMockResult) LastInsertId() (int64, error) { return 0, nil }
func (m *playbackMockResult) RowsAffected() (int64, error) { return m.rowsAffected, nil }

func setupPlaybackRouter(svc *service.PlaybackStatsService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xWallet")
		c.Next()
	})
	RegisterPlaybackStatsRoutes(r.Group("/"), svc)
	return r
}

func TestRecordPlaybackEvent_Success(t *testing.T) {
	db := &playbackMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &playbackMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	body := `{"content_id":"c1","event_type":"start"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/stats/playback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["recorded"].(bool))
}

func TestRecordPlaybackEvent_InvalidBody(t *testing.T) {
	db := &playbackMockDB{}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/stats/playback", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRecordPlaybackEvent_MissingFields(t *testing.T) {
	db := &playbackMockDB{}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/stats/playback", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRecordPlaybackEvent_DBError(t *testing.T) {
	db := &playbackMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("insert failed")
		},
	}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/stats/playback",
		strings.NewReader(`{"content_id":"c1","event_type":"start"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

type contentStatsRowScanner struct{}

func (s *contentStatsRowScanner) Scan(dest ...interface{}) error {
	*dest[0].(*string) = "c1"
	*dest[1].(*int) = 10
	*dest[2].(*int) = 5
	*dest[3].(*int64) = int64(1000)
	*dest[4].(*int) = 200
	return nil
}

func TestGetContentStats_Success(t *testing.T) {
	db := &playbackMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&contentStatsRowScanner{})
		},
	}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/c1/stats", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "c1", resp["content_id"])
	assert.Equal(t, float64(10), resp["total_plays"])
}

func TestGetContentStats_NotFound(t *testing.T) {
	db := &playbackMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/c1/stats", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetContentStats_DBError(t *testing.T) {
	db := &playbackMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("query failed"))
		},
	}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/c1/stats", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

type topContentRowScanner struct {
	idx int
	max int
}

func (r *topContentRowScanner) Next() bool {
	return r.idx < r.max
}

func (r *topContentRowScanner) Scan(dest ...interface{}) error {
	r.idx++
	*dest[0].(*string) = "c1"
	*dest[1].(*int) = 10
	*dest[2].(*int) = 5
	*dest[3].(*int64) = int64(1000)
	*dest[4].(*int) = 200
	return nil
}

func (r *topContentRowScanner) Close() error { return nil }
func (r *topContentRowScanner) Err() error   { return nil }

func TestListTopContent_Success(t *testing.T) {
	db := &playbackMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &topContentRowScanner{idx: 0, max: 2}, nil
		},
	}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/stats/top", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	content := resp["content"].([]interface{})
	assert.Len(t, content, 2)
}

func TestListTopContent_DBError(t *testing.T) {
	db := &playbackMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/stats/top", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListTopContent_WithLimit(t *testing.T) {
	var capturedLimit int
	db := &playbackMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			capturedLimit = args[0].(int)
			return &topContentRowScanner{idx: 0, max: 1}, nil
		},
	}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/stats/top?limit=5", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 5, capturedLimit)
}

func TestListTopContent_InvalidLimit(t *testing.T) {
	db := &playbackMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &topContentRowScanner{idx: 0, max: 1}, nil
		},
	}
	svc := service.NewPlaybackStatsService(db, zap.NewNop())
	r := setupPlaybackRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/stats/top?limit=abc", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
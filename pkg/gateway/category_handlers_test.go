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

type categoryMockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn    func(ctx context.Context) (*sql.Tx, error)
	inTxFn     func(ctx context.Context, fn func(tx *sql.Tx) error) error
}

func (m *categoryMockDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}

func (m *categoryMockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return stg.NewErrorCancelRow(errors.New("not implemented"))
}

func (m *categoryMockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}

func (m *categoryMockDB) Begin(ctx context.Context) (*sql.Tx, error) { return nil, errors.New("not implemented") }

func (m *categoryMockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if m.inTxFn != nil {
		return m.inTxFn(ctx, fn)
	}
	return errors.New("not implemented")
}

func (m *categoryMockDB) Ping(ctx context.Context) error { return nil }
func (m *categoryMockDB) Close() error                   { return nil }

type categoryMockResult struct {
	rowsAffected int64
}

func (m *categoryMockResult) LastInsertId() (int64, error) { return 0, nil }
func (m *categoryMockResult) RowsAffected() (int64, error) { return m.rowsAffected, nil }

func setupCategoryRouter(svc *service.CategoryService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterCategoryRoutes(r.Group("/"), svc)
	return r
}

func TestListCategories_Success(t *testing.T) {
	db := &categoryMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("no rows expected here")
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/categories", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListCategories_DBError(t *testing.T) {
	db := &categoryMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/categories", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateCategory_Success(t *testing.T) {
	db := &categoryMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &categoryMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	body := `{"name":"test","slug":"test-slug"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/categories", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp["id"])
	assert.NotNil(t, resp["category"])
}

func TestCreateCategory_InvalidBody(t *testing.T) {
	db := &categoryMockDB{}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/categories", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateCategory_MissingRequired(t *testing.T) {
	db := &categoryMockDB{}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/categories", strings.NewReader(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateCategory_DBError(t *testing.T) {
	db := &categoryMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("insert failed")
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/categories", strings.NewReader(`{"name":"test","slug":"test-slug"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

type catRowScanner struct {
	id, name, slug, desc string
	parentID             *sql.NullString
}

func (s *catRowScanner) Scan(dest ...interface{}) error {
	*dest[0].(*string) = s.id
	*dest[1].(*string) = s.name
	*dest[2].(*string) = s.slug
	*dest[3].(*string) = s.desc
	if s.parentID != nil {
		*dest[4].(*sql.NullString) = *s.parentID
	} else {
		*dest[4].(*sql.NullString) = sql.NullString{Valid: false}
	}
	return nil
}

func TestGetCategory_Success(t *testing.T) {
	db := &categoryMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&catRowScanner{
				id: "cat1", name: "Test", slug: "test", desc: "desc",
			})
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/categories/cat1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "Test", resp["name"])
}

func TestGetCategory_NotFound(t *testing.T) {
	db := &categoryMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/categories/missing", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateCategory_Success(t *testing.T) {
	db := &categoryMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&catRowScanner{
				id: "cat1", name: "Old", slug: "old", desc: "old desc",
			})
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &categoryMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/categories/cat1",
		strings.NewReader(`{"name":"New","slug":"new-slug","description":"new desc"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	cat := resp["category"].(map[string]interface{})
	assert.Equal(t, "New", cat["name"])
	assert.Equal(t, "new-slug", cat["slug"])
}

func TestUpdateCategory_NotFound(t *testing.T) {
	db := &categoryMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/categories/missing",
		strings.NewReader(`{"name":"New"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateCategory_InvalidBody(t *testing.T) {
	db := &categoryMockDB{}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/categories/cat1", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateCategory_DBError(t *testing.T) {
	db := &categoryMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&catRowScanner{
				id: "cat1", name: "Old", slug: "old", desc: "",
			})
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("update failed")
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/categories/cat1",
		strings.NewReader(`{"name":"New"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeleteCategory_Success(t *testing.T) {
	db := &categoryMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &categoryMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/categories/cat1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["deleted"].(bool))
}

func TestDeleteCategory_NotFound(t *testing.T) {
	db := &categoryMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &categoryMockResult{rowsAffected: 0}, nil
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/categories/missing", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestBindContentCategory_Success(t *testing.T) {
	db := &categoryMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &categoryMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content/content1/categories/cat1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["bound"].(bool))
}

func TestBindContentCategory_DBError(t *testing.T) {
	db := &categoryMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("bind failed")
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content/content1/categories/cat1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUnbindContentCategory_Success(t *testing.T) {
	db := &categoryMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &categoryMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/content/content1/categories/cat1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["unbound"].(bool))
}

func TestUnbindContentCategory_DBError(t *testing.T) {
	db := &categoryMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("unbind failed")
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/content/content1/categories/cat1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

type contentIDRowScanner struct {
	ids  []string
	idx  int
}

func (s *contentIDRowScanner) Next() bool {
	return s.idx < len(s.ids)
}

func (s *contentIDRowScanner) Scan(dest ...interface{}) error {
	*dest[0].(*string) = s.ids[s.idx]
	s.idx++
	return nil
}

func (s *contentIDRowScanner) Close() error { return nil }
func (s *contentIDRowScanner) Err() error   { return nil }

func TestListContentByCategory_Success(t *testing.T) {
	db := &categoryMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &contentIDRowScanner{ids: []string{"c1", "c2"}}, nil
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/categories/cat1/content", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	ids := resp["content_ids"].([]interface{})
	assert.Len(t, ids, 2)
}

func TestListContentByCategory_DBError(t *testing.T) {
	db := &categoryMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/categories/cat1/content", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListContentByCategory_WithPagination(t *testing.T) {
	var capturedLimit, capturedOffset int
	db := &categoryMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			capturedLimit = args[1].(int)
			capturedOffset = args[2].(int)
			return &contentIDRowScanner{ids: []string{"c1"}}, nil
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/categories/cat1/content?limit=10&offset=5", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 10, capturedLimit)
	assert.Equal(t, 5, capturedOffset)
}

func TestListContentByCategory_InvalidPagination(t *testing.T) {
	db := &categoryMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &contentIDRowScanner{ids: []string{"c1"}}, nil
		},
	}
	svc := service.NewCategoryService(db, zap.NewNop())
	r := setupCategoryRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/categories/cat1/content?limit=abc&offset=-1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
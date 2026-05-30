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

type gatingMockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn    func(ctx context.Context) (*sql.Tx, error)
	inTxFn     func(ctx context.Context, fn func(tx *sql.Tx) error) error
}

func (m *gatingMockDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}

func (m *gatingMockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return stg.NewErrorCancelRow(errors.New("not implemented"))
}

func (m *gatingMockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}

func (m *gatingMockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	return nil, errors.New("not implemented")
}

func (m *gatingMockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if m.inTxFn != nil {
		return m.inTxFn(ctx, fn)
	}
	return errors.New("not implemented")
}

func (m *gatingMockDB) Ping(ctx context.Context) error { return nil }
func (m *gatingMockDB) Close() error                   { return nil }

type gatingMockResult struct {
	rowsAffected int64
}

func (m *gatingMockResult) LastInsertId() (int64, error) { return 0, nil }
func (m *gatingMockResult) RowsAffected() (int64, error) { return m.rowsAffected, nil }

func setupGatingRouter(svc *service.GatingRuleService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterGatingRuleRoutes(r.Group("/"), svc)
	return r
}

func TestListGatingRules_Success(t *testing.T) {
	db := &gatingMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &gatingRuleRowScanner{idx: 0, max: 2}, nil
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/c1/gating-rules", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp["rules"])
}

func TestListGatingRules_DBError(t *testing.T) {
	db := &gatingMockDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/content/c1/gating-rules", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateGatingRule_Success(t *testing.T) {
	db := &gatingMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &gatingMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	body := `{"contract_address":"0xabc"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content/c1/gating-rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp["id"])
}

func TestCreateGatingRule_InvalidBody(t *testing.T) {
	db := &gatingMockDB{}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content/c1/gating-rules", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateGatingRule_MissingContract(t *testing.T) {
	db := &gatingMockDB{}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content/c1/gating-rules", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateGatingRule_DBError(t *testing.T) {
	db := &gatingMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("insert failed")
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/content/c1/gating-rules",
		strings.NewReader(`{"contract_address":"0xabc"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

type gatingRuleQueryRowScanner struct {
	id, contentID, contractAddr, tokenID string
	chainID                              int64
	standard                             string
	minBalance                           int
	isActive                             bool
}

func (s *gatingRuleQueryRowScanner) Scan(dest ...interface{}) error {
	*dest[0].(*string) = s.id
	*dest[1].(*string) = s.contentID
	*dest[2].(*string) = s.contractAddr
	*dest[3].(*string) = s.tokenID
	*dest[4].(*int64) = s.chainID
	*dest[5].(*string) = s.standard
	*dest[6].(*int) = s.minBalance
	*dest[7].(*bool) = s.isActive
	return nil
}

type gatingRuleRowScanner struct {
	idx int
	max int
}

func (r *gatingRuleRowScanner) Next() bool {
	return r.idx < r.max
}

func (r *gatingRuleRowScanner) Scan(dest ...interface{}) error {
	r.idx++
	*dest[0].(*string) = "rule1"
	*dest[1].(*string) = "c1"
	*dest[2].(*string) = "0xabc"
	*dest[3].(*string) = "1"
	*dest[4].(*int64) = 1
	*dest[5].(*string) = "erc721"
	*dest[6].(*int) = 1
	*dest[7].(*bool) = true
	return nil
}

func (r *gatingRuleRowScanner) Close() error { return nil }
func (r *gatingRuleRowScanner) Err() error   { return nil }

func TestUpdateGatingRule_Success(t *testing.T) {
	db := &gatingMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&gatingRuleQueryRowScanner{
				id: "r1", contentID: "c1", contractAddr: "0xabc",
				tokenID: "1", chainID: 1, standard: "erc721", minBalance: 1, isActive: true,
			})
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &gatingMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/gating-rules/r1",
		strings.NewReader(`{"contract_address":"0xdef","is_active":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	rule := resp["rule"].(map[string]interface{})
	assert.Equal(t, "0xdef", rule["contract_address"])
	assert.Equal(t, false, rule["is_active"])
}

func TestUpdateGatingRule_NotFound(t *testing.T) {
	db := &gatingMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/gating-rules/missing",
		strings.NewReader(`{"contract_address":"0xdef"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateGatingRule_InvalidBody(t *testing.T) {
	db := &gatingMockDB{}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/gating-rules/r1", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateGatingRule_DBError(t *testing.T) {
	db := &gatingMockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&gatingRuleQueryRowScanner{
				id: "r1", contentID: "c1", contractAddr: "0xabc",
				tokenID: "1", chainID: 1, standard: "erc721", minBalance: 1, isActive: true,
			})
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("update failed")
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/gating-rules/r1",
		strings.NewReader(`{"contract_address":"0xdef"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeleteGatingRule_Success(t *testing.T) {
	db := &gatingMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &gatingMockResult{rowsAffected: 1}, nil
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/gating-rules/r1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["deleted"].(bool))
}

func TestDeleteGatingRule_NotFound(t *testing.T) {
	db := &gatingMockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &gatingMockResult{rowsAffected: 0}, nil
		},
	}
	svc := service.NewGatingRuleService(db, zap.NewNop())
	r := setupGatingRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/gating-rules/missing", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

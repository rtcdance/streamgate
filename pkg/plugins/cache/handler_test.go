package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestCacheHandler(t *testing.T) *CacheHandler {
	t.Helper()
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	store, err := NewCacheStore(cfg, zap.NewNop())
	require.NoError(t, err)

	return NewCacheHandler(store, zap.NewNop(), kernel)
}

func TestCacheHandler_HealthHandler_Healthy(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.HealthHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCacheHandler_ReadyHandler(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ReadyHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCacheHandler_GetHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/get", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestCacheHandler_GetHandler_MissingKey(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/get", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCacheHandler_GetHandler_NotFound(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/get?key=nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetHandler(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCacheHandler_GetHandler_Success(t *testing.T) {
	handler := newTestCacheHandler(t)
	ctx := context.Background()

	err := handler.store.Set(ctx, "test-key", "test-value", 1*time.Hour)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/get?key=test-key", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCacheHandler_SetHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/set", http.NoBody)
	rec := httptest.NewRecorder()

	handler.SetHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestCacheHandler_SetHandler_InvalidBody(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/set", bytes.NewReader([]byte("bad")))
	rec := httptest.NewRecorder()

	handler.SetHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCacheHandler_SetHandler_Success(t *testing.T) {
	handler := newTestCacheHandler(t)

	body, _ := json.Marshal(map[string]interface{}{
		"key":   "test-key",
		"value": "test-value",
		"ttl":   60,
	})
	req := httptest.NewRequest(http.MethodPost, "/set", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.SetHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCacheHandler_DeleteHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/delete", http.NoBody)
	rec := httptest.NewRecorder()

	handler.DeleteHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestCacheHandler_DeleteHandler_MissingKey(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/delete", http.NoBody)
	rec := httptest.NewRecorder()

	handler.DeleteHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCacheHandler_DeleteHandler_Success(t *testing.T) {
	handler := newTestCacheHandler(t)
	ctx := context.Background()

	err := handler.store.Set(ctx, "test-key", "test-value", 1*time.Hour)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/delete?key=test-key", http.NoBody)
	rec := httptest.NewRecorder()

	handler.DeleteHandler(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestCacheHandler_ClearHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/clear", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ClearHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestCacheHandler_ClearHandler_Success(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/clear", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ClearHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCacheHandler_StatsHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/stats", http.NoBody)
	rec := httptest.NewRecorder()

	handler.StatsHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestCacheHandler_StatsHandler_Success(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/stats", http.NoBody)
	rec := httptest.NewRecorder()

	handler.StatsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCacheHandler_NotFoundHandler(t *testing.T) {
	handler := newTestCacheHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.NotFoundHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNewCacheServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewCacheServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, server)
	assert.NotNil(t, server.store)
}

func TestCacheServer_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	server := &CacheServer{config: cfg, logger: zap.NewNop()}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestCacheServer_Health_NoStore(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	server := &CacheServer{
		config: cfg,
		logger: zap.NewNop(),
		server: &http.Server{},
	}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestCacheStore_Get_NotFound(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	store, err := NewCacheStore(cfg, zap.NewNop())
	require.NoError(t, err)

	_, err = store.Get(context.Background(), "nonexistent")
	assert.Equal(t, ErrNotFound, err)
}

func TestCacheStore_SetAndGet(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	store, err := NewCacheStore(cfg, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()
	err = store.Set(ctx, "key1", "value1", 1*time.Hour)
	require.NoError(t, err)

	val, err := store.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)
}

func TestCacheStore_Delete(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	store, err := NewCacheStore(cfg, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()
	err = store.Set(ctx, "key1", "value1", 1*time.Hour)
	require.NoError(t, err)

	err = store.Delete(ctx, "key1")
	require.NoError(t, err)

	_, err = store.Get(ctx, "key1")
	assert.Equal(t, ErrNotFound, err)
}

func TestCacheStore_Clear(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	store, err := NewCacheStore(cfg, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()
	err = store.Set(ctx, "key1", "value1", 1*time.Hour)
	require.NoError(t, err)

	err = store.Clear(ctx)
	require.NoError(t, err)

	_, err = store.Get(ctx, "key1")
	assert.Equal(t, ErrNotFound, err)
}

func TestCacheStore_Stats(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	store, err := NewCacheStore(cfg, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()
	err = store.Set(ctx, "key1", "value1", 1*time.Hour)
	require.NoError(t, err)

	stats := store.Stats(ctx)
	assert.NotNil(t, stats)
}

func TestCacheStore_Health(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	store, err := NewCacheStore(cfg, zap.NewNop())
	require.NoError(t, err)

	err = store.Health(context.Background())
	assert.NoError(t, err)
}

func TestCacheStore_Health_NilLRU(t *testing.T) {
	store := &CacheStore{logger: zap.NewNop()}

	err := store.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestCacheStore_Close(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	store, err := NewCacheStore(cfg, zap.NewNop())
	require.NoError(t, err)

	err = store.Close()
	assert.NoError(t, err)
}

func TestCachePlugin_NameVersion(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewCachePlugin(cfg, zap.NewNop())

	assert.Equal(t, "cache", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
}

func TestCachePlugin_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewCachePlugin(cfg, zap.NewNop())

	err := plugin.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

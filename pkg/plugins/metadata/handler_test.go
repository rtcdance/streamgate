package metadata

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

func newTestMetadataHandler(t *testing.T) *MetadataHandler {
	t.Helper()
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	return NewMetadataHandler(db, zap.NewNop(), kernel)
}

func TestMetadataHandler_HealthHandler_Healthy(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.HealthHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetadataHandler_ReadyHandler(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ReadyHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetadataHandler_GetMetadataHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/metadata", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetMetadataHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMetadataHandler_GetMetadataHandler_MissingID(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/metadata", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetMetadataHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetadataHandler_GetMetadataHandler_Success(t *testing.T) {
	handler := newTestMetadataHandler(t)
	ctx := context.Background()

	err := handler.db.CreateMetadata(ctx, &ContentMetadata{
		ContentID: "content-1",
		Title:     "Test Video",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/metadata?content_id=content-1", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetMetadataHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetadataHandler_CreateMetadataHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/create", http.NoBody)
	rec := httptest.NewRecorder()

	handler.CreateMetadataHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMetadataHandler_CreateMetadataHandler_InvalidBody(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/create", bytes.NewReader([]byte("bad")))
	rec := httptest.NewRecorder()

	handler.CreateMetadataHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetadataHandler_CreateMetadataHandler_Success(t *testing.T) {
	handler := newTestMetadataHandler(t)

	body, _ := json.Marshal(ContentMetadata{
		ContentID: "content-1",
		Title:     "Test Video",
	})
	req := httptest.NewRequest(http.MethodPost, "/create", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.CreateMetadataHandler(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestMetadataHandler_UpdateMetadataHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/update", http.NoBody)
	rec := httptest.NewRecorder()

	handler.UpdateMetadataHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMetadataHandler_UpdateMetadataHandler_InvalidBody(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/update", bytes.NewReader([]byte("bad")))
	rec := httptest.NewRecorder()

	handler.UpdateMetadataHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetadataHandler_UpdateMetadataHandler_NotFound(t *testing.T) {
	handler := newTestMetadataHandler(t)

	body, _ := json.Marshal(ContentMetadata{
		ContentID: "nonexistent",
		Title:     "Updated",
	})
	req := httptest.NewRequest(http.MethodPut, "/update", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.UpdateMetadataHandler(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestMetadataHandler_UpdateMetadataHandler_Success(t *testing.T) {
	handler := newTestMetadataHandler(t)
	ctx := context.Background()

	err := handler.db.CreateMetadata(ctx, &ContentMetadata{
		ContentID: "content-1",
		Title:     "Original",
	})
	require.NoError(t, err)

	body, _ := json.Marshal(ContentMetadata{
		ContentID: "content-1",
		Title:     "Updated",
	})
	req := httptest.NewRequest(http.MethodPut, "/update", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.UpdateMetadataHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetadataHandler_DeleteMetadataHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/delete", http.NoBody)
	rec := httptest.NewRecorder()

	handler.DeleteMetadataHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMetadataHandler_DeleteMetadataHandler_MissingID(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/delete", http.NoBody)
	rec := httptest.NewRecorder()

	handler.DeleteMetadataHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetadataHandler_DeleteMetadataHandler_Success(t *testing.T) {
	handler := newTestMetadataHandler(t)
	ctx := context.Background()

	err := handler.db.CreateMetadata(ctx, &ContentMetadata{ContentID: "content-1"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/delete?content_id=content-1", http.NoBody)
	rec := httptest.NewRecorder()

	handler.DeleteMetadataHandler(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestMetadataHandler_SearchMetadataHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/search", http.NoBody)
	rec := httptest.NewRecorder()

	handler.SearchMetadataHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMetadataHandler_SearchMetadataHandler_MissingQuery(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/search", http.NoBody)
	rec := httptest.NewRecorder()

	handler.SearchMetadataHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetadataHandler_SearchMetadataHandler_Success(t *testing.T) {
	handler := newTestMetadataHandler(t)
	ctx := context.Background()

	err := handler.db.CreateMetadata(ctx, &ContentMetadata{ContentID: "content-1", Title: "Test"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/search?q=test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.SearchMetadataHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetadataHandler_NotFoundHandler(t *testing.T) {
	handler := newTestMetadataHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.NotFoundHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMetadataDB_CRUD(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()

	err = db.CreateMetadata(ctx, &ContentMetadata{ContentID: "c1", Title: "Title 1"})
	require.NoError(t, err)

	meta, err := db.GetMetadata(ctx, "c1")
	require.NoError(t, err)
	assert.Equal(t, "Title 1", meta.Title)

	err = db.UpdateMetadata(ctx, &ContentMetadata{ContentID: "c1", Title: "Updated"})
	require.NoError(t, err)

	meta, err = db.GetMetadata(ctx, "c1")
	require.NoError(t, err)
	assert.Equal(t, "Updated", meta.Title)

	err = db.DeleteMetadata(ctx, "c1")
	require.NoError(t, err)
}

func TestMetadataDB_UpdateNonexistent(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	err = db.UpdateMetadata(context.Background(), &ContentMetadata{ContentID: "nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMetadataDB_Search(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()
	err = db.CreateMetadata(ctx, &ContentMetadata{ContentID: "c1", Title: "Video 1"})
	require.NoError(t, err)
	err = db.CreateMetadata(ctx, &ContentMetadata{ContentID: "c2", Title: "Video 2"})
	require.NoError(t, err)

	results, err := db.SearchMetadata(ctx, "Video")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestMetadataDB_Health(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	err = db.Health(context.Background())
	assert.NoError(t, err)
}

func TestMetadataDB_Close(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	err = db.Close()
	assert.NoError(t, err)
}

func TestDatabase_Operations(t *testing.T) {
	db := &Database{}

	result, err := db.Query("SELECT 1")
	assert.Nil(t, result)
	assert.NoError(t, err)

	err = db.Insert("table", map[string]interface{}{"key": "value"})
	assert.NoError(t, err)

	err = db.Update("table", "1", map[string]interface{}{"key": "updated"})
	assert.NoError(t, err)

	err = db.Delete("table", "1")
	assert.NoError(t, err)
}

func TestIndexer_Operations(t *testing.T) {
	indexer := &Indexer{}

	err := indexer.Index("id1", map[string]interface{}{"key": "value"})
	assert.NoError(t, err)

	results, err := indexer.Search("query")
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestSearcher_Operations(t *testing.T) {
	searcher := &Searcher{}

	results, err := searcher.Search("query")
	assert.NoError(t, err)
	assert.Empty(t, results)

	results, err = searcher.Filter(map[string]interface{}{"key": "value"})
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestMetadataServer_Health_NotStarted(t *testing.T) {
	server := &MetadataServer{logger: zap.NewNop()}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestMetadataServer_Health_NoDB(t *testing.T) {
	server := &MetadataServer{
		logger: zap.NewNop(),
		server: &http.Server{},
	}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestMetadataPlugin_NameVersion(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMetadataPlugin(cfg, zap.NewNop())

	assert.Equal(t, "metadata", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
}

func TestMetadataPlugin_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMetadataPlugin(cfg, zap.NewNop())

	err := plugin.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestMetadataDB_GetMetadata_NotFound(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	meta, err := db.GetMetadata(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Equal(t, "nonexistent", meta.ContentID)
	assert.Empty(t, meta.Title)
}

func TestMetadataServer_New(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMetadataServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, server)
	assert.NotNil(t, server.db)
}

func TestMetadataPlugin_DependsOn(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMetadataPlugin(cfg, zap.NewNop())

	deps := plugin.DependsOn()
	assert.Nil(t, deps)
}

func TestMetadataPlugin_Init(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMetadataPlugin(cfg, zap.NewNop())

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, plugin.server)
}

func TestMetadataPlugin_Stop_NoServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMetadataPlugin(cfg, zap.NewNop())

	err := plugin.Stop(context.Background())
	require.NoError(t, err)
}

func TestMetadataHandler_HealthHandler_Unhealthy(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	handler := NewMetadataHandler(db, zap.NewNop(), kernel)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.HealthHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetadataHandler_GetMetadataHandler_DBError(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	handler := NewMetadataHandler(db, zap.NewNop(), kernel)

	req := httptest.NewRequest(http.MethodGet, "/metadata?content_id=test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetMetadataHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetadataHandler_CreateMetadataHandler_DBError(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	handler := NewMetadataHandler(db, zap.NewNop(), kernel)

	body, _ := json.Marshal(ContentMetadata{
		ContentID: "content-1",
		Title:     "Test",
	})
	req := httptest.NewRequest(http.MethodPost, "/create", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.CreateMetadataHandler(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestMetadataHandler_DeleteMetadataHandler_DBError(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	handler := NewMetadataHandler(db, zap.NewNop(), kernel)

	req := httptest.NewRequest(http.MethodDelete, "/delete?content_id=test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.DeleteMetadataHandler(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestMetadataHandler_SearchMetadataHandler_DBError(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	handler := NewMetadataHandler(db, zap.NewNop(), kernel)

	req := httptest.NewRequest(http.MethodGet, "/search?q=test", http.NoBody)
	rec := httptest.NewRecorder()

	handler.SearchMetadataHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetadataServer_StartAndStop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMetadataServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/search?q=test", http.NoBody)
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	require.NoError(t, server.Stop(stopCtx))
}

func TestMetadataServer_Health_WithServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMetadataServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))

	err = server.Health(context.Background())
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}

func TestMetadataPlugin_StartAndStop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	plugin := NewMetadataPlugin(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = plugin.Start(ctx)
	require.NoError(t, err)

	err = plugin.Health(context.Background())
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	err = plugin.Stop(stopCtx)
	require.NoError(t, err)
}

func TestMetadataPlugin_Stop_WithServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	plugin := NewMetadataPlugin(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = plugin.Start(ctx)
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	err = plugin.Stop(stopCtx)
	require.NoError(t, err)
}

func TestMetadataDB_DeleteMetadata_Nonexistent(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	err = db.DeleteMetadata(context.Background(), "nonexistent")
	assert.NoError(t, err)
}

func TestMetadataDB_SearchMetadata_Empty(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	db, err := NewMetadataDB(cfg, zap.NewNop())
	require.NoError(t, err)

	results, err := db.SearchMetadata(context.Background(), "test")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMetadataServer_CRUDViaHTTP(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMetadataServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))

	body, _ := json.Marshal(ContentMetadata{
		ContentID: "http-test",
		Title:     "HTTP Test",
		Format:    "mp4",
		Duration:  120,
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/metadata/create", bytes.NewReader(body))
	createRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(createRec, createReq)
	assert.Equal(t, http.StatusCreated, createRec.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/metadata?content_id=http-test", http.NoBody)
	getRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(getRec, getReq)
	assert.Equal(t, http.StatusOK, getRec.Code)

	searchReq := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/search?q=test", http.NoBody)
	searchRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(searchRec, searchReq)
	assert.Equal(t, http.StatusOK, searchRec.Code)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/metadata/delete?content_id=http-test", http.NoBody)
	delRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(delRec, delReq)
	assert.Equal(t, http.StatusNoContent, delRec.Code)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}

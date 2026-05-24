package upload

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUploadServer_StartAndStop(t *testing.T) {
	svc := service.NewUploadService(nil, newMockObjStore(), "test-bucket", zap.NewNop())
	cfg := &config.Config{
		Mode:   "monolith",
		Server: config.ServerConfig{Port: 0, ReadTimeout: 5, WriteTimeout: 5},
	}
	server := &UploadServer{
		config: cfg,
		logger: zap.NewNop(),
		svc:    svc,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	err = server.Stop(ctx)
	require.NoError(t, err)
}

func TestUploadServer_Stop_WithHTTPServer(t *testing.T) {
	svc := service.NewUploadService(nil, newMockObjStore(), "test-bucket", zap.NewNop())
	httpSrv := &http.Server{Addr: ":0"}
	server := &UploadServer{
		logger: zap.NewNop(),
		svc:    svc,
		server: httpSrv,
	}

	go func() {
		_ = httpSrv.ListenAndServe()
	}()
	time.Sleep(50 * time.Millisecond)

	err := server.Stop(context.Background())
	require.NoError(t, err)
}

func TestUploadPlugin_Init_AndLifecycle(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create upload server")
}

func TestUploadPlugin_Start_NoServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	assert.Panics(t, func() {
		_ = plugin.Start(context.Background())
	})
}

func TestUploadPlugin_Stop_WithServerError(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	plugin.server = &UploadServer{
		logger: zap.NewNop(),
		svc:    service.NewUploadService(nil, newMockObjStore(), "b", zap.NewNop()),
	}
	err := plugin.Stop(context.Background())
	require.NoError(t, err)
}

func TestCreateObjectStorage_MinIO(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Type:      "minio",
			Endpoint:  "localhost:9000",
			AccessKey: "minioadmin",
			SecretKey: "minioadmin",
			UseSSL:    false,
		},
	}
	store, err := createObjectStorage(cfg, zap.NewNop())
	assert.NoError(t, err)
	assert.NotNil(t, store)
}

func TestCreateObjectStorage_S3(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Type:      "s3",
			Region:    "us-east-1",
			AccessKey: "test",
			SecretKey: "test",
			Endpoint:  "http://localhost:4566",
		},
	}
	store, err := createObjectStorage(cfg, zap.NewNop())
	assert.NoError(t, err)
	assert.NotNil(t, store)
}

func TestUploadHandler_CompleteUploadHandler_UploadIDFromBody(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{"upload_id": "upload-1", "total_chunks": 5})
	req := httptest.NewRequest(http.MethodPost, "/complete", strings.NewReader(string(body)))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_CompleteUploadWithContentHandler_QueryOnly(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/complete-upload?upload_id=upload-1", strings.NewReader(`{}`))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadWithContentHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_DownloadURLHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/download-url?upload_id=upload-1", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.DownloadURLHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_DeleteUploadHandler_NotFoundError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodDelete, "/delete?upload_id=upload-1", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.DeleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadServer_Start_WithHandlers(t *testing.T) {
	svc := service.NewUploadService(nil, newMockObjStore(), "test-bucket", zap.NewNop())
	cfg := &config.Config{
		Mode:   "monolith",
		Server: config.ServerConfig{Port: 0, ReadTimeout: 5, WriteTimeout: 5},
	}
	server := &UploadServer{
		config: cfg,
		logger: zap.NewNop(),
		svc:    svc,
	}

	err := server.Start(context.Background())
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	err = server.Stop(context.Background())
	require.NoError(t, err)
}

func TestUploadServer_Stop_NilServer_NilSvc(t *testing.T) {
	server := &UploadServer{logger: zap.NewNop()}
	err := server.Stop(context.Background())
	require.NoError(t, err)
}

func TestUploadPlugin_Health_WithServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	plugin.server = &UploadServer{
		logger: zap.NewNop(),
		server: &http.Server{},
		svc:    service.NewUploadService(nil, newMockObjStore(), "b", zap.NewNop()),
	}
	err := plugin.Health(context.Background())
	require.NoError(t, err)
}

func TestUploadHandler_UploadChunkHandler_SuccessPath(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/chunk?upload_id=u1&chunk_index=0", strings.NewReader("data"))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.UploadChunkHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_InitChunkedUploadHandler_SuccessPath(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{
		"filename":     "test.mp4",
		"total_size":   1024,
		"total_chunks": 5,
	})
	req := httptest.NewRequest(http.MethodPost, "/init", strings.NewReader(string(body)))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.InitChunkedUploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

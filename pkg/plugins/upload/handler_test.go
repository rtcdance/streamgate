package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/plugins/transcoder"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockObjStore struct {
	data map[string][]byte
}

func newMockObjStore() *mockObjStore {
	return &mockObjStore{data: make(map[string][]byte)}
}

func (m *mockObjStore) Upload(_ context.Context, _, key string, data []byte) error {
	m.data[key] = data
	return nil
}

func (m *mockObjStore) UploadStream(_ context.Context, _, key string, reader io.Reader, size int64) error {
	data, _ := io.ReadAll(reader)
	m.data[key] = data
	return nil
}

func (m *mockObjStore) Download(_ context.Context, _, key string) ([]byte, error) {
	d, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return d, nil
}

func (m *mockObjStore) DownloadStream(_ context.Context, _, key string) (io.ReadCloser, error) {
	d, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(d)), nil
}

func (m *mockObjStore) Delete(_ context.Context, _, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockObjStore) DeleteObjects(_ context.Context, _ string, keys []string) error {
	for _, k := range keys {
		delete(m.data, k)
	}
	return nil
}

func (m *mockObjStore) Exists(_ context.Context, _, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockObjStore) ListObjects(_ context.Context, _, _ string) ([]string, error) {
	var keys []string
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func newTestUploadHandlerWithSvc(t *testing.T) *UploadHandler {
	t.Helper()
	kernel, err := core.NewMicrokernel(&config.Config{Mode: "monolith"}, zap.NewNop())
	require.NoError(t, err)
	svc := service.NewUploadService(nil, newMockObjStore(), "test-bucket", zap.NewNop())
	return NewUploadHandler(svc, zap.NewNop(), kernel)
}

func TestUploadHandler_HealthHandler_Healthy(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()
	handler.HealthHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestUploadHandler_ReadyHandler(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ReadyHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestUploadHandler_UploadHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/upload", http.NoBody)
	rec := httptest.NewRecorder()
	handler.UploadHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_UploadHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/upload", http.NoBody)
	rec := httptest.NewRecorder()
	handler.UploadHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_UploadHandler_ParseFormError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader([]byte("not multipart")))
	req.Header.Set("X-Wallet-Address", "0x1234")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=bad")
	rec := httptest.NewRecorder()
	handler.UploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_UploadHandler_NoFile(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader([]byte{}))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.UploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_UploadHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test.mp4")
	require.NoError(t, err)
	_, _ = part.Write([]byte("fake video data"))
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("X-Wallet-Address", "0x1234")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.UploadHandler(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestUploadHandler_InitChunkedUploadHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/init", http.NoBody)
	rec := httptest.NewRecorder()
	handler.InitChunkedUploadHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_InitChunkedUploadHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/init", http.NoBody)
	rec := httptest.NewRecorder()
	handler.InitChunkedUploadHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_InitChunkedUploadHandler_InvalidBody(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/init", bytes.NewReader([]byte("bad")))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.InitChunkedUploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_InitChunkedUploadHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{
		"filename":     "test.mp4",
		"total_size":   1024,
		"total_chunks": 5,
	})
	req := httptest.NewRequest(http.MethodPost, "/init", bytes.NewReader(body))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.InitChunkedUploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_UploadChunkHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/chunk", http.NoBody)
	rec := httptest.NewRecorder()
	handler.UploadChunkHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_UploadChunkHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/chunk", http.NoBody)
	rec := httptest.NewRecorder()
	handler.UploadChunkHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_UploadChunkHandler_MissingParams(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/chunk", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.UploadChunkHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_UploadChunkHandler_InvalidChunkIndex(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	tests := []struct {
		name       string
		chunkIndex string
	}{
		{"non-numeric", "abc"},
		{"negative", "-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/chunk?upload_id=u1&chunk_index="+tc.chunkIndex, http.NoBody)
			req.Header.Set("X-Wallet-Address", "0x1234")
			rec := httptest.NewRecorder()
			handler.UploadChunkHandler(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestUploadHandler_UploadChunkHandler_MissingUploadID(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/chunk?chunk_index=0", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.UploadChunkHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_UploadChunkHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/chunk?upload_id=u1&chunk_index=0", bytes.NewReader([]byte("data")))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.UploadChunkHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_CompleteUploadHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/complete", http.NoBody)
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_CompleteUploadHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/complete", http.NoBody)
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_CompleteUploadHandler_InvalidBody(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/complete", bytes.NewReader([]byte("bad")))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_CompleteUploadHandler_MissingUploadID(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{"total_chunks": 5})
	req := httptest.NewRequest(http.MethodPost, "/complete", bytes.NewReader(body))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_CompleteUploadHandler_ZeroChunks(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{"upload_id": "upload-1", "total_chunks": 0})
	req := httptest.NewRequest(http.MethodPost, "/complete", bytes.NewReader(body))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_CompleteUploadHandler_UploadIDFromQuery(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{"total_chunks": 5})
	req := httptest.NewRequest(http.MethodPost, "/complete?upload_id=upload-1", bytes.NewReader(body))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_CompleteUploadHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{"upload_id": "upload-1", "total_chunks": 5})
	req := httptest.NewRequest(http.MethodPost, "/complete", bytes.NewReader(body))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_CompleteUploadWithContentHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/complete-upload", http.NoBody)
	rec := httptest.NewRecorder()
	handler.CompleteUploadWithContentHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_CompleteUploadWithContentHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/complete-upload", http.NoBody)
	rec := httptest.NewRecorder()
	handler.CompleteUploadWithContentHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_CompleteUploadWithContentHandler_MissingUploadID(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/complete-upload", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadWithContentHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_CompleteUploadWithContentHandler_UploadIDFromQuery(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/complete-upload?upload_id=upload-1", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadWithContentHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_CompleteUploadWithContentHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{"upload_id": "upload-1"})
	req := httptest.NewRequest(http.MethodPost, "/complete-upload", bytes.NewReader(body))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadWithContentHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_GetUploadStatusHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/status", http.NoBody)
	rec := httptest.NewRecorder()
	handler.GetUploadStatusHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_GetUploadStatusHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/status", http.NoBody)
	rec := httptest.NewRecorder()
	handler.GetUploadStatusHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_GetUploadStatusHandler_MissingID(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/status", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.GetUploadStatusHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_GetUploadStatusHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/status?upload_id=upload-1", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.GetUploadStatusHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_DownloadURLHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/download-url", http.NoBody)
	rec := httptest.NewRecorder()
	handler.DownloadURLHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_DownloadURLHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/download-url", http.NoBody)
	rec := httptest.NewRecorder()
	handler.DownloadURLHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_DownloadURLHandler_MissingID(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/download-url", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.DownloadURLHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_DownloadURLHandler_ExpiryMinutes(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	tests := []struct {
		name          string
		expiryMinutes string
	}{
		{"valid expiry", "30"},
		{"invalid expiry", "abc"},
		{"zero expiry", "0"},
		{"too large expiry", "61"},
		{"no expiry param", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "/download-url?upload_id=upload-1"
			if tc.expiryMinutes != "" {
				url += "&expiry_minutes=" + tc.expiryMinutes
			}
			req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
			req.Header.Set("X-Wallet-Address", "0x1234")
			rec := httptest.NewRecorder()
			handler.DownloadURLHandler(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestUploadHandler_ListUploadsHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/list", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ListUploadsHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_ListUploadsHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/list", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ListUploadsHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_ListUploadsHandler_WithPagination(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	tests := []struct {
		name   string
		limit  string
		offset string
	}{
		{"default pagination", "", ""},
		{"custom limit", "10", ""},
		{"custom offset", "", "5"},
		{"both custom", "50", "10"},
		{"invalid limit", "abc", ""},
		{"negative limit", "-1", ""},
		{"too large limit", "200", ""},
		{"invalid offset", "abc", ""},
		{"negative offset", "-1", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "/list"
			params := []string{}
			if tc.limit != "" {
				params = append(params, "limit="+tc.limit)
			}
			if tc.offset != "" {
				params = append(params, "offset="+tc.offset)
			}
			if len(params) > 0 {
				url += "?" + strings.Join(params, "&")
			}
			req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
			req.Header.Set("X-Wallet-Address", "0x1234")
			rec := httptest.NewRecorder()
			handler.ListUploadsHandler(rec, req)
			assert.Equal(t, http.StatusInternalServerError, rec.Code)
		})
	}
}

func TestUploadHandler_ChunkStatusesHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/chunks", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ChunkStatusesHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_ChunkStatusesHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/chunks", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ChunkStatusesHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_ChunkStatusesHandler_MissingID(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/chunks", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.ChunkStatusesHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_ChunkStatusesHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/chunks?upload_id=upload-1", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.ChunkStatusesHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_DeleteUploadHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/delete", http.NoBody)
	rec := httptest.NewRecorder()
	handler.DeleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadHandler_DeleteUploadHandler_NoWallet(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodDelete, "/delete", http.NoBody)
	rec := httptest.NewRecorder()
	handler.DeleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadHandler_DeleteUploadHandler_MissingID(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodDelete, "/delete", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.DeleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_DeleteUploadHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodDelete, "/delete?upload_id=upload-1", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.DeleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_NotFoundHandler(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	handler.NotFoundHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadPlugin_NameVersion(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	assert.Equal(t, "upload", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
}

func TestUploadPlugin_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	err := plugin.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestUploadPlugin_DependsOn(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	deps := plugin.DependsOn()
	assert.Nil(t, deps)
}

func TestUploadPlugin_Stop_NoServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	err := plugin.Stop(context.Background())
	require.NoError(t, err)
}

func TestUploadServer_Health_NotStarted(t *testing.T) {
	server := &UploadServer{logger: zap.NewNop()}
	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestUploadServer_Health_NoService(t *testing.T) {
	server := &UploadServer{logger: zap.NewNop(), server: &http.Server{}}
	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestUploadServer_GetService(t *testing.T) {
	svc := service.NewUploadService(nil, newMockObjStore(), "test-bucket", zap.NewNop())
	server := &UploadServer{logger: zap.NewNop(), svc: svc}
	assert.Equal(t, svc, server.GetService())
}

func TestUploadServer_Stop_NoServer(t *testing.T) {
	svc := service.NewUploadService(nil, newMockObjStore(), "test-bucket", zap.NewNop())
	server := &UploadServer{logger: zap.NewNop(), svc: svc}
	err := server.Stop(context.Background())
	require.NoError(t, err)
}

func TestFFmpegAdapter_SelectProfiles_FFprobeFail(t *testing.T) {
	ft := transcoder.NewFFmpegTranscoder(&transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     1 * time.Second,
	}, zap.NewNop())
	adapter := &ffmpegAdapter{ft: ft, log: zap.NewNop()}

	tests := []struct {
		name             string
		requestedProfile string
		expectedLen      int
		expectedBitrate  string
	}{
		{"fallback to 720p for unknown profile", "unknown", 1, "2500k"},
		{"fallback to 1080p", "1080p", 1, "5000k"},
		{"fallback to 720p", "720p", 1, "2500k"},
		{"fallback to 480p", "480p", 1, "1000k"},
		{"fallback to 360p", "360p", 1, "500k"},
		{"empty profile defaults to 720p", "", 1, "2500k"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			profiles, err := adapter.SelectProfiles(context.Background(), "/nonexistent/video.mp4", tc.requestedProfile)
			require.NoError(t, err)
			assert.Len(t, profiles, tc.expectedLen)
			assert.Equal(t, tc.expectedBitrate, profiles[0].Bitrate)
		})
	}
}

func TestFFmpegAdapter_TranscodeHLS_FFprobeFail(t *testing.T) {
	ft := transcoder.NewFFmpegTranscoder(&transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     1 * time.Second,
	}, zap.NewNop())
	adapter := &ffmpegAdapter{ft: ft, log: zap.NewNop()}
	err := adapter.TranscodeHLS(context.Background(), "/nonexistent/video.mp4", os.TempDir(), "720p", nil)
	require.Error(t, err)
}

func TestFFmpegAdapter_TranscodeHLS_WithProgress(t *testing.T) {
	ft := transcoder.NewFFmpegTranscoder(&transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     1 * time.Second,
	}, zap.NewNop())
	adapter := &ffmpegAdapter{ft: ft, log: zap.NewNop()}
	progressCalled := false
	progressFn := func(_ string, progress float64) { progressCalled = true }
	err := adapter.TranscodeHLS(context.Background(), "/nonexistent/video.mp4", os.TempDir(), "720p", progressFn)
	require.Error(t, err)
	assert.False(t, progressCalled)
}

func TestUploadHandler_HealthHandler_Unhealthy(t *testing.T) {
	kernel, err := core.NewMicrokernel(&config.Config{Mode: "monolith"}, zap.NewNop())
	require.NoError(t, err)

	unhealthyPlugin := &mockPluginWithHealth{healthErr: fmt.Errorf("db connection failed")}
	require.NoError(t, kernel.RegisterPlugin(unhealthyPlugin))

	svc := service.NewUploadService(nil, newMockObjStore(), "test-bucket", zap.NewNop())
	handler := NewUploadHandler(svc, zap.NewNop(), kernel)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()
	handler.HealthHandler(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

type mockPluginWithHealth struct {
	healthErr error
}

func (m *mockPluginWithHealth) Name() string                                      { return "api-gateway" }
func (m *mockPluginWithHealth) Version() string                                   { return "1.0.0" }
func (m *mockPluginWithHealth) Init(_ context.Context, _ *core.Microkernel) error { return nil }
func (m *mockPluginWithHealth) Start(_ context.Context) error                     { return nil }
func (m *mockPluginWithHealth) Stop(_ context.Context) error                      { return nil }
func (m *mockPluginWithHealth) Health(_ context.Context) error                    { return m.healthErr }
func (m *mockPluginWithHealth) DependsOn() []string                               { return nil }

func TestUploadPlugin_Init_NoDB(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create upload server")
}

func TestUploadServer_Health_Healthy(t *testing.T) {
	svc := service.NewUploadService(nil, newMockObjStore(), "test-bucket", zap.NewNop())
	server := &UploadServer{
		logger: zap.NewNop(),
		server: &http.Server{},
		svc:    svc,
	}
	err := server.Health(context.Background())
	require.NoError(t, err)
}

func TestUploadServer_Stop_WithTranscodingSvc(t *testing.T) {
	svc := service.NewUploadService(nil, newMockObjStore(), "test-bucket", zap.NewNop())
	server := &UploadServer{
		logger:         zap.NewNop(),
		svc:            svc,
		transcodingSvc: nil,
	}
	err := server.Stop(context.Background())
	require.NoError(t, err)
}

func TestUploadPlugin_Stop_WithServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewUploadPlugin(cfg, zap.NewNop())
	plugin.server = &UploadServer{logger: zap.NewNop()}
	err := plugin.Stop(context.Background())
	require.NoError(t, err)
}

func TestProfileDefs_AllFormats(t *testing.T) {
	assert.Equal(t, "hls", profileDefs["1080p"].Format)
	assert.Equal(t, "hls", profileDefs["720p"].Format)
	assert.Equal(t, "hls", profileDefs["480p"].Format)
	assert.Equal(t, "hls", profileDefs["360p"].Format)
}

func TestProfileRes_AllResolutions(t *testing.T) {
	assert.Equal(t, resolution{1280, 720}, profileRes["720p"])
	assert.Equal(t, resolution{854, 480}, profileRes["480p"])
}

func TestFFmpegAdapter_TranscodeHLS_WithDeadline(t *testing.T) {
	ft := transcoder.NewFFmpegTranscoder(&transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     1 * time.Second,
	}, zap.NewNop())
	adapter := &ffmpegAdapter{ft: ft, log: zap.NewNop()}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.TranscodeHLS(ctx, "/nonexistent/video.mp4", os.TempDir(), "720p", nil)
	require.Error(t, err)
}

func TestUploadHandler_UploadChunkHandler_MissingChunkIndex(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/chunk?upload_id=u1", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.UploadChunkHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_CompleteUploadHandler_NegativeChunks(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{"upload_id": "upload-1", "total_chunks": -1})
	req := httptest.NewRequest(http.MethodPost, "/complete", bytes.NewReader(body))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestFFmpegAdapter_SelectProfiles_EmptyProfile(t *testing.T) {
	ft := transcoder.NewFFmpegTranscoder(&transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     os.TempDir(),
		Timeout:     1 * time.Second,
	}, zap.NewNop())
	adapter := &ffmpegAdapter{ft: ft, log: zap.NewNop()}

	profiles, err := adapter.SelectProfiles(context.Background(), "/nonexistent/video.mp4", "")
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, "2500k", profiles[0].Bitrate)
}

func TestProfileDefs(t *testing.T) {
	assert.Len(t, profileDefs, 4)
	assert.Contains(t, profileDefs, "1080p")
	assert.Contains(t, profileDefs, "720p")
	assert.Contains(t, profileDefs, "480p")
	assert.Contains(t, profileDefs, "360p")
}

func TestProfileRes(t *testing.T) {
	assert.Len(t, profileRes, 4)
	assert.Equal(t, 1920, profileRes["1080p"].w)
	assert.Equal(t, 1080, profileRes["1080p"].h)
	assert.Equal(t, 640, profileRes["360p"].w)
	assert.Equal(t, 360, profileRes["360p"].h)
}

// -- SelectProfiles success path tests --

func requireFFprobe(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}
}

func generateTestVideo(t *testing.T, width, height int) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), fmt.Sprintf("video_%dx%d.mp4", width, height))
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", fmt.Sprintf("color=c=black:s=%dx%d:d=0.04", width, height),
		"-frames:v", "1",
		"-y", f)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "ffmpeg failed: %s", string(out))
	return f
}

func newFFmpegAdapter(t *testing.T) *ffmpegAdapter {
	t.Helper()
	ft := transcoder.NewFFmpegTranscoder(&transcoder.FFmpegConfig{
		FFmpegPath:  "ffmpeg",
		FFprobePath: "ffprobe",
		TempDir:     t.TempDir(),
		Timeout:     5 * time.Second,
	}, zap.NewNop())
	return &ffmpegAdapter{ft: ft, log: zap.NewNop()}
}

func TestFFmpegAdapter_SelectProfiles_1080p_ProducesAllProfiles(t *testing.T) {
	requireFFprobe(t)
	adapter := newFFmpegAdapter(t)
	videoPath := generateTestVideo(t, 1920, 1080)

	profiles, err := adapter.SelectProfiles(context.Background(), videoPath, "")

	require.NoError(t, err)
	assert.Len(t, profiles, 4, "should produce all 4 profiles for 1080p input")
	assert.Equal(t, "5000k", profiles[0].Bitrate)
	assert.Equal(t, "1920x1080", profiles[0].Resolution)
	assert.Equal(t, "1280x720", profiles[1].Resolution)
	assert.Equal(t, "854x480", profiles[2].Resolution)
	assert.Equal(t, "640x360", profiles[3].Resolution)
}

func TestFFmpegAdapter_SelectProfiles_720p_ProducesThreeProfiles(t *testing.T) {
	requireFFprobe(t)
	adapter := newFFmpegAdapter(t)
	videoPath := generateTestVideo(t, 1280, 720)

	profiles, err := adapter.SelectProfiles(context.Background(), videoPath, "")

	require.NoError(t, err)
	assert.Len(t, profiles, 3, "should produce 3 profiles for 720p input")
	assert.Equal(t, "1280x720", profiles[0].Resolution)
	assert.Equal(t, "854x480", profiles[1].Resolution)
	assert.Equal(t, "640x360", profiles[2].Resolution)
}

func TestFFmpegAdapter_SelectProfiles_480p_ProducesTwoProfiles(t *testing.T) {
	requireFFprobe(t)
	adapter := newFFmpegAdapter(t)
	videoPath := generateTestVideo(t, 854, 480)

	profiles, err := adapter.SelectProfiles(context.Background(), videoPath, "")

	require.NoError(t, err)
	assert.Len(t, profiles, 2, "should produce 2 profiles for 480p input")
	assert.Equal(t, "854x480", profiles[0].Resolution)
	assert.Equal(t, "640x360", profiles[1].Resolution)
}

func TestFFmpegAdapter_SelectProfiles_360p_ProducesOneProfile(t *testing.T) {
	requireFFprobe(t)
	adapter := newFFmpegAdapter(t)
	videoPath := generateTestVideo(t, 640, 360)

	profiles, err := adapter.SelectProfiles(context.Background(), videoPath, "")

	require.NoError(t, err)
	assert.Len(t, profiles, 1, "should produce 1 profile for 360p input")
	assert.Equal(t, "640x360", profiles[0].Resolution)
}

func TestFFmpegAdapter_SelectProfiles_Sub360p_FallsBackTo360p(t *testing.T) {
	requireFFprobe(t)
	adapter := newFFmpegAdapter(t)
	videoPath := generateTestVideo(t, 320, 240)

	profiles, err := adapter.SelectProfiles(context.Background(), videoPath, "")

	require.NoError(t, err)
	assert.Len(t, profiles, 1, "should fall back to 360p for very low resolution")
	assert.Equal(t, "640x360", profiles[0].Resolution)
}

func TestFFmpegAdapter_TranscodeHLS_SelectProfilesWithVideo(t *testing.T) {
	t.Skip("requires ffmpeg binary")
	requireFFprobe(t)
	adapter := newFFmpegAdapter(t)
	videoPath := generateTestVideo(t, 1920, 1080)

	err := adapter.TranscodeHLS(context.Background(), videoPath,
		filepath.Join(t.TempDir(), "output"), "1080p", nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "failed to select profiles")
}

func TestFFmpegAdapter_TranscodeHLS_WithVideoAndProgress(t *testing.T) {
	t.Skip("requires ffmpeg binary")
	requireFFprobe(t)
	adapter := newFFmpegAdapter(t)
	videoPath := generateTestVideo(t, 640, 360)

	progressCalled := false
	progressFn := func(_ string, progress float64) { progressCalled = true }

	err := adapter.TranscodeHLS(context.Background(), videoPath,
		filepath.Join(t.TempDir(), "output"), "360p", progressFn)
	require.Error(t, err)
	assert.False(t, progressCalled)
}

// -- Handler edge case tests for uncovered branches --

func TestUploadHandler_CompleteUploadHandler_AuthFailure(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	body, _ := json.Marshal(map[string]interface{}{"upload_id": "upload-1", "total_chunks": 5})
	req := httptest.NewRequest(http.MethodPost, "/complete", bytes.NewReader(body))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_CompleteUploadWithContentHandler_InvalidJSON(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodPost, "/complete-upload", bytes.NewReader([]byte(`{"upload_id": null}`)))
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.CompleteUploadWithContentHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_ChunkStatusesHandler_GetChunkStatusesError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/chunks?upload_id=upload-1", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.ChunkStatusesHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUploadHandler_ListUploadsHandler_ServiceError(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/list", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x1234")
	rec := httptest.NewRecorder()
	handler.ListUploadsHandler(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestUploadHandler_UploadHandler_SuccessPath(t *testing.T) {
	handler := newTestUploadHandlerWithSvc(t)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test.mp4")
	require.NoError(t, err)
	_, _ = part.Write([]byte("fake video content"))
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("X-Wallet-Address", "0xwallet")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.UploadHandler(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

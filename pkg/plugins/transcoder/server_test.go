package transcoder

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
)

func newTestTranscoderServer(t *testing.T) *TranscoderServer {
	t.Helper()

	cfg := &config.Config{}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewTranscoderServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)
	return server
}

func TestTranscoderServer_StartRegistersRoutes(t *testing.T) {
	server := newTestTranscoderServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))
	t.Cleanup(func() {
		_ = server.Stop(context.Background())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/profiles", http.NoBody)
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"profiles":`)
}

func TestTranscoderServer_SubmitTaskFlow(t *testing.T) {
	server := newTestTranscoderServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))
	t.Cleanup(func() {
		_ = server.Stop(context.Background())
	})

	body, _ := json.Marshal(map[string]interface{}{
		"file_id":   "file-99",
		"file_path": "https://example.com/input.mp4",
		"profiles":  []map[string]string{{"resolution": "720p", "bitrate": "2500k", "format": "mp4"}},
		"priority":  2,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"task_id":`)
	assert.Contains(t, rec.Body.String(), `"status":"`)
}

func TestTranscoderServer_StatusAndCancelByPath(t *testing.T) {
	server := newTestTranscoderServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))
	t.Cleanup(func() {
		_ = server.Stop(context.Background())
	})

	body, _ := json.Marshal(map[string]interface{}{
		"file_id":   "file-100",
		"file_path": "https://example.com/input.mp4",
		"profiles":  []map[string]string{{"resolution": "720p", "bitrate": "2500k", "format": "mp4"}},
		"priority":  2,
	})
	submitReq := httptest.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewReader(body))
	submitRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(submitRec, submitReq)
	require.Equal(t, http.StatusAccepted, submitRec.Code)

	var submitResp map[string]interface{}
	require.NoError(t, json.Unmarshal(submitRec.Body.Bytes(), &submitResp))
	taskID, ok := submitResp["task_id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, taskID)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/status?task_id="+taskID, http.NoBody)
	statusRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(statusRec, statusReq)
	require.Equal(t, http.StatusOK, statusRec.Code)
	assert.Contains(t, statusRec.Body.String(), taskID)

	pathStatusReq := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/status/"+taskID, http.NoBody)
	pathStatusRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(pathStatusRec, pathStatusReq)
	assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, pathStatusRec.Code)

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/v1/transcode/cancel?task_id="+taskID, http.NoBody)
	cancelRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(cancelRec, cancelReq)
	assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest, http.StatusInternalServerError}, cancelRec.Code)
}

func TestTranscoderServer_ListTasksAlias(t *testing.T) {
	server := newTestTranscoderServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))
	t.Cleanup(func() {
		_ = server.Stop(context.Background())
	})

	body, _ := json.Marshal(map[string]interface{}{
		"file_id":   "file-200",
		"file_path": "https://example.com/input.mp4",
		"profiles":  []map[string]string{{"resolution": "720p", "bitrate": "2500k", "format": "mp4"}},
		"priority":  1,
	})
	submitReq := httptest.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewReader(body))
	submitRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(submitRec, submitReq)
	require.Equal(t, http.StatusAccepted, submitRec.Code)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/tasks?content_id=file-200", http.NoBody)
	listRec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(listRec, listReq)

	require.Equal(t, http.StatusOK, listRec.Code)
	assert.Contains(t, listRec.Body.String(), `"tasks":`)
	assert.Contains(t, listRec.Body.String(), `"FileID":"file-200"`)
}

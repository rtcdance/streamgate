package transcoder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/core/event"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSanitizeFilePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"absolute path", "/data/videos/input.mp4", "/data/videos/input.mp4"},
		{"relative path", "videos/input.mp4", ""},
		{"path traversal", "/data/../etc/passwd", "/etc/passwd"},
		{"empty", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizeFilePath(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGenerateTaskID(t *testing.T) {
	id1 := generateTaskID()
	id2 := generateTaskID()

	assert.NotEmpty(t, id1)
	assert.NotEqual(t, id1, id2)
	assert.Contains(t, id1, "task_")
}

func TestResolveTaskID(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		query    string
		expected string
	}{
		{"from query param", http.MethodGet, "/api/v1/transcode/status", "task_id=abc123", "abc123"},
		{"from path suffix", http.MethodGet, "/api/v1/transcode/status/abc123", "", "abc123"},
		{"from last path segment", http.MethodGet, "/abc123", "", "abc123"},
		{"empty", http.MethodGet, "/", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path+"?"+tc.query, http.NoBody)
			result := resolveTaskID(req, "/api/v1/transcode/status/")
			assert.Equal(t, tc.expected, result)
		})
	}
}

func newTestTranscoderHandler(t *testing.T) *TranscoderHandler {
	t.Helper()
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewTranscoderServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	err = server.plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	return NewTranscoderHandler(server.plugin, zap.NewNop(), kernel)
}

func TestTranscoderHandler_HealthHandler_Healthy(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.HealthHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTranscoderHandler_ReadyHandler(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ReadyHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTranscoderHandler_SubmitTaskHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/submit", http.NoBody)
	rec := httptest.NewRecorder()

	handler.SubmitTaskHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTranscoderHandler_SubmitTaskHandler_InvalidBody(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader([]byte("bad")))
	rec := httptest.NewRecorder()

	handler.SubmitTaskHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTranscoderHandler_SubmitTaskHandler_Success(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	body, _ := json.Marshal(submitTranscodeRequest{
		FileID:   "file-1",
		FilePath: "/data/input.mp4",
		Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}},
		Priority: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.SubmitTaskHandler(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestTranscoderHandler_SubmitTaskHandler_PathTraversal(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	body, _ := json.Marshal(submitTranscodeRequest{
		FileID:   "file-1",
		FilePath: "/data/../etc/passwd",
		Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.SubmitTaskHandler(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestTranscoderHandler_GetTaskStatusHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/status", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetTaskStatusHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTranscoderHandler_GetTaskStatusHandler_MissingID(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetTaskStatusHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTranscoderHandler_GetTaskStatusHandler_NotFound(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/status?task_id=nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetTaskStatusHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTranscoderHandler_CancelTaskHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/cancel", http.NoBody)
	rec := httptest.NewRecorder()

	handler.CancelTaskHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTranscoderHandler_CancelTaskHandler_MissingID(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.CancelTaskHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTranscoderHandler_ListTasksHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/list", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ListTasksHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTranscoderHandler_ListTasksHandler_Success(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/list", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ListTasksHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTranscoderHandler_GetMetricsHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetMetricsHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTranscoderHandler_GetMetricsHandler_Success(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetMetricsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTranscoderHandler_ListProfilesHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/profiles", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ListProfilesHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTranscoderHandler_ListProfilesHandler_Success(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/profiles", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ListProfilesHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp, "profiles")
}

func TestTranscoderHandler_NotFoundHandler(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.NotFoundHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTranscoderPluginWrapper_NameVersion(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	wrapper := NewTranscoderPluginWrapper(cfg, zap.NewNop())

	assert.Equal(t, "transcoder", wrapper.Name())
	assert.Equal(t, "1.0.0", wrapper.Version())
}

func TestTranscoderPluginWrapper_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	wrapper := NewTranscoderPluginWrapper(cfg, zap.NewNop())

	err := wrapper.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestTranscoderServer_Health_NotStarted(t *testing.T) {
	server := &TranscoderServer{logger: zap.NewNop()}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestTranscoderServer_Health_NoPlugin(t *testing.T) {
	server := &TranscoderServer{
		logger: zap.NewNop(),
		server: &http.Server{},
	}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestTranscoderPluginWrapper_DependsOn(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	wrapper := NewTranscoderPluginWrapper(cfg, zap.NewNop())

	deps := wrapper.DependsOn()
	assert.Nil(t, deps)
}

func TestTranscoderPluginWrapper_Init(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	wrapper := NewTranscoderPluginWrapper(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = wrapper.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, wrapper.server)
}

func TestTranscoderPluginWrapper_Stop_NoServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	wrapper := NewTranscoderPluginWrapper(cfg, zap.NewNop())

	err := wrapper.Stop(context.Background())
	require.NoError(t, err)
}

func TestTaskQueue_UpdateTask(t *testing.T) {
	tq := &TaskQueue{
		tasks:   make(map[string]*TranscodeTask),
		queue:   make(chan *TranscodeTask, 10),
		maxSize: 10,
		metrics: &QueueMetrics{},
	}

	err := tq.Enqueue(&TranscodeTask{ID: "task-1"})
	require.NoError(t, err)

	err = tq.UpdateTask(&TranscodeTask{ID: "task-1", Status: TaskStatusProcessing})
	require.NoError(t, err)

	task, err := tq.GetTask("task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskStatusProcessing, task.Status)
}

func TestTaskQueue_Len(t *testing.T) {
	tq := &TaskQueue{
		tasks:   make(map[string]*TranscodeTask),
		queue:   make(chan *TranscodeTask, 10),
		maxSize: 10,
		metrics: &QueueMetrics{},
	}

	assert.Equal(t, 0, tq.Len())

	_ = tq.Enqueue(&TranscodeTask{ID: "task-1"})
	assert.Equal(t, 1, tq.Len())
}

func TestTranscoderHandler_SubmitTaskHandler_EmptyFilePath(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	body, _ := json.Marshal(submitTranscodeRequest{
		FileID:   "file-1",
		FilePath: "relative/path.mp4",
		Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.SubmitTaskHandler(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestTranscoderHandler_CancelTaskHandler_Success(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	body, _ := json.Marshal(submitTranscodeRequest{
		FileID:   "file-cancel",
		FilePath: "/data/input.mp4",
		Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}},
	})
	submitReq := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	submitRec := httptest.NewRecorder()
	handler.SubmitTaskHandler(submitRec, submitReq)
	require.Equal(t, http.StatusAccepted, submitRec.Code)

	var submitResp map[string]interface{}
	require.NoError(t, json.Unmarshal(submitRec.Body.Bytes(), &submitResp))
	taskID, ok := submitResp["task_id"].(string)
	require.True(t, ok)

	cancelReq := httptest.NewRequest(http.MethodPost, "/cancel?task_id="+taskID, http.NoBody)
	cancelRec := httptest.NewRecorder()
	handler.CancelTaskHandler(cancelRec, cancelReq)

	assert.Equal(t, http.StatusOK, cancelRec.Code)
}

func TestTranscoderHandler_GetTaskStatusHandler_Success(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	body, _ := json.Marshal(submitTranscodeRequest{
		FileID:   "file-status",
		FilePath: "/data/input.mp4",
		Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}},
	})
	submitReq := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	submitRec := httptest.NewRecorder()
	handler.SubmitTaskHandler(submitRec, submitReq)
	require.Equal(t, http.StatusAccepted, submitRec.Code)

	var submitResp map[string]interface{}
	require.NoError(t, json.Unmarshal(submitRec.Body.Bytes(), &submitResp))
	taskID, ok := submitResp["task_id"].(string)
	require.True(t, ok)

	statusReq := httptest.NewRequest(http.MethodGet, "/status?task_id="+taskID, http.NoBody)
	statusRec := httptest.NewRecorder()
	handler.GetTaskStatusHandler(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
}

func TestTranscoderHandler_ListTasksHandler_WithContentFilter(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	body, _ := json.Marshal(submitTranscodeRequest{
		FileID:   "filter-me",
		FilePath: "/data/input.mp4",
		Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}},
	})
	submitReq := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	submitRec := httptest.NewRecorder()
	handler.SubmitTaskHandler(submitRec, submitReq)
	require.Equal(t, http.StatusAccepted, submitRec.Code)

	req := httptest.NewRequest(http.MethodGet, "/list?content_id=filter-me", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ListTasksHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	tasks, ok := resp["tasks"].([]interface{})
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(tasks), 1)
}

func TestTranscoderHandler_ListTasksHandler_WithPagination(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/list?limit=5&offset=0", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ListTasksHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTranscoderHandler_ListTasksHandler_OffsetBeyondRange(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	for i := 0; i < 3; i++ {
		body, _ := json.Marshal(submitTranscodeRequest{
			FileID:   fmt.Sprintf("file-%d", i),
			FilePath: "/data/input.mp4",
			Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}},
		})
		req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.SubmitTaskHandler(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/list?offset=100", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ListTasksHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	tasks, ok := resp["tasks"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, tasks)
}

func TestTranscoderHandler_SubmitTaskHandler_InvalidLimit(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/list?limit=abc&offset=xyz", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ListTasksHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTranscoderPluginWrapper_Init_AndStop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	wrapper := NewTranscoderPluginWrapper(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = wrapper.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, wrapper.server)
}

func TestTranscoderPlugin_HealthCheck_WithWorkers(t *testing.T) {
	bus, err := event.NewMemoryEventBus()
	require.NoError(t, err)

	queue := newTestTaskQueue(10)
	pool := &WorkerPool{
		taskQueue: queue,
		eventBus:  bus,
		logger:    zap.NewNop(),
		metrics:   &WorkerMetrics{},
		workers: []*Worker{
			{ID: "w1", Status: WorkerStatusIdle, LastHeartbeat: time.Now()},
		},
	}

	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize: 1,
		MaxQueueSize:   10,
		ScalingPolicy:  &ScalingPolicy{MinWorkers: 1, MaxWorkers: 1},
	})
	plugin.workerPool = pool

	err = plugin.HealthCheck()
	require.NoError(t, err)
}

func TestTranscoderPlugin_ScaleWorkers(t *testing.T) {
	bus, err := event.NewMemoryEventBus()
	require.NoError(t, err)

	queue := newTestTaskQueue(10)
	pool := &WorkerPool{
		taskQueue: queue,
		eventBus:  bus,
		logger:    zap.NewNop(),
		metrics:   &WorkerMetrics{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, pool.Start(ctx, 2))

	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize: 2,
		MaxQueueSize:   10,
		ScalingPolicy:  &ScalingPolicy{MinWorkers: 1, MaxWorkers: 5},
	})
	plugin.workerPool = pool
	plugin.logger = zap.NewNop()

	err = plugin.ScaleWorkers(3)
	require.NoError(t, err)
}

func TestTranscoderPlugin_GetMetrics(t *testing.T) {
	queue := newTestTaskQueue(10)
	pool := &WorkerPool{
		taskQueue: queue,
		logger:    zap.NewNop(),
		metrics:   &WorkerMetrics{},
		workers: []*Worker{
			{ID: "w1", Status: WorkerStatusIdle},
		},
	}

	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize: 1,
		MaxQueueSize:   10,
		ScalingPolicy:  &ScalingPolicy{MinWorkers: 1, MaxWorkers: 1},
	})
	plugin.workerPool = pool

	metrics := plugin.GetMetrics()
	assert.NotNil(t, metrics)
}

func TestTranscodeHLS_UnknownProfile(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	err := ft.TranscodeHLS(context.Background(), "/nonexistent.mp4", t.TempDir(), "unknown_profile", nil)
	require.Error(t, err)
}

func TestFFmpegTranscoder_ValidateMediaFile_NotFound(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	_, err := ft.ValidateMediaFile(context.Background(), "/nonexistent/file.mp4")
	require.Error(t, err)
}

func TestFFmpegTranscoder_ValidateMediaFile_HTTPURL(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	_, err := ft.ValidateMediaFile(context.Background(), "https://example.com/video.mp4")
	require.Error(t, err)
}

func TestFFmpegTranscoder_ValidateMediaFile_FileTooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("small content")
	err := os.WriteFile(filepath.Join(tmpDir, "big.mp4"), content, 0o644)
	require.NoError(t, err)

	cfg := &FFmpegConfig{
		TempDir:     t.TempDir(),
		MaxFileSize: 1,
	}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	_, err = ft.ValidateMediaFile(context.Background(), filepath.Join(tmpDir, "big.mp4"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestFFmpegTranscoder_Transcode_NotFound(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	err := ft.Transcode(context.Background(), "/nonexistent.mp4", "/tmp/out.mp4", TranscodeProfile{}, nil)
	require.Error(t, err)
}

func TestFFmpegTranscoder_ExtractThumbnail_NotFound(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	err := ft.ExtractThumbnail(context.Background(), "/nonexistent.mp4", "/tmp/thumb.jpg", "00:00:01")
	require.Error(t, err)
}

func TestFFmpegTranscoder_ExtractAudio_NotFound(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	err := ft.ExtractAudio(context.Background(), "/nonexistent.mp4", "/tmp/audio.aac")
	require.Error(t, err)
}

func TestFFmpegTranscoder_ConcatVideos_NotFound(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	err := ft.ConcatVideos(context.Background(), []string{"/nonexistent1.mp4", "/nonexistent2.mp4"}, "/tmp/out.mp4", nil)
	require.Error(t, err)
}

func TestFFmpegTranscoder_TranscodeToHLS_NotFound(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	err := ft.TranscodeToHLS(context.Background(), "/nonexistent.mp4", t.TempDir(), []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}}, nil, nil)
	require.Error(t, err)
}

func TestFFmpegTranscoder_TranscodeToDASH_NotFound(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	err := ft.TranscodeToDASH(context.Background(), "/nonexistent.mp4", t.TempDir(), []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "dash"}}, nil)
	require.Error(t, err)
}

func TestFFmpegTranscoder_CleanupPartialOutput(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "seg1.ts"), []byte("ts data"), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "playlist.m3u8"), []byte("m3u8 data"), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "keep.mp4"), []byte("mp4 data"), 0o644)

	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	ft.cleanupPartialOutput(tmpDir)

	_, err := os.Stat(filepath.Join(tmpDir, "keep.mp4"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, "seg1.ts"))
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(filepath.Join(tmpDir, "playlist.m3u8"))
	assert.True(t, os.IsNotExist(err))
}

func TestFFmpegTranscoder_GenerateHLSMasterPlaylist(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	profiles := []TranscodeProfile{
		{Resolution: "1920x1080", Bitrate: "5000k", Format: "hls"},
		{Resolution: "1280x720", Bitrate: "2500k", Format: "hls"},
	}

	err := ft.generateHLSMasterPlaylist(tmpDir, profiles)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(tmpDir, "master.m3u8"))
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "#EXTM3U")
	assert.Contains(t, content, "1920x1080")
	assert.Contains(t, content, "1280x720")
}

func TestMonitorProgress(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		matches bool
	}{
		{"no match", "some random output", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matches := progressRegex.FindStringSubmatch(tc.output)
			if tc.matches {
				assert.GreaterOrEqual(t, len(matches), 8)
			} else {
				assert.Empty(t, matches)
			}
		})
	}
}

func TestMonitorProgress_Callback(t *testing.T) {
	pr, pw, err := os.Pipe()
	require.NoError(t, err)

	_, err = pw.WriteString("frame= 100 fps= 25.0 q=28.0 size= 1024 time=00:00:04.00 bitrate=2048.0kbits/s speed=1.0x\n")
	require.NoError(t, err)
	pw.Close()

	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	var received *TranscodeProgress
	callback := func(p *TranscodeProgress) {
		received = p
	}

	done := make(chan struct{})
	go func() {
		ft.monitorProgress(pr, 10*time.Second, callback)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("monitorProgress timed out")
	}

	require.NotNil(t, received, "callback should have been called with progress data")
	assert.Equal(t, int64(100), received.Frame)
	assert.Equal(t, 25.0, received.FPS)
	assert.Equal(t, "2048.0kbits/s", received.CurrentBitrate)
	assert.Equal(t, "1.0", received.Speed)
	assert.InDelta(t, 40.0, received.Progress, 0.1)
}

func TestMonitorProgress_CarriageReturn(t *testing.T) {
	pr, pw, err := os.Pipe()
	require.NoError(t, err)

	go func() {
		pw.WriteString("frame=  50 fps= 25.0 q=28.0 size=  512 time=00:00:02.00 bitrate=2048.0kbits/s speed=1.0x\r")
		pw.WriteString("frame= 100 fps= 25.0 q=28.0 size= 1024 time=00:00:04.00 bitrate=2048.0kbits/s speed=1.0x\r")
		pw.WriteString("frame= 150 fps= 25.0 q=28.0 size= 1536 time=00:00:06.00 bitrate=2048.0kbits/s speed=1.0x\n")
		pw.Close()
	}()

	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	var calls []float64
	callback := func(p *TranscodeProgress) {
		calls = append(calls, p.Progress)
	}

	done := make(chan struct{})
	go func() {
		ft.monitorProgress(pr, 10*time.Second, callback)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("monitorProgress timed out")
	}

	assert.Equal(t, 3, len(calls), "expected 3 progress callbacks for \\r and \\n separated lines")
	assert.InDelta(t, 20.0, calls[0], 0.1)
	assert.InDelta(t, 40.0, calls[1], 0.1)
	assert.InDelta(t, 60.0, calls[2], 0.1)
}

func TestMonitorProgress_MixedSeparators(t *testing.T) {
	pr, pw, err := os.Pipe()
	require.NoError(t, err)

	go func() {
		pw.WriteString("frame=  50 fps= 25.0 q=28.0 size=  512 time=00:00:02.00 bitrate=2048.0kbits/s speed=1.0x\r\n")
		pw.WriteString("frame= 100 fps= 25.0 q=28.0 size= 1024 time=00:00:04.00 bitrate=2048.0kbits/s speed=1.0x\r")
		pw.WriteString("frame= 200 fps= 25.0 q=28.0 size= 2048 time=00:00:08.00 bitrate=2048.0kbits/s speed=1.0x\n")
		pw.Close()
	}()

	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	var calls []float64
	callback := func(p *TranscodeProgress) {
		calls = append(calls, p.Progress)
	}

	done := make(chan struct{})
	go func() {
		ft.monitorProgress(pr, 10*time.Second, callback)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("monitorProgress timed out")
	}

	assert.Equal(t, 3, len(calls), "expected 3 progress callbacks for mixed \\r\\n/\\r/\\n separators")
	assert.InDelta(t, 20.0, calls[0], 0.1)
	assert.InDelta(t, 40.0, calls[1], 0.1)
	assert.InDelta(t, 80.0, calls[2], 0.1)
}

func TestMonitorProgress_ZeroDuration(t *testing.T) {
	pr, pw, err := os.Pipe()
	require.NoError(t, err)

	_, err = pw.WriteString("frame= 100 fps= 25.0 q=28.0 size= 1024 time=00:00:04.00 bitrate=2048.0kbits/s speed=1.0x\n")
	require.NoError(t, err)
	pw.Close()

	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	var received *TranscodeProgress
	callback := func(p *TranscodeProgress) {
		received = p
	}

	done := make(chan struct{})
	go func() {
		ft.monitorProgress(pr, 0, callback)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("monitorProgress timed out")
	}

	if received != nil {
		assert.Equal(t, float64(0), received.Progress, "Progress should be 0 when totalDuration is 0")
		assert.Equal(t, time.Duration(4*time.Second), received.Processed)
	}
}

func TestFFmpegTranscoder_Transcode_CustomCodecs(t *testing.T) {
	cfg := &FFmpegConfig{
		TempDir:    t.TempDir(),
		VideoCodec: "libx265",
		AudioCodec: "libopus",
	}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	err := ft.Transcode(context.Background(), "/nonexistent.mp4", "/tmp/out.mp4", TranscodeProfile{}, nil)
	require.Error(t, err)
}

func TestFFmpegTranscoder_TranscodeHLS_WithProgress(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	err := ft.TranscodeHLS(context.Background(), "/nonexistent.mp4", t.TempDir(), "720p", func(_ string, f float64) {
	})
	require.Error(t, err)
}

func TestFFmpegTranscoder_ValidateMediaFile_MaxDuration(t *testing.T) {
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "video.mp4")
	err := os.WriteFile(videoPath, []byte("fake video"), 0o644)
	require.NoError(t, err)

	cfg := &FFmpegConfig{
		TempDir:     t.TempDir(),
		MaxDuration: 0.001,
	}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	_, err = ft.ValidateMediaFile(context.Background(), videoPath)
	require.Error(t, err)
}

func TestFFmpegTranscoder_ValidateMediaFile_HTTPURL_SkipsStat(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir(), MaxFileSize: 1}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	_, err := ft.ValidateMediaFile(context.Background(), "http://example.com/video.mp4")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "exceeds maximum")
}

func TestTranscoderPlugin_Init_WithKernel(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize:      2,
		MaxQueueSize:        10,
		TaskTimeout:         time.Second,
		HealthCheckInterval: time.Second,
		ScalingPolicy:       &ScalingPolicy{MinWorkers: 1, MaxWorkers: 3, CheckInterval: time.Second},
	})

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, plugin.taskQueue)
	assert.NotNil(t, plugin.workerPool)
	assert.NotNil(t, plugin.logger)
}

func TestTranscoderPlugin_SubmitTask_QueueFull(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize:      1,
		MaxQueueSize:        1,
		TaskTimeout:         time.Second,
		HealthCheckInterval: time.Second,
		ScalingPolicy:       &ScalingPolicy{MinWorkers: 1, MaxWorkers: 1},
	})

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	err = plugin.SubmitTask(&TranscodeTask{ID: "task-1", FileID: "f1", FilePath: "/data/v.mp4", Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}}})
	require.NoError(t, err)

	err = plugin.SubmitTask(&TranscodeTask{ID: "task-2", FileID: "f2", FilePath: "/data/v.mp4", Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "full")
}

func TestTranscoderPlugin_CancelTask_Success(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize:      1,
		MaxQueueSize:        10,
		TaskTimeout:         time.Second,
		HealthCheckInterval: time.Second,
		ScalingPolicy:       &ScalingPolicy{MinWorkers: 1, MaxWorkers: 1},
	})

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	err = plugin.SubmitTask(&TranscodeTask{ID: "task-1", FileID: "f1", FilePath: "/data/v.mp4", Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}}})
	require.NoError(t, err)

	err = plugin.CancelTask("task-1")
	require.NoError(t, err)
}

func TestTranscoderPlugin_CancelTask_NotFound(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize:      1,
		MaxQueueSize:        10,
		TaskTimeout:         time.Second,
		HealthCheckInterval: time.Second,
		ScalingPolicy:       &ScalingPolicy{MinWorkers: 1, MaxWorkers: 1},
	})

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	err = plugin.CancelTask("nonexistent")
	require.Error(t, err)
}

func TestTranscoderPlugin_GetTaskStatus_NotFound(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize:      1,
		MaxQueueSize:        10,
		TaskTimeout:         time.Second,
		HealthCheckInterval: time.Second,
		ScalingPolicy:       &ScalingPolicy{MinWorkers: 1, MaxWorkers: 1},
	})

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	_, err = plugin.GetTaskStatus("nonexistent")
	require.Error(t, err)
}

func TestTranscoderPlugin_PerformAutoScaling_NoActiveWorkers(t *testing.T) {
	bus, err := event.NewMemoryEventBus()
	require.NoError(t, err)

	queue := newTestTaskQueue(10)
	pool := &WorkerPool{
		taskQueue: queue,
		eventBus:  bus,
		logger:    zap.NewNop(),
		metrics:   &WorkerMetrics{},
		workers:   []*Worker{},
	}

	plugin := NewTranscoderPlugin(&TranscoderConfig{
		WorkerPoolSize: 1,
		MaxQueueSize:   10,
		ScalingPolicy:  &ScalingPolicy{MinWorkers: 1, MaxWorkers: 3, ScaleUpThreshold: 2.0, ScaleDownThreshold: 0.5, CheckInterval: time.Second},
	})
	plugin.workerPool = pool
	plugin.logger = zap.NewNop()
	plugin.taskQueue = queue

	plugin.performAutoScaling()
}

func TestTranscoderPluginWrapper_Stop_WithServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	wrapper := NewTranscoderPluginWrapper(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = wrapper.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, wrapper.server)
}

func TestTranscoderHandler_CancelTaskHandler_ProcessingTask(t *testing.T) {
	handler := newTestTranscoderHandler(t)

	body, _ := json.Marshal(submitTranscodeRequest{
		FileID:   "file-proc",
		FilePath: "/data/input.mp4",
		Profiles: []TranscodeProfile{{Resolution: "720p", Bitrate: "2500k", Format: "hls"}},
	})
	submitReq := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	submitRec := httptest.NewRecorder()
	handler.SubmitTaskHandler(submitRec, submitReq)
	require.Equal(t, http.StatusAccepted, submitRec.Code)

	var submitResp map[string]interface{}
	require.NoError(t, json.Unmarshal(submitRec.Body.Bytes(), &submitResp))
	taskID, ok := submitResp["task_id"].(string)
	require.True(t, ok)

	handler.plugin.taskQueue.mu.Lock()
	if task, exists := handler.plugin.taskQueue.tasks[taskID]; exists {
		task.Status = TaskStatusProcessing
	}
	handler.plugin.taskQueue.mu.Unlock()

	cancelReq := httptest.NewRequest(http.MethodPost, "/cancel?task_id="+taskID, http.NoBody)
	cancelRec := httptest.NewRecorder()
	handler.CancelTaskHandler(cancelRec, cancelReq)

	assert.Equal(t, http.StatusInternalServerError, cancelRec.Code)
}

func TestTranscoderHandler_ListTasksHandler_NilPlugin(t *testing.T) {
	handler := &TranscoderHandler{
		plugin:           nil,
		logger:           zap.NewNop(),
		kernel:           nil,
		metricsCollector: monitoring.NewMetricsCollector(zap.NewNop()),
	}

	req := httptest.NewRequest(http.MethodGet, "/list", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ListTasksHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestFFmpegTranscoder_CleanupPartialOutput_NonexistentDir(t *testing.T) {
	cfg := &FFmpegConfig{TempDir: t.TempDir()}
	ft := NewFFmpegTranscoder(cfg, zap.NewNop())

	ft.cleanupPartialOutput("/nonexistent/directory/for/test")
}

func TestParseTime_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{"negative hours", "-01:02:03.500", 0},
		{"negative minutes", "01:-02:03.500", 0},
		{"minutes over 59", "01:60:03.500", 0},
		{"negative seconds", "01:02:-03.500", 0},
		{"seconds over 60", "01:02:61.000", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseTime(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseBitrate_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"uppercase K", "5000K", 5000},
		{"uppercase M", "5M", 5000},
		{"zero", "0", 0},
		{"negative number", "-100", -100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseBitrate(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseProfileHeight(t *testing.T) {
	tests := []struct {
		name       string
		resolution string
		want       int
	}{
		{"1080p", "1920x1080", 1080},
		{"720p", "1280x720", 720},
		{"480p", "854x480", 480},
		{"360p", "640x360", 360},
		{"malformed no x", "1080", 0},
		{"malformed bad height", "1920xabc", 0},
		{"empty", "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseProfileHeight(tc.resolution)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSelectABRProfiles_FiltersAboveSource(t *testing.T) {
	profiles := selectABRProfiles(720)
	assert.Len(t, profiles, 3)
}

func TestSelectABRProfiles_AllProfiles(t *testing.T) {
	profiles := selectABRProfiles(1080)
	assert.Len(t, profiles, 4)
}

func TestSelectABRProfiles_ProbeFallback(t *testing.T) {
	profiles := selectABRProfiles(0)
	assert.Len(t, profiles, 4)
}

func TestSelectABRProfiles_SourceBelow360p(t *testing.T) {
	profiles := selectABRProfiles(240)
	assert.Len(t, profiles, 1)
	assert.Equal(t, "640x360", profiles[0].Resolution)
}

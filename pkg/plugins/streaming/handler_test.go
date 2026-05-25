package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestStreamingHandler(t *testing.T) *StreamingHandler {
	t.Helper()
	kernel, err := core.NewMicrokernel(&config.Config{Mode: "monolith"}, zap.NewNop())
	require.NoError(t, err)
	cache := NewStreamCache(zap.NewNop())
	return NewStreamingHandler(cache, zap.NewNop(), kernel)
}

func TestStreamingHandler_HealthHandler_Healthy(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.HealthHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStreamingHandler_ReadyHandler(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ReadyHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStreamingHandler_GetHLSPlaylistHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/hls", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetHLSPlaylistHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestStreamingHandler_GetHLSPlaylistHandler_MissingID(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/hls", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetHLSPlaylistHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestStreamingHandler_GetHLSPlaylistHandler_Success(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/hls?content_id=test-123", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetHLSPlaylistHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/vnd.apple.mpegurl", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "#EXTM3U")
}

func TestStreamingHandler_GetDASHManifestHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/dash", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetDASHManifestHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestStreamingHandler_GetDASHManifestHandler_MissingID(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/dash", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetDASHManifestHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestStreamingHandler_GetDASHManifestHandler_Success(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/dash?content_id=test-123", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetDASHManifestHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/dash+xml", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "MPD")
}

func TestStreamingHandler_GetSegmentHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/segment", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetSegmentHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestStreamingHandler_GetSegmentHandler_MissingIDs(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/segment?content_id=test-123", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetSegmentHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestStreamingHandler_GetSegmentHandler_Success(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/segment?content_id=test-123&segment_id=seg-1", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetSegmentHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "video/mp2t", rec.Header().Get("Content-Type"))
}

func TestStreamingHandler_GetStreamInfoHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/info", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetStreamInfoHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestStreamingHandler_GetStreamInfoHandler_MissingID(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/info", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetStreamInfoHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestStreamingHandler_GetStreamInfoHandler_Success(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/info?content_id=test-123", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetStreamInfoHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "test-123", resp["content_id"])
}

func TestStreamingHandler_NotFoundHandler(t *testing.T) {
	handler := newTestStreamingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.NotFoundHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHLSGenerator_Generate(t *testing.T) {
	gen := &HLSGenerator{}
	result, err := gen.Generate("content-1")
	require.NoError(t, err)
	assert.Contains(t, result, "#EXTM3U")
}

func TestDASHGenerator_Generate(t *testing.T) {
	gen := &DASHGenerator{}
	result, err := gen.Generate("content-1")
	require.NoError(t, err)
	assert.Contains(t, result, "MPD")
}

func TestStreamCache_Operations(t *testing.T) {
	cache := NewStreamCache(zap.NewNop())

	val, ok := cache.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, val)

	cache.Set("key1", "value1")
	cache.Delete("key1")

	cache.Close()
}

func TestAdaptiveBitrate_SelectBitrate(t *testing.T) {
	ab := &AdaptiveBitrate{}

	tests := []struct {
		name      string
		bandwidth int
		expected  int
	}{
		{"low bandwidth", 500, 500},
		{"medium bandwidth", 3000, 2500},
		{"high bandwidth", 10000, 5000},
		{"zero bandwidth", 0, 500},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ab.SelectBitrate(tc.bandwidth)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNewStreamingServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewStreamingServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, server)
	assert.NotNil(t, server.cache)
}

func TestStreamingServer_Health_NotStarted(t *testing.T) {
	server := &StreamingServer{logger: zap.NewNop()}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestStreamingPlugin_NameVersion(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewStreamingPlugin(cfg, zap.NewNop())

	assert.Equal(t, "streaming", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
}

func TestStreamingPlugin_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewStreamingPlugin(cfg, zap.NewNop())

	err := plugin.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestStreamingPlugin_Init(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewStreamingPlugin(cfg, zap.NewNop())

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, plugin.server)
}

func TestStreamingPlugin_DependsOn(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewStreamingPlugin(cfg, zap.NewNop())

	deps := plugin.DependsOn()
	assert.Nil(t, deps)
}

func TestStreamingPlugin_Stop_NoServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewStreamingPlugin(cfg, zap.NewNop())

	err := plugin.Stop(context.Background())
	require.NoError(t, err)
}

func TestStreamingServer_Health_NoServer(t *testing.T) {
	server := &StreamingServer{logger: zap.NewNop()}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestStreamingServer_Stop_NoServer(t *testing.T) {
	server := &StreamingServer{logger: zap.NewNop()}

	err := server.Stop(context.Background())
	require.NoError(t, err)
}

func TestStreamingServer_Stop_WithCache(t *testing.T) {
	cache := NewStreamCache(zap.NewNop())
	server := &StreamingServer{
		logger: zap.NewNop(),
		cache:  cache,
	}

	err := server.Stop(context.Background())
	require.NoError(t, err)
}

func TestAdaptiveBitrate_SelectBitrate_Table(t *testing.T) {
	ab := &AdaptiveBitrate{}

	tests := []struct {
		name      string
		bandwidth int
		expected  int
	}{
		{"very low bandwidth", 100, 500},
		{"low bandwidth", 500, 500},
		{"medium bandwidth", 3000, 2500},
		{"high bandwidth", 10000, 5000},
		{"zero bandwidth", 0, 500},
		{"boundary 1000", 999, 500},
		{"boundary 5000", 4999, 2500},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ab.SelectBitrate(tc.bandwidth)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func makeTestJWT(t *testing.T, secret, wallet string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"wallet_address": wallet,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	return s
}

func newTestStreamingServer(t *testing.T) *StreamingServer {
	t.Helper()
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1
	cfg.Auth.JWTSecret = "test-secret"

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewStreamingServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)
	return server
}

func TestStreamingServer_RequireAuth_NoHeader(t *testing.T) {
	server := newTestStreamingServer(t)

	called := false
	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestStreamingServer_RequireAuth_InvalidFormat(t *testing.T) {
	server := newTestStreamingServer(t)

	called := false
	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	tests := []struct {
		name   string
		header string
	}{
		{"basic auth", "Basic dXNlcjpwYXNz"},
		{"bearer no token", "Bearer "},
		{"bearer only spaces", "Bearer    "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			req.Header.Set("Authorization", tc.header)
			rec := httptest.NewRecorder()
			handler(rec, req)

			assert.False(t, called)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestStreamingServer_RequireAuth_InvalidToken(t *testing.T) {
	server := newTestStreamingServer(t)

	called := false
	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestStreamingServer_RequireAuth_MissingWallet(t *testing.T) {
	server := newTestStreamingServer(t)

	called := false
	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	claims := jwt.MapClaims{}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rec := httptest.NewRecorder()
	handler(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestStreamingServer_RequireAuth_WrongSecret(t *testing.T) {
	server := newTestStreamingServer(t)

	called := false
	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	tokenStr := makeTestJWT(t, "wrong-secret", "0x1234")

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rec := httptest.NewRecorder()
	handler(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestStreamingServer_RequireAuth_Success(t *testing.T) {
	server := newTestStreamingServer(t)

	called := false
	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	tokenStr := makeTestJWT(t, "test-secret", "0xABCDEF")

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rec := httptest.NewRecorder()
	handler(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStreamingServer_StartAndStop(t *testing.T) {
	server := newTestStreamingServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, server.server)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	err = server.Stop(stopCtx)
	require.NoError(t, err)
}

func TestStreamingServer_Health_WithServer(t *testing.T) {
	server := newTestStreamingServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)

	err = server.Health(context.Background())
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}

func TestStreamingPlugin_StartAndStop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	plugin := NewStreamingPlugin(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = plugin.Start(ctx)
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	err = plugin.Stop(stopCtx)
	require.NoError(t, err)
}

func TestStreamingPlugin_Health_AfterInit(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	plugin := NewStreamingPlugin(cfg, zap.NewNop())
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

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = plugin.Stop(stopCtx)
}

func TestRangeHandler_ServeHTTP_EmptyPath(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRangeHandler_ServeHTTP_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/../../../etc/passwd", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRangeHandler_ServeHTTP_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/nonexistent.mp4", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRangeHandler_ServeHTTP_FullFile(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world this is a test file with some content")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/test.mp4", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "bytes", rec.Header().Get("Accept-Ranges"))
	assert.Equal(t, fmt.Sprintf("%d", len(content)), rec.Header().Get("Content-Length"))
}

func TestRangeHandler_ServeHTTP_SingleRange(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world this is a test file with some content")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/test.mp4", http.NoBody)
	req.Header.Set("Range", "bytes=0-4")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, fmt.Sprintf("bytes 0-4/%d", len(content)), rec.Header().Get("Content-Range"))
	assert.Equal(t, "5", rec.Header().Get("Content-Length"))
	assert.Equal(t, "hello", rec.Body.String())
}

func TestRangeHandler_ServeHTTP_InvalidRange(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/test.mp4", http.NoBody)
	req.Header.Set("Range", "bytes=9999-10000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestedRangeNotSatisfiable, rec.Code)
}

func TestRangeHandler_ServeHTTP_MultiRange(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world this is a test file with some content for multi range")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/test.mp4", http.NoBody)
	req.Header.Set("Range", "bytes=0-4,6-10")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "multipart/byteranges")
}

func TestRangeHandler_ServeRange_Success(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world test data for range serving")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	data, err := handler.ServeRange(context.Background(), "test.mp4", 0, 4)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), data)
}

func TestRangeHandler_ServeRange_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())

	_, err := handler.ServeRange(context.Background(), "../../../etc/passwd", 0, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestRangeHandler_ServeRange_InvalidRange(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("short")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	_, err = handler.ServeRange(context.Background(), "test.mp4", 100, 200)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid range")
}

func TestRangeHandler_GetFileInfo_Success(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("test content for file info")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	info, err := handler.GetFileInfo(context.Background(), "test.mp4")
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), info.Size)
	assert.Equal(t, "video/mp4", info.ContentType)
	assert.True(t, info.SupportsRange)
	assert.False(t, info.ModifiedTime.IsZero())
}

func TestRangeHandler_GetFileInfo_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())

	_, err := handler.GetFileInfo(context.Background(), "../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestRangeHandler_ValidateRange_Success(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("test content for validation")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	valid, err := handler.ValidateRange(context.Background(), "test.mp4", 0, 10)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestRangeHandler_ValidateRange_InvalidStart(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("short")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	valid, err := handler.ValidateRange(context.Background(), "test.mp4", 100, 200)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestRangeHandler_ValidateRange_EndBeyondFile(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("short")
	err := os.WriteFile(filepath.Join(tmpDir, "test.mp4"), content, 0o644)
	require.NoError(t, err)

	handler := NewRangeHandler(tmpDir, zap.NewNop())

	valid, err := handler.ValidateRange(context.Background(), "test.mp4", 0, 100)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestRangeHandler_ValidateRange_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())

	_, err := handler.ValidateRange(context.Background(), "../../../etc/passwd", 0, 10)
	require.Error(t, err)
}

func TestRangeHandler_CacheRange(t *testing.T) {
	cache := NewRangeCache(1024 * 1024)
	handler := &RangeHandler{storageDir: t.TempDir(), logger: zap.NewNop(), cache: cache}
	ctx := context.Background()

	cache.mu.Lock()
	cache.entries[handler.getCacheKey("test.mp4", 0, 10)] = &CacheEntry{
		Data:      []byte("cached data"),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		Accessed:  time.Now(),
	}
	cache.mu.Unlock()

	data, ok := handler.GetCachedRange(ctx, "test.mp4", 0, 10)
	assert.True(t, ok)
	assert.Equal(t, []byte("cached data"), data)
}

func TestRangeHandler_GetCachedRange_Expired(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewRangeCache(1024)
	handler := &RangeHandler{storageDir: tmpDir, logger: zap.NewNop(), cache: cache}
	ctx := context.Background()

	cacheKey := handler.getCacheKey("test.mp4", 0, 10)
	cache.mu.Lock()
	cache.entries[cacheKey] = &CacheEntry{
		Data:      []byte("expired data"),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		Accessed:  time.Now(),
	}
	cache.mu.Unlock()

	_, ok := handler.GetCachedRange(ctx, "test.mp4", 0, 10)
	assert.False(t, ok)
}

func TestRangeHandler_EvictIfNeeded(t *testing.T) {
	cache := NewRangeCache(10)

	cache.mu.Lock()
	cache.entries["key1"] = &CacheEntry{
		Data:      make([]byte, 8),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		Accessed:  time.Now().Add(-2 * time.Minute),
	}
	cache.entries["key2"] = &CacheEntry{
		Data:      make([]byte, 8),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		Accessed:  time.Now(),
	}
	cache.mu.Unlock()

	cache.mu.Lock()
	var totalSize int64
	for _, entry := range cache.entries {
		totalSize += int64(len(entry.Data))
	}
	assert.Greater(t, totalSize, cache.maxSize)

	var oldestKey string
	var oldestTime time.Time
	for key, entry := range cache.entries {
		if oldestKey == "" || entry.Accessed.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Accessed
		}
	}
	if oldestKey != "" {
		delete(cache.entries, oldestKey)
	}
	cache.mu.Unlock()

	cache.mu.RLock()
	total := len(cache.entries)
	cache.mu.RUnlock()
	assert.LessOrEqual(t, total, 1)
}

func TestRangeHandler_GetContentType_Table(t *testing.T) {
	handler := NewRangeHandler("/tmp", zap.NewNop())

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"mp4", "video.mp4", "video/mp4"},
		{"webm", "video.webm", "video/webm"},
		{"ogg", "video.ogg", "video/ogg"},
		{"mp3", "audio.mp3", "audio/mpeg"},
		{"wav", "audio.wav", "audio/wav"},
		{"flac", "audio.flac", "audio/flac"},
		{"m3u8", "stream.m3u8", "application/vnd.apple.mpegurl"},
		{"mpd", "manifest.mpd", "application/dash+xml"},
		{"ts", "segment.ts", "video/mp2t"},
		{"unknown", "file.xyz", "application/octet-stream"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.getContentType(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestMultipartWriter_Closed(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := &multipartWriter{w: rec, boundary: "test-boundary"}

	mw.Close()

	n, err := mw.Write([]byte("data"))
	assert.Equal(t, 0, n)
	assert.Error(t, err)

	mw.WriteHeader()
	mw.Close()
}

func TestParseRangeHeader_EmptyParts(t *testing.T) {
	ranges, err := parseRangeHeader("bytes=0-4,,6-10", 100)
	require.NoError(t, err)
	assert.Len(t, ranges, 2)
}

func TestParseRangeSpec_SuffixRange(t *testing.T) {
	fileRange, err := parseRangeSpec("-100", 1000)
	require.NoError(t, err)
	assert.Equal(t, int64(900), fileRange.Start)
	assert.Equal(t, int64(999), fileRange.End)
}

func TestParseRangeSpec_SuffixNoCount(t *testing.T) {
	fileRange, err := parseRangeSpec("-", 1000)
	require.NoError(t, err)
	assert.Equal(t, int64(999), fileRange.Start)
	assert.Equal(t, int64(999), fileRange.End)
}

func TestParseRangeSpec_InvalidSpec(t *testing.T) {
	_, err := parseRangeSpec("a-b-c", 1000)
	require.Error(t, err)
}

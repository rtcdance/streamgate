package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"streamgate/pkg/service"
)

type mockSegmentStorage struct {
	objects     map[string]string
	listResult  []string
	listErr     error
	downloadErr error
}

func newMockSegmentStorage() *mockSegmentStorage {
	return &mockSegmentStorage{objects: make(map[string]string)}
}

func (m *mockSegmentStorage) Upload(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

func (m *mockSegmentStorage) UploadStream(_ context.Context, _, _ string, _ io.Reader, _ int64) error {
	return nil
}

func (m *mockSegmentStorage) UploadWithContentType(_ context.Context, _, _ string, _ []byte, _ string) error {
	return nil
}

func (m *mockSegmentStorage) Download(_ context.Context, _, objectName string) ([]byte, error) {
	if data, ok := m.objects[objectName]; ok {
		return []byte(data), nil
	}
	return nil, fmt.Errorf("object not found: %s", objectName)
}

func (m *mockSegmentStorage) DownloadStream(_ context.Context, _, objectName string) (io.ReadCloser, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	if data, ok := m.objects[objectName]; ok {
		return io.NopCloser(strings.NewReader(data)), nil
	}
	return nil, fmt.Errorf("object not found: %s", objectName)
}

func (m *mockSegmentStorage) ListObjects(_ context.Context, _, _ string) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listResult, nil
}

func (m *mockSegmentStorage) Exists(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (m *mockSegmentStorage) UploadStreamWithContentType(_ context.Context, _, _ string, _ io.Reader, _ int64, _ string) error {
	return nil
}

func (m *mockSegmentStorage) Delete(_ context.Context, _, _ string) error {
	return nil
}

func newStreamingTestAuthService() *service.AuthService {
	sigVerifier := service.NewMultiChainSignatureVerifier(zap.NewNop(), nil)
	return service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-streaming-tests",
		newMockAuthStorage(),
		sigVerifier,
		service.NewMemoryChallengeStore(),
		5*time.Minute,
		service.NewMemoryTokenBlacklist(),
	)
}

func setupStreamingManifestRouter(authService *service.AuthService, storage service.SegmentStorage) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xTestWallet1234567890abcdef1234567890abcdef12")
		c.Set("nft_contract", "0xNFTContract")
		c.Set("nft_chain_id", int64(1))
		c.Next()
	})
	RegisterStreamingRoutes(r, zap.NewNop(), authService, storage)
	return r
}

func setupStreamingSegmentRouter(authService *service.AuthService, storage service.SegmentStorage) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterStreamingSegmentRoute(r, zap.NewNop(), authService, storage)
	return r
}

func TestRegisterStreamingRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authService := newStreamingTestAuthService()
	RegisterStreamingRoutes(r, zap.NewNop(), authService, nil)
	RegisterStreamingSegmentRoute(r, zap.NewNop(), authService, nil)

	routes := r.Routes()
	routeMap := make(map[string]string)
	for _, route := range routes {
		routeMap[route.Path] = route.Method
	}

	assert.Equal(t, "GET", routeMap["/api/v1/streaming/:id/manifest.m3u8"],
		"manifest route should be registered as GET")
	assert.Equal(t, "GET", routeMap["/api/v1/streaming/:id/segment/:num"],
		"segment route should be registered as GET")
}

func TestHandleGetManifest(t *testing.T) {
	authService := newStreamingTestAuthService()
	manifestCacheMu.Lock()
	manifestCache = make(map[string]manifestCacheEntry)
	manifestCacheMu.Unlock()

	t.Run("returns HLS manifest with playback tokens", func(t *testing.T) {
		storage := newMockSegmentStorage()
		storage.listResult = []string{
			"streams/content-1/720p/seg001.ts",
			"streams/content-1/720p/seg002.ts",
		}
		r := setupStreamingManifestRouter(authService, storage)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/manifest.m3u8?token_id=1", http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/vnd.apple.mpegurl", w.Header().Get("Content-Type"))

		body := w.Body.String()
		assert.Contains(t, body, "#EXTM3U")
		assert.Contains(t, body, "#EXT-X-VERSION:3")
		assert.Contains(t, body, "#EXT-X-TARGETDURATION:10")
		assert.Contains(t, body, "seg001.ts")
		assert.Contains(t, body, "seg002.ts")
		assert.Contains(t, body, "playback_token=")
		assert.Contains(t, body, "#EXT-X-ENDLIST")
	})

	t.Run("returns 404 when no segments available", func(t *testing.T) {
		manifestCacheMu.Lock()
		delete(manifestCache, "content-1")
		manifestCacheMu.Unlock()
		storage := newMockSegmentStorage()
		storage.listResult = []string{}
		r := setupStreamingManifestRouter(authService, storage)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/manifest.m3u8?token_id=1", http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		var resp APIError
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, ErrContentNotFound, resp.Code)
	})

	t.Run("returns 404 when nil storage", func(t *testing.T) {
		manifestCacheMu.Lock()
		delete(manifestCache, "content-1")
		manifestCacheMu.Unlock()
		r := setupStreamingManifestRouter(authService, nil)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/manifest.m3u8?token_id=1", http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("filters only 720p ts segments", func(t *testing.T) {
		manifestCacheMu.Lock()
		delete(manifestCache, "content-1")
		manifestCacheMu.Unlock()
		storage := newMockSegmentStorage()
		storage.listResult = []string{
			"streams/content-1/720p/seg001.ts",
			"streams/content-1/1080p/seg001.ts",
			"streams/content-1/720p/metadata.json",
		}
		r := setupStreamingManifestRouter(authService, storage)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/manifest.m3u8?token_id=1", http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "seg001.ts")
		assert.NotContains(t, body, "metadata.json")
		assert.Contains(t, body, "720p")
		assert.Contains(t, body, "1080p")
	})
}

func TestHandleGetSegment(t *testing.T) {
	authService := newStreamingTestAuthService()

	t.Run("returns segment data with valid token", func(t *testing.T) {
		storage := newMockSegmentStorage()
		storage.objects["streams/content-1/720p/seg001.ts"] = "fake-ts-data"
		r := setupStreamingSegmentRouter(authService, storage)

		token, err := authService.GeneratePlaybackToken(
			"0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute,
		)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet,
			"/api/v1/streaming/content-1/segment/seg001.ts?playback_token="+token, http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "video/mp2t", w.Header().Get("Content-Type"))
		assert.Equal(t, "private, max-age=3600", w.Header().Get("Cache-Control"))
		assert.Equal(t, "fake-ts-data", w.Body.String())
	})

	t.Run("falls back to flat key pattern", func(t *testing.T) {
		storage := newMockSegmentStorage()
		storage.objects["content-1/seg001.ts"] = "flat-ts-data"
		r := setupStreamingSegmentRouter(authService, storage)

		token, err := authService.GeneratePlaybackToken(
			"0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute,
		)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet,
			"/api/v1/streaming/content-1/segment/seg001.ts?playback_token="+token, http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "flat-ts-data", w.Body.String())
	})

	t.Run("returns 503 when segment download fails", func(t *testing.T) {
		storage := newMockSegmentStorage()
		storage.downloadErr = fmt.Errorf("storage unavailable")
		r := setupStreamingSegmentRouter(authService, storage)

		token, err := authService.GeneratePlaybackToken(
			"0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute,
		)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet,
			"/api/v1/streaming/content-1/segment/seg001.ts?playback_token="+token, http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		var resp APIError
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, ErrContentUnavailable, resp.Code)
	})

	t.Run("returns 404 when storage is nil", func(t *testing.T) {
		r := setupStreamingSegmentRouter(authService, nil)

		token, err := authService.GeneratePlaybackToken(
			"0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute,
		)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet,
			"/api/v1/streaming/content-1/segment/seg001.ts?playback_token="+token, http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		var resp APIError
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, ErrNotFound, resp.Code)
	})
}

func TestHandleGetSegment_PathTraversal(t *testing.T) {
	authService := newStreamingTestAuthService()
	storage := newMockSegmentStorage()
	r := setupStreamingSegmentRouter(authService, storage)

	token, err := authService.GeneratePlaybackToken(
		"0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute,
	)
	require.NoError(t, err)

	tests := []struct {
		name    string
		segName string
	}{
		{"double dot", "..secret.ts"},
		{"backslash", "dir\\secret.ts"},
		{"non-ts extension", "seg001.mp4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			target := fmt.Sprintf("/api/v1/streaming/content-1/segment/%s?playback_token=%s", tt.segName, token)
			req, _ := http.NewRequest(http.MethodGet, target, http.NoBody)
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			var resp APIError
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			assert.Equal(t, ErrInvalidRequest, resp.Code)
		})
	}
}

func TestHandleGetSegment_MissingToken(t *testing.T) {
	authService := newStreamingTestAuthService()
	storage := newMockSegmentStorage()
	r := setupStreamingSegmentRouter(authService, storage)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/segment/seg001.ts", http.NoBody)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, ErrUnauthorized, resp.Code)
	assert.Equal(t, "missing playback token", resp.Error)
}

func TestHandleGetSegment_AuthHeader(t *testing.T) {
	authService := newStreamingTestAuthService()
	storage := newMockSegmentStorage()
	r := setupStreamingSegmentRouter(authService, storage)

	token, err := authService.GeneratePlaybackToken(
		"0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute,
	)
	require.NoError(t, err)

	t.Run("valid token in Authorization header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/segment/seg001.ts", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)

		assert.NotEqual(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Authorization header takes priority over query param", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/segment/seg001.ts?playback_token=invalid", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)

		assert.NotEqual(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid token in Authorization header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/segment/seg001.ts", http.NoBody)
		req.Header.Set("Authorization", "Bearer invalid-token")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var resp APIError
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "invalid playback token", resp.Error)
	})
}

func TestHandleGetSegment_InvalidToken(t *testing.T) {
	authService := newStreamingTestAuthService()
	storage := newMockSegmentStorage()
	r := setupStreamingSegmentRouter(authService, storage)

	tests := []struct {
		name  string
		token string
	}{
		{"garbage string", "not-a-jwt"},
		{"malformed JWT", "eyJhbGciOiJIUzI1NiJ9.invalid.payload"},
		{"wrong signing key", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJjb250ZW50LTEifQ.wrong-signature"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet,
				"/api/v1/streaming/content-1/segment/seg001.ts?playback_token="+tt.token, http.NoBody)
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			var resp APIError
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			assert.Equal(t, ErrUnauthorized, resp.Code)
			assert.Equal(t, "invalid playback token", resp.Error)
		})
	}
}

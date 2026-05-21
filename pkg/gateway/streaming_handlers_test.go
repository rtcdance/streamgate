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

	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
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
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)
}

func setupStreamingManifestRouter(authService *service.AuthService, storage service.SegmentStorage, cache *StreamingCache) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xTestWallet1234567890abcdef1234567890abcdef12")
		c.Set("nft_contract", "0xNFTContract")
		c.Set("nft_chain_id", int64(1))
		c.Next()
	})
	RegisterStreamingRoutes(r, zap.NewNop(), authService, service.NewStreamingService(nil, nil, nil, "", nil), storage, newStreamLimiter(100), cache)
	return r
}

func setupStreamingSegmentRouter(authService *service.AuthService, storage service.SegmentStorage, cache *StreamingCache) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterStreamingSegmentRoute(r, zap.NewNop(), authService, storage, nil, cache)
	return r
}

func TestRegisterStreamingRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authService := newStreamingTestAuthService()
	RegisterStreamingRoutes(r, zap.NewNop(), authService, service.NewStreamingService(nil, nil, nil, "", nil), nil, newStreamLimiter(100), NewStreamingCache())
	RegisterStreamingSegmentRoute(r, zap.NewNop(), authService, nil, nil, NewStreamingCache())

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
	cache := NewStreamingCache()

	t.Run("returns HLS manifest with playback tokens", func(t *testing.T) {
		storage := newMockSegmentStorage()
		storage.listResult = []string{
			"streams/content-1/720p/seg001.ts",
			"streams/content-1/720p/seg002.ts",
		}
		r := setupStreamingManifestRouter(authService, storage, cache)

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
		cache.Invalidate("content-1")
		storage := newMockSegmentStorage()
		storage.listResult = []string{}
		r := setupStreamingManifestRouter(authService, storage, cache)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/manifest.m3u8?token_id=1", http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		var resp APIError
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, ErrContentNotFound, resp.Code)
	})

	t.Run("returns 404 when nil storage", func(t *testing.T) {
		cache.Invalidate("content-1")
		r := setupStreamingManifestRouter(authService, nil, cache)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/manifest.m3u8?token_id=1", http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("filters only 720p ts segments", func(t *testing.T) {
		cache.Invalidate("content-1")
		storage := newMockSegmentStorage()
		storage.listResult = []string{
			"streams/content-1/720p/seg001.ts",
			"streams/content-1/1080p/seg001.ts",
			"streams/content-1/720p/metadata.json",
		}
		r := setupStreamingManifestRouter(authService, storage, cache)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-1/manifest.m3u8?token_id=1", http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "#EXT-X-STREAM-INF")
		assert.NotContains(t, body, "metadata.json")
		assert.Contains(t, body, "quality=720p")
		assert.Contains(t, body, "quality=1080p")
	})
}

func TestHandleGetSegment(t *testing.T) {
	authService := newStreamingTestAuthService()
	cache := NewStreamingCache()

	t.Run("returns segment data with valid token", func(t *testing.T) {
		storage := newMockSegmentStorage()
		storage.objects["streams/content-1/720p/seg001.ts"] = "fake-ts-data"
		r := setupStreamingSegmentRouter(authService, storage, cache)

		token, err := authService.GeneratePlaybackToken(
			context.Background(), "0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute, "",
		)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet,
			"/api/v1/streaming/content-1/segment/seg001.ts?playback_token="+token, http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "video/mp2t", w.Header().Get("Content-Type"))
		assert.Equal(t, "private, max-age=86400", w.Header().Get("Cache-Control"))
		assert.Equal(t, "fake-ts-data", w.Body.String())
	})

	t.Run("falls back to flat key pattern", func(t *testing.T) {
		storage := newMockSegmentStorage()
		storage.objects["content-1/seg001.ts"] = "flat-ts-data"
		r := setupStreamingSegmentRouter(authService, storage, cache)

		token, err := authService.GeneratePlaybackToken(
			context.Background(), "0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute, "",
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
		r := setupStreamingSegmentRouter(authService, storage, cache)

		token, err := authService.GeneratePlaybackToken(
			context.Background(), "0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute, "",
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
		r := setupStreamingSegmentRouter(authService, nil, cache)

		token, err := authService.GeneratePlaybackToken(
			context.Background(), "0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute, "",
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
	r := setupStreamingSegmentRouter(authService, storage, NewStreamingCache())

	token, err := authService.GeneratePlaybackToken(
		context.Background(), "0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute, "",
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
	r := setupStreamingSegmentRouter(authService, storage, NewStreamingCache())

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
	r := setupStreamingSegmentRouter(authService, storage, NewStreamingCache())

	token, err := authService.GeneratePlaybackToken(
		context.Background(), "0xTestWallet", "content-1", "0xNFTContract", "1", 1, 2*time.Minute, "",
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
	r := setupStreamingSegmentRouter(authService, storage, NewStreamingCache())

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

func TestStreamingE2E_AuthToSegment(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authService := newStreamingTestAuthService()
	storage := newMockSegmentStorage()
	storage.objects["streams/content-e2e/720p/seg001.ts"] = "e2e-ts-data"
	storage.listResult = []string{
		"streams/content-e2e/720p/seg001.ts",
	}
	cache := NewStreamingCache()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xE2EWallet1234567890abcdef1234567890abcdef12")
		c.Set("nft_contract", "0xE2ENFTContract")
		c.Set("nft_chain_id", int64(1))
		c.Next()
	})
	RegisterStreamingRoutes(r, zap.NewNop(), authService, service.NewStreamingService(nil, nil, nil, "", nil), storage, newStreamLimiter(100), cache)
	RegisterStreamingSegmentRoute(r, zap.NewNop(), authService, storage, nil, cache)

	t.Run("full flow: manifest → segment with playback token", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/content-e2e/manifest.m3u8?token_id=1", http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/vnd.apple.mpegurl", w.Header().Get("Content-Type"))

		body := w.Body.String()
		assert.Contains(t, body, "#EXTM3U")
		assert.Contains(t, body, "playback_token=")

		var playbackToken string
		for _, line := range strings.Split(body, "\n") {
			if strings.Contains(line, "playback_token=") {
				idx := strings.Index(line, "playback_token=")
				tokenPart := line[idx+len("playback_token="):]
				if end := strings.IndexAny(tokenPart, "&\r"); end != -1 {
					playbackToken = tokenPart[:end]
				} else {
					playbackToken = strings.TrimSpace(tokenPart)
				}
				break
			}
		}
		require.NotEmpty(t, playbackToken, "playback token should be present in manifest")

		w2 := httptest.NewRecorder()
		segReq, _ := http.NewRequest(http.MethodGet,
			"/api/v1/streaming/content-e2e/segment/seg001.ts?playback_token="+playbackToken, http.NoBody)
		r.ServeHTTP(w2, segReq)

		assert.Equal(t, http.StatusOK, w2.Code)
		assert.Equal(t, "video/mp2t", w2.Header().Get("Content-Type"))
		assert.Equal(t, "private, max-age=86400", w2.Header().Get("Cache-Control"))
		assert.Equal(t, "Authorization", w2.Header().Get("Vary"))
		assert.Equal(t, "e2e-ts-data", w2.Body.String())
	})

	t.Run("segment with wrong content token rejected", func(t *testing.T) {
		token, err := authService.GeneratePlaybackToken(
			context.Background(), "0xE2EWallet", "other-content", "0xE2ENFTContract", "1", 1, 2*time.Minute, "",
		)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet,
			"/api/v1/streaming/content-e2e/segment/seg001.ts?playback_token="+token, http.NoBody)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTranscodeHandlers_Submit_Validation(t *testing.T) {
	svc := newTestTranscodingService()

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			"missing content_id",
			`{"profile":"720p","input_url":"https://example.com/video.mp4"}`,
			http.StatusBadRequest,
			ErrInvalidRequest,
		},
		{
			"missing profile",
			`{"content_id":"c1","input_url":"https://example.com/video.mp4"}`,
			http.StatusBadRequest,
			ErrInvalidRequest,
		},
		{
			"missing input_url",
			`{"content_id":"c1","profile":"720p"}`,
			http.StatusBadRequest,
			ErrInvalidRequest,
		},
		{
			"invalid json",
			`{invalid}`,
			http.StatusBadRequest,
			ErrInvalidRequest,
		},
		{
			"priority too low",
			`{"content_id":"c1","profile":"720p","input_url":"https://example.com/video.mp4","priority":-1}`,
			http.StatusBadRequest,
			ErrInvalidRequest,
		},
		{
			"priority too high",
			`{"content_id":"c1","profile":"720p","input_url":"https://example.com/video.mp4","priority":11}`,
			http.StatusBadRequest,
			ErrInvalidRequest,
		},
		{
			"invalid input_url scheme",
			`{"content_id":"c1","profile":"720p","input_url":"ftp://example.com/video.mp4"}`,
			http.StatusBadRequest,
			ErrInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupTranscodeRouterWithService("0xOwner1234567890abcdef1234567890abcdef12", svc)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, tt.wantStatus, w.Code)
			var resp map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCode, resp["code"])
		})
	}
}

func TestTranscodeHandlers_Submit_Success(t *testing.T) {
	svc := newTestTranscodingService()
	// The input URL must name an allowed host: the API fetches the input itself,
	// so by default only the configured object store is acceptable.
	r := setupTranscodeRouterWithPolicy("0xOwner1234567890abcdef1234567890abcdef12", svc,
		TranscodeInputPolicy{AllowedHosts: []string{"example.com"}})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewBufferString(`{"content_id":"c1","profile":"720p","input_url":"https://example.com/video.mp4","priority":5}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp["task_id"])
	assert.Equal(t, "pending", resp["status"])
}

// The transcode API fetches the input itself, so by default it must refuse a URL
// of the caller's choosing — including a public one.
func TestTranscodeHandlers_Submit_RefusesExternalInputByDefault(t *testing.T) {
	svc := newTestTranscodingService()
	r := setupTranscodeRouterWithService("0xOwner1234567890abcdef1234567890abcdef12", svc)

	for _, inputURL := range []string{
		"https://example.com/video.mp4",
		"http://127.0.0.1:9000/streamgate/in.mp4",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5/in.mp4",
		"file:///etc/passwd",
	} {
		t.Run(inputURL, func(t *testing.T) {
			body := fmt.Sprintf(`{"content_id":"c1","profile":"720p","input_url":%q}`, inputURL)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			var resp map[string]interface{}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, ErrInvalidRequest, resp["code"])
			// The refusal must not become a probe for which internal names exist.
			assert.NotContains(t, w.Body.String(), "10.0.0.5")
			assert.NotContains(t, w.Body.String(), "169.254")
		})
	}
}

// The allowlist admits the configured object store and nothing else.
func TestTranscodeHandlers_Submit_AllowlistAdmitsOnlyTheObjectStore(t *testing.T) {
	svc := newTestTranscodingService()
	r := setupTranscodeRouterWithPolicy("0xOwner1234567890abcdef1234567890abcdef12", svc,
		TranscodeInputPolicy{AllowedHosts: []string{"minio:9000"}})

	post := func(inputURL string) int {
		body := fmt.Sprintf(`{"content_id":"c1","profile":"720p","input_url":%q}`, inputURL)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusAccepted, post("http://minio:9000/streamgate/in.mp4?sig=abc"),
		"the configured object store must be reachable")
	assert.Equal(t, http.StatusBadRequest, post("http://other:9000/streamgate/in.mp4"),
		"a different host must not be admitted by the same policy")
}

// Turning external input on admits public URLs but still refuses internal ones.
func TestTranscodeHandlers_Submit_ExternalOptInStillRefusesInternal(t *testing.T) {
	svc := newTestTranscodingService()
	r := setupTranscodeRouterWithPolicy("0xOwner1234567890abcdef1234567890abcdef12", svc,
		TranscodeInputPolicy{AllowExternal: true})

	post := func(inputURL string) int {
		body := fmt.Sprintf(`{"content_id":"c1","profile":"720p","input_url":%q}`, inputURL)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusBadRequest, post("http://127.0.0.1:9000/in.mp4"))
	assert.Equal(t, http.StatusBadRequest, post("http://169.254.169.254/latest/meta-data/"))
	assert.Equal(t, http.StatusBadRequest, post("http://10.0.0.5/in.mp4"))
}

func TestTranscodeHandlers_Submit_NilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xAny")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewBufferString(`{"content_id":"c1","profile":"720p","input_url":"https://example.com/video.mp4"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTranscodeHandlers_Cancel_NilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xAny")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/cancel/some-id", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTranscodeHandlers_Profiles_NilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xAny")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/profiles", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTranscodeHandlers_Status_NoWallet(t *testing.T) {
	svc := newTestTranscodingService()
	taskID, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/video.mp4", 0, "0xOwner1234567890abcdef1234567890abcdef12")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterTranscodingRoutes(r, zap.NewNop(), svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/status/"+taskID, http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTranscodeHandlers_Cancel_NoWallet(t *testing.T) {
	svc := newTestTranscodingService()
	taskID, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/video.mp4", 0, "0xOwner1234567890abcdef1234567890abcdef12")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterTranscodingRoutes(r, zap.NewNop(), svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/cancel/"+taskID, http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTranscodeHandlers_Status_NotFound(t *testing.T) {
	svc := newTestTranscodingService()
	r := setupTranscodeRouterWithService("0xOwner1234567890abcdef1234567890abcdef12", svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/status/nonexistent", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTranscodeHandlers_Cancel_NotFound(t *testing.T) {
	svc := newTestTranscodingService()
	r := setupTranscodeRouterWithService("0xOwner1234567890abcdef1234567890abcdef12", svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/cancel/nonexistent", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTranscodeHandlers_Tasks_Pagination(t *testing.T) {
	svc := newTestTranscodingService()
	r := setupTranscodeRouterWithService("0xOwner1234567890abcdef1234567890abcdef12", svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/tasks?limit=10&offset=0", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp, "tasks")
}

func TestTranscodeHandlers_Tasks_InvalidPagination(t *testing.T) {
	svc := newTestTranscodingService()
	r := setupTranscodeRouterWithService("0xOwner1234567890abcdef1234567890abcdef12", svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/tasks?limit=-1&offset=-1", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTranscodeHandlers_Profiles_Success(t *testing.T) {
	svc := newTestTranscodingService()
	r := setupTranscodeRouterWithService("0xOwner1234567890abcdef1234567890abcdef12", svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/profiles", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp, "profiles")
}

func TestTranscodeHandlers_Cancel_Success(t *testing.T) {
	ownerWallet := "0xOwner1234567890abcdef1234567890abcdef12"
	svc := newTestTranscodingService()
	taskID, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/video.mp4", 0, ownerWallet)
	require.NoError(t, err)

	r := setupTranscodeRouterWithService(ownerWallet, svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/cancel/"+taskID, http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", resp["status"])
}

// The plan's completion criterion for the fetch guard is that the target
// "actually receives 0 requests" — not merely that the response was a 4xx. A
// real listener is started and asserted to have been contacted zero times, with
// external input both off and on, so a future refactor that validates the URL
// but fetches it anyway cannot pass.
func TestTranscodeHandlers_Submit_SSRFTargetReceivesNoRequests(t *testing.T) {
	var hits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	for _, allowExternal := range []bool{false, true} {
		name := "external off"
		if allowExternal {
			name = "external on"
		}
		t.Run(name, func(t *testing.T) {
			svc := newTestTranscodingService()
			r := setupTranscodeRouterWithPolicy("0xOwner1234567890abcdef1234567890abcdef12", svc,
				TranscodeInputPolicy{AllowExternal: allowExternal})

			body := fmt.Sprintf(`{"content_id":"c1","profile":"720p","input_url":%q}`, target.URL+"/video.mp4")
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, int64(0), hits.Load(),
				"the target must receive no request at all, not merely be refused afterwards")
		})
	}
}

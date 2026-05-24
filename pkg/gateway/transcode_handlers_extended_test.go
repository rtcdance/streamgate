package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	r := setupTranscodeRouterWithService("0xOwner1234567890abcdef1234567890abcdef12", svc)

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

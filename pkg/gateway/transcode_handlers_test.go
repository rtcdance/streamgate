package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/service"
)

func newTestTranscodingService() *service.TranscodingService {
	return service.NewTranscodingService(nil, nil)
}

func setupTranscodeRouterWithService(walletAddr string, svc *service.TranscodingService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", walletAddr)
		c.Next()
	})

	RegisterTranscodingRoutes(r, zap.NewNop(), svc)
	return r
}

func TestTranscodeHandlers_SubmitAndStatus(t *testing.T) {
	ownerWallet := "0xOwner1234567890abcdef1234567890abcdef12"
	svc := newTestTranscodingService()

	// Submit a task
	taskID, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/video.mp4", 0, ownerWallet)
	require.NoError(t, err)

	t.Run("owner can check status", func(t *testing.T) {
		r := setupTranscodeRouterWithService(ownerWallet, svc)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/status/"+taskID, nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("different wallet gets 403", func(t *testing.T) {
		r2 := setupTranscodeRouterWithService("0xAttacker1234567890abcdef1234567890ab", svc)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/status/"+taskID, nil)
		r2.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "FORBIDDEN", resp["code"])
	})
}

func TestTranscodeHandlers_CancelOwnership(t *testing.T) {
	ownerWallet := "0xOwner1234567890abcdef1234567890abcdef12"
	svc := newTestTranscodingService()

	taskID, err := svc.Transcode(context.Background(), "content-2", "720p", "https://example.com/video.mp4", 0, ownerWallet)
	require.NoError(t, err)

	t.Run("different wallet cannot cancel", func(t *testing.T) {
		r2 := setupTranscodeRouterWithService("0xAttacker1234567890abcdef1234567890ab", svc)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/cancel/"+taskID, nil)
		r2.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "FORBIDDEN", resp["code"])
	})
}

func TestTranscodeHandlers_NilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xAny")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), nil)

	t.Run("status with nil service returns 503", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/status/some-id", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("tasks with nil service returns 503", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/tasks", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

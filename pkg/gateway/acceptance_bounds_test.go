package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func submitTranscodeRequest(t *testing.T, r *gin.Engine, contentID string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"content_id":"` + contentID + `","profile":"720p","input_url":"http://127.0.0.1:9000/video.mp4"}`
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func setupBoundsRouter(svc *service.TranscodingService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xOwner1234567890abcdef1234567890abcdef12")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), svc, TranscodeInputPolicy{AllowedHosts: []string{"127.0.0.1:9000"}})
	return r
}

func TestAcceptance_WorkerAndQueueBounds(t *testing.T) {
	t.Run("per-owner limit maps to 429 with Retry-After", func(t *testing.T) {
		svc := service.NewTranscodingService(nil, service.NewMemoryTranscodingQueue(), service.WithOwnerTaskLimit(2))
		r := setupBoundsRouter(svc)
		assert.Equal(t, http.StatusAccepted, submitTranscodeRequest(t, r, "own-1").Code)
		assert.Equal(t, http.StatusAccepted, submitTranscodeRequest(t, r, "own-2").Code)
		w := submitTranscodeRequest(t, r, "own-3")
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.NotEmpty(t, w.Header().Get("Retry-After"), "429 must carry Retry-After")
	})

	t.Run("queue full maps to 503 with Retry-After", func(t *testing.T) {
		svc := service.NewTranscodingService(nil, service.NewMemoryTranscodingQueue(), service.WithQueueLimit(1))
		r := setupBoundsRouter(svc)
		assert.Equal(t, http.StatusAccepted, submitTranscodeRequest(t, r, "q-1").Code)
		w := submitTranscodeRequest(t, r, "q-2")
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.NotEmpty(t, w.Header().Get("Retry-After"), "503 must carry Retry-After")
	})
}

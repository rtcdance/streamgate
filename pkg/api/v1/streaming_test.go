package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestStreamingHandler_GetHLS(t *testing.T) {
	handler := &StreamingHandler{}

	router := gin.New()
	router.GET("/stream/:id/hls", handler.GetHLS)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stream/123/hls", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/vnd.apple.mpegurl", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "#EXTM3U")
}

func TestStreamingHandler_GetDASH(t *testing.T) {
	handler := &StreamingHandler{}

	router := gin.New()
	router.GET("/stream/:id/dash", handler.GetDASH)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stream/123/dash", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/dash+xml", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "<MPD>")
}

func TestStreamingHandler_GetSegment(t *testing.T) {
	handler := &StreamingHandler{}

	router := gin.New()
	router.GET("/stream/:id/segment/:segment", handler.GetSegment)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stream/123/segment/001.ts", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "video/mp2t", w.Header().Get("Content-Type"))
}

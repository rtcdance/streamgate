package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUploadHandler_Upload(t *testing.T) {
	handler := &UploadHandler{}

	router := gin.New()
	router.POST("/upload", handler.Upload)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/upload", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "file_id")
}

func TestUploadHandler_UploadChunk(t *testing.T) {
	handler := &UploadHandler{}

	router := gin.New()
	router.POST("/upload/chunk", handler.UploadChunk)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/upload/chunk", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "chunk_index")
}

func TestUploadHandler_GetStatus(t *testing.T) {
	handler := &UploadHandler{}

	router := gin.New()
	router.GET("/upload/status/:id", handler.GetStatus)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/upload/status/123", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "status")
}

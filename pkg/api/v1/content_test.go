package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestContentHandler_List(t *testing.T) {
	handler := &ContentHandler{}

	router := gin.New()
	router.GET("/content", handler.List)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/content", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "content")
}

func TestContentHandler_Get(t *testing.T) {
	handler := &ContentHandler{}

	router := gin.New()
	router.GET("/content/:id", handler.Get)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/content/1", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "content")
}

func TestContentHandler_Create(t *testing.T) {
	handler := &ContentHandler{}

	router := gin.New()
	router.POST("/content", handler.Create)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/content", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "created")
}

func TestContentHandler_Update(t *testing.T) {
	handler := &ContentHandler{}

	router := gin.New()
	router.PUT("/content/:id", handler.Update)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/content/1", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "updated")
}

func TestContentHandler_Delete(t *testing.T) {
	handler := &ContentHandler{}

	router := gin.New()
	router.DELETE("/content/:id", handler.Delete)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/content/1", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

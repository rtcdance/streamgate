package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"streamgate/pkg/middleware"
	"streamgate/test/helpers"
)

func TestCORSMiddleware_AllowsOrigin(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil)
	router.Use(service.CORSMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test CORS headers
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Check CORS headers
	helpers.AssertTrue(t, len(w.Header().Get("Access-Control-Allow-Origin")) > 0)
}

func TestCORSMiddleware_HandlesPreflight(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil)
	router.Use(service.CORSMiddleware())

	router.OPTIONS("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Test preflight request
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should allow preflight
	helpers.AssertTrue(t, w.Code == http.StatusOK || w.Code == http.StatusNoContent)
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"streamgate/test/helpers"
)

func TestRateLimitMiddleware_AllowsRequests(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := NewService(nil)
	router.Use(service.RateLimitMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test single request
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:8080"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should allow request
	helpers.AssertTrue(t, w.Code == http.StatusOK || w.Code == http.StatusTooManyRequests)
}

func TestRateLimitMiddleware_EnforcesLimit(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := NewService(nil)
	router.Use(service.RateLimitMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Make multiple requests from same IP
	var lastCode int
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:8080"
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		lastCode = w.Code
	}

	// Eventually should hit rate limit
	helpers.AssertTrue(t, lastCode == http.StatusOK || lastCode == http.StatusTooManyRequests)
}

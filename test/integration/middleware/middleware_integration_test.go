package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"streamgate/pkg/middleware"
	"streamgate/test/helpers"
)

func TestMiddlewareStack_Integration(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)

	// Apply middleware stack
	router.Use(service.LoggingMiddleware())
	router.Use(service.CORSMiddleware())
	router.Use(service.RateLimitMiddleware())
	router.Use(service.AuthMiddleware())

	// Add test route
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test with valid token
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should pass through all middleware
	helpers.AssertTrue(t, w.Code == http.StatusOK || w.Code == http.StatusTooManyRequests)
}

func TestMiddlewareStack_AuthenticationRequired(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)

	// Apply middleware stack
	router.Use(service.LoggingMiddleware())
	router.Use(service.AuthMiddleware())

	// Add test route
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test without token
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should be rejected by auth middleware
	helpers.AssertEqual(t, http.StatusUnauthorized, w.Code)
}

func TestMiddlewareStack_CORSHeaders(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)

	// Apply CORS middleware
	router.Use(service.CORSMiddleware())

	// Add test route
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test CORS headers
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should have CORS headers
	helpers.AssertTrue(t, len(w.Header().Get("Access-Control-Allow-Origin")) > 0)
}

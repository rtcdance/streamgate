package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"streamgate/pkg/middleware"
	"streamgate/test/helpers"
)

func TestAuthMiddleware_WithValidToken(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create middleware service
	service := middleware.NewService(nil, nil, nil)
	router.Use(service.AuthMiddleware())

	// Add test route
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test with valid token
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should pass through (middleware doesn't validate token content in basic impl)
	helpers.AssertEqual(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_WithoutToken(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)
	router.Use(service.AuthMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test without token
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should be rejected
	helpers.AssertEqual(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_WithEmptyToken(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)
	router.Use(service.AuthMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test with empty token
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should be rejected
	helpers.AssertEqual(t, http.StatusUnauthorized, w.Code)
}

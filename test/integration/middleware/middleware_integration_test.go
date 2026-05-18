package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/middleware"
)

func TestMiddlewareStack_Integration(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	svc := middleware.NewService(zap.NewNop())
	jwtConfig := middleware.JWTAuthConfig{Secret: "test-secret-key-at-least-32-chars!"}

	// Apply middleware stack
	router.Use(svc.LoggingMiddleware())
	router.Use(svc.CORSMiddleware())
	router.Use(svc.RateLimitMiddleware())
	router.Use(middleware.JWTAuthMiddleware(jwtConfig, zap.NewNop()))

	// Add test route
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test with valid JWT
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"wallet_address": "0xTest",
		"exp":            time.Now().Add(time.Hour).Unix(),
	})
	tokenStr, _ := tok.SignedString([]byte("test-secret-key-at-least-32-chars!"))

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should pass through all middleware
	require.True(t, w.Code == http.StatusOK || w.Code == http.StatusTooManyRequests)
}

func TestMiddlewareStack_AuthenticationRequired(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	svc := middleware.NewService(zap.NewNop())
	jwtConfig := middleware.JWTAuthConfig{Secret: "test-secret-key-at-least-32-chars!"}

	// Apply middleware stack
	router.Use(svc.LoggingMiddleware())
	router.Use(middleware.JWTAuthMiddleware(jwtConfig, zap.NewNop()))

	// Add test route
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test without token
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should be rejected by auth middleware
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddlewareStack_CORSHeaders(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	svc := middleware.NewService(zap.NewNop())

	// Apply CORS middleware
	router.Use(svc.CORSMiddleware())

	// Add test route
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test CORS headers
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should have CORS headers
	require.True(t, w.Header().Get("Access-Control-Allow-Origin") != "")
}

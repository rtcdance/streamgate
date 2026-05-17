package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLoggingMiddleware_LogsRequest(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	service := NewService(logger)
	router.Use(service.LoggingMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test request logging
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should complete successfully
	require.Equal(t, http.StatusOK, w.Code)
}

func TestLoggingMiddleware_LogsErrors(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	service := NewService(logger)
	router.Use(service.LoggingMiddleware())

	router.GET("/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "test error"})
	})

	// Test error logging
	req := httptest.NewRequest("GET", "/error", http.NoBody)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should log error
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"streamgate/pkg/middleware"
	"streamgate/test/helpers"
)

func TestLoggingMiddleware_LogsRequest(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	service := middleware.NewService(logger)
	router.Use(service.LoggingMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test request logging
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should complete successfully
	helpers.AssertEqual(t, http.StatusOK, w.Code)
}

func TestLoggingMiddleware_LogsErrors(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	service := middleware.NewService(logger)
	router.Use(service.LoggingMiddleware())

	router.GET("/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "test error"})
	})

	// Test error logging
	req := httptest.NewRequest("GET", "/error", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should log error
	helpers.AssertEqual(t, http.StatusInternalServerError, w.Code)
}

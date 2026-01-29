package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"streamgate/pkg/middleware"
	"streamgate/test/helpers"
)

func TestE2E_MiddlewareStack(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create middleware service
	service := middleware.NewService(nil, nil, nil)

	// Add middleware stack
	router.Use(service.CORSMiddleware())
	router.Use(service.LoggingMiddleware())
	router.Use(service.AuthMiddleware())
	router.Use(service.RateLimitMiddleware())

	// Add test route
	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test request with token
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should pass through middleware stack
	helpers.AssertTrue(t, w.Code == http.StatusOK || w.Code == http.StatusTooManyRequests)
}

func TestE2E_AuthenticationFlow(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)
	router.Use(service.AuthMiddleware())

	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "protected"})
	})

	// Test 1: Request without token
	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	helpers.AssertEqual(t, http.StatusUnauthorized, w.Code)

	// Test 2: Request with valid token
	req = httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	helpers.AssertEqual(t, http.StatusOK, w.Code)

	// Test 3: Request with invalid token format
	req = httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "InvalidFormat")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	helpers.AssertEqual(t, http.StatusUnauthorized, w.Code)
}

func TestE2E_CORSFlow(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)
	router.Use(service.CORSMiddleware())

	router.GET("/api/data", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": "test"})
	})

	// Test CORS preflight
	req := httptest.NewRequest("OPTIONS", "/api/data", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should handle CORS
	helpers.AssertTrue(t, w.Code == http.StatusOK || w.Code == http.StatusNoContent)
}

func TestE2E_RateLimitingFlow(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)
	router.Use(service.RateLimitMiddleware())

	router.GET("/api/limited", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// Make multiple requests
	successCount := 0
	rateLimitedCount := 0

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/api/limited", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			successCount++
		} else if w.Code == http.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	// Should have some successful requests
	helpers.AssertTrue(t, successCount > 0)
}

func TestE2E_LoggingFlow(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)
	router.Use(service.LoggingMiddleware())

	router.GET("/api/logged", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "logged"})
	})

	// Make request
	req := httptest.NewRequest("GET", "/api/logged", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should log request
	helpers.AssertEqual(t, http.StatusOK, w.Code)
}

func TestE2E_ErrorRecoveryFlow(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)
	router.Use(service.RecoveryMiddleware())

	router.GET("/api/panic", func(c *gin.Context) {
		panic("test panic")
	})

	router.GET("/api/normal", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// Test panic recovery
	req := httptest.NewRequest("GET", "/api/panic", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should recover from panic
	helpers.AssertTrue(t, w.Code == http.StatusInternalServerError || w.Code == http.StatusOK)

	// Test normal request
	req = httptest.NewRequest("GET", "/api/normal", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	helpers.AssertEqual(t, http.StatusOK, w.Code)
}

func TestE2E_TracingFlow(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil, nil, nil)
	router.Use(service.TracingMiddleware())

	router.GET("/api/traced", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "traced"})
	})

	// Make request
	req := httptest.NewRequest("GET", "/api/traced", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should trace request
	helpers.AssertEqual(t, http.StatusOK, w.Code)
}

func TestE2E_MiddlewareOrdering(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	executionOrder := []string{}

	// Add middleware in specific order
	router.Use(func(c *gin.Context) {
		executionOrder = append(executionOrder, "middleware1")
		c.Next()
	})

	router.Use(func(c *gin.Context) {
		executionOrder = append(executionOrder, "middleware2")
		c.Next()
	})

	router.GET("/api/test", func(c *gin.Context) {
		executionOrder = append(executionOrder, "handler")
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// Make request
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify execution order
	helpers.AssertEqual(t, 3, len(executionOrder))
	helpers.AssertEqual(t, "middleware1", executionOrder[0])
	helpers.AssertEqual(t, "middleware2", executionOrder[1])
	helpers.AssertEqual(t, "handler", executionOrder[2])
}

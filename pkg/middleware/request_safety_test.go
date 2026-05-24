package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupSafetyRouter(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(handler)
	router.POST("/api/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.POST("/api/v1/upload/chunk", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "uploaded"})
	})
	router.GET("/api/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return router
}

func TestContentTypeMiddleware_JSONAccepted(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.ContentTypeMiddleware())

	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestContentTypeMiddleware_JSONWithCharset(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.ContentTypeMiddleware())

	tests := []struct {
		name        string
		contentType string
	}{
		{"charset with space", "application/json; charset=utf-8"},
		{"charset without space", "application/json;charset=utf-8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestContentTypeMiddleware_WrongContentType(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.ContentTypeMiddleware())

	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`data`))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
	assert.Contains(t, w.Body.String(), "UNSUPPORTED_MEDIA_TYPE")
}

func TestContentTypeMiddleware_FormURLEncoded(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.ContentTypeMiddleware())

	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`a=1`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
}

func TestContentTypeMiddleware_GetSkipped(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.ContentTypeMiddleware())

	req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestContentTypeMiddleware_DeleteSkipped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(nil)
	router.Use(svc.ContentTypeMiddleware())
	router.DELETE("/api/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("DELETE", "/api/v1/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestContentTypeMiddleware_HeadSkipped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(nil)
	router.Use(svc.ContentTypeMiddleware())
	router.HEAD("/api/v1/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("HEAD", "/api/v1/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestContentTypeMiddleware_OptionsSkipped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(nil)
	router.Use(svc.ContentTypeMiddleware())
	router.OPTIONS("/api/v1/test", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest("OPTIONS", "/api/v1/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestContentTypeMiddleware_UploadSkipped(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.ContentTypeMiddleware())

	req := httptest.NewRequest("POST", "/api/v1/upload/chunk", strings.NewReader(`data`))
	req.Header.Set("Content-Type", "multipart/form-data")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestContentTypeMiddleware_ZeroContentLength(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.ContentTypeMiddleware())

	req := httptest.NewRequest("POST", "/api/v1/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestContentTypeMiddleware_NonAPIPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(nil)
	router.Use(svc.ContentTypeMiddleware())
	router.POST("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("POST", "/health", strings.NewReader(`data`))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequestSizeLimitMiddleware_WithinLimit(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.RequestSizeLimitMiddleware(1024))

	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequestSizeLimitMiddleware_ExceedsLimit(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.RequestSizeLimitMiddleware(10))

	body := strings.NewReader(`{"key":"this is definitely more than 10 bytes"}`)
	req := httptest.NewRequest("POST", "/api/v1/test", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), "PAYLOAD_TOO_LARGE")
}

func TestRequestSizeLimitMiddleware_UploadExempt(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.RequestSizeLimitMiddleware(10))

	body := strings.NewReader(`{"large":"this is definitely more than 10 bytes of data"}`)
	req := httptest.NewRequest("POST", "/api/v1/upload/chunk", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequestSizeLimitMiddleware_ZeroUsesDefault(t *testing.T) {
	svc := NewService(nil)
	handler := svc.RequestSizeLimitMiddleware(0)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(handler)
	router.POST("/api/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	smallBody := strings.NewReader(`{}`)
	req := httptest.NewRequest("POST", "/api/v1/test", smallBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequestSizeLimitMiddleware_NegativeUsesDefault(t *testing.T) {
	svc := NewService(nil)
	handler := svc.RequestSizeLimitMiddleware(-5)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(handler)
	router.POST("/api/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSecurityHeadersMiddleware_SetsHeaders(t *testing.T) {
	svc := NewService(nil)
	router := setupSafetyRouter(svc.SecurityHeadersMiddleware())

	req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
	assert.Equal(t, "default-src 'self'", w.Header().Get("Content-Security-Policy"))
	assert.Contains(t, w.Header().Get("Strict-Transport-Security"), "max-age=63072000")
	assert.Contains(t, w.Header().Get("Strict-Transport-Security"), "includeSubDomains")
	assert.Contains(t, w.Header().Get("Strict-Transport-Security"), "preload")
}

func TestSecurityHeadersMiddleware_AppliesToAllResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(nil)
	router.Use(svc.SecurityHeadersMiddleware())
	router.GET("/ok", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "fail"})
	})

	tests := []struct {
		name       string
		path       string
		expectCode int
	}{
		{"ok response", "/ok", http.StatusOK},
		{"error response", "/error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, http.NoBody)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectCode, w.Code)
			assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
		})
	}
}

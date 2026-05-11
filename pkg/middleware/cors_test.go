package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"streamgate/test/helpers"
)

func TestCORSMiddleware_AllowsOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := NewService(nil)
	router.Use(service.CORSMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	helpers.AssertTrue(t, len(w.Header().Get("Access-Control-Allow-Origin")) > 0)
}

func TestCORSMiddleware_HandlesPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := NewService(nil)
	router.Use(service.CORSMiddleware())

	router.OPTIONS("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	helpers.AssertTrue(t, w.Code == http.StatusOK || w.Code == http.StatusNoContent)
}

func TestCORSMiddleware_AllowedOrigins_EchoesOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := NewService(nil)
	router.Use(service.CORSMiddleware("http://localhost:3000", "https://app.example.com"))

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	helpers.AssertEqual(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
	helpers.AssertEqual(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSMiddleware_AllowedOrigins_RejectsUnknownOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := NewService(nil)
	router.Use(service.CORSMiddleware("http://localhost:3000"))

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	helpers.AssertEqual(t, "", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSMiddleware_AllowedOrigins_PreflightRejectsUnknownOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := NewService(nil)
	router.Use(service.CORSMiddleware("http://localhost:3000"))

	router.OPTIONS("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	helpers.AssertEqual(t, http.StatusForbidden, w.Code)
}

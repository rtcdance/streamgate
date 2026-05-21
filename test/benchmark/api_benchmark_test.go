package benchmark_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
)

func BenchmarkAPI_RoutingSimple(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/test", http.NoBody)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkAPI_RoutingWithMiddleware(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil)
	router.Use(service.LoggingMiddleware())
	router.Use(service.CORSMiddleware())

	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/test", http.NoBody)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkAPI_JSONSerialization(b *testing.B) {
	data := map[string]interface{}{
		"id":    "123",
		"name":  "test",
		"email": "test@example.com",
		"age":   30,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(data)
	}
}

func BenchmarkAPI_JSONDeserialization(b *testing.B) {
	data := map[string]interface{}{
		"id":    "123",
		"name":  "test",
		"email": "test@example.com",
		"age":   30,
	}
	jsonData, _ := json.Marshal(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result map[string]interface{}
		_ = json.Unmarshal(jsonData, &result)
	}
}

func BenchmarkAPI_POSTRequest(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.POST("/api/data", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"id": "123"})
	})

	payload := map[string]string{"name": "test"}
	bodyBytes, _ := json.Marshal(payload)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/api/data", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkAPI_Authentication(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	jwtConfig := middleware.JWTAuthConfig{Secret: "bench-secret-key-at-least-32-chars!"}
	router.Use(middleware.JWTAuthMiddleware(jwtConfig, zap.NewNop()))

	router.GET("/api/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "protected"})
	})

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"wallet_address": "0xBench",
		"exp":            time.Now().Add(time.Hour).Unix(),
	})
	tokenStr, _ := tok.SignedString([]byte("bench-secret-key"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/protected", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkAPI_RateLimiting(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil)
	router.Use(service.RateLimitMiddleware())

	router.GET("/api/limited", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/api/limited", http.NoBody)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})
}

func BenchmarkAPI_ErrorHandling(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	service := middleware.NewService(nil)
	router.Use(service.RecoveryMiddleware())

	router.GET("/api/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error"})
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/error", http.NoBody)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkAPI_ConcurrentRequests(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/api/test", http.NoBody)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})
}

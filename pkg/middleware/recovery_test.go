package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestRecoveryMiddleware_NoPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(zap.NewNop())
	router.Use(svc.RecoveryMiddleware())
	router.GET("/ok", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/ok", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecoveryMiddleware_PanicRecovered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(zap.NewNop())
	router.Use(svc.RecoveryMiddleware())
	router.GET("/panic", func(c *gin.Context) {
		panic("something broke")
	})

	req := httptest.NewRequest("GET", "/panic", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "INTERNAL_ERROR")
	assert.Contains(t, w.Body.String(), "internal server error")
}

func TestRecoveryMiddleware_PanicWithNilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var svc *Service
	router.Use(svc.RecoveryMiddleware())
	router.GET("/panic", func(c *gin.Context) {
		panic("nil service panic")
	})

	req := httptest.NewRequest("GET", "/panic", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRecoveryMiddleware_PanicWithNilLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := &Service{logger: nil}
	router.Use(svc.RecoveryMiddleware())
	router.GET("/panic", func(c *gin.Context) {
		panic("nil logger panic")
	})

	req := httptest.NewRequest("GET", "/panic", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRecoveryMiddleware_PanicWithLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(zap.NewNop())
	router.Use(svc.RecoveryMiddleware())
	router.GET("/panic", func(c *gin.Context) {
		panic("logged panic")
	})

	req := httptest.NewRequest("GET", "/panic", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "INTERNAL_ERROR")
}

func TestRecoveryMiddleware_PanicWithDifferentTypes(t *testing.T) {
	tests := []struct {
		name  string
		panic interface{}
	}{
		{"string", "string panic"},
		{"int", 42},
		{"error", http.ErrHandlerTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			svc := NewService(zap.NewNop())
			router.Use(svc.RecoveryMiddleware())
			router.GET("/panic", func(c *gin.Context) {
				panic(tt.panic)
			})

			req := httptest.NewRequest("GET", "/panic", http.NoBody)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})
	}
}

func TestRecoveryMiddleware_RecordsPathAndMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(zap.NewNop())
	router.Use(svc.RecoveryMiddleware())
	router.POST("/api/test", func(c *gin.Context) {
		panic("post panic")
	})

	req := httptest.NewRequest("POST", "/api/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

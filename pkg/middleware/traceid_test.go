package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRequestIDFromCtx_ContextValue(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-123")
	id := RequestIDFromCtx(ctx)
	assert.Equal(t, "req-123", id)
}

func TestRequestIDFromCtx_EmptyContext(t *testing.T) {
	id := RequestIDFromCtx(context.Background())
	assert.Equal(t, "", id)
}

func TestRequestIDFromCtx_GinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "gin-req-456")

	id := RequestIDFromCtx(c)
	assert.Equal(t, "gin-req-456", id)
}

func TestRequestIDFromCtx_GinContextNoID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	id := RequestIDFromCtx(c)
	assert.Equal(t, "", id)
}

func TestRequestIDFromCtx_ContextValueTakesPrecedence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "gin-id")

	ctx := ContextWithRequestID(c, "ctx-id")
	id := RequestIDFromCtx(ctx)
	assert.Equal(t, "ctx-id", id)
}

func TestRequestIDFromCtx_WrongTypeValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), requestIDKey{}, 42)
	id := RequestIDFromCtx(ctx)
	assert.Equal(t, "", id)
}

func TestContextWithRequestID(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "test-id")
	require.NotNil(t, ctx)

	id := RequestIDFromCtx(ctx)
	assert.Equal(t, "test-id", id)
}

func TestTraceIDMiddleware_WithRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	logger := zap.NewNop()
	svc := NewService(logger)

	router.Use(func(c *gin.Context) {
		c.Set("request_id", "trace-abc-123")
		c.Next()
	})
	router.Use(svc.TraceIDMiddleware())
	router.GET("/test", func(c *gin.Context) {
		l, exists := c.Get("logger")
		assert.True(t, exists)
		assert.NotNil(t, l)

		reqID := RequestIDFromCtx(c.Request.Context())
		assert.Equal(t, "trace-abc-123", reqID)

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTraceIDMiddleware_WithoutRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	logger := zap.NewNop()
	svc := NewService(logger)

	router.Use(svc.TraceIDMiddleware())
	router.GET("/test", func(c *gin.Context) {
		_, exists := c.Get("logger")
		assert.True(t, exists, "logger should be set even without request_id")

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTraceIDMiddleware_NilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var svc *Service

	router.Use(func(c *gin.Context) {
		c.Set("request_id", "test-id")
		c.Next()
	})

	handler := svc.TraceIDMiddleware()
	assert.NotNil(t, handler)
}

func TestTraceIDMiddleware_NilLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := &Service{logger: nil}

	router.Use(func(c *gin.Context) {
		c.Set("request_id", "test-id")
		c.Next()
	})
	router.Use(svc.TraceIDMiddleware())
	router.GET("/test", func(c *gin.Context) {
		_, exists := c.Get("logger")
		assert.False(t, exists, "logger should not be set when service logger is nil")

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetLogger_WithContextLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	testLogger := zap.NewNop()
	c.Set("logger", testLogger)

	result := GetLogger(c, nil)
	assert.Equal(t, testLogger, result)
}

func TestGetLogger_Fallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	fallback := zap.NewNop()
	result := GetLogger(c, fallback)
	assert.Equal(t, fallback, result)
}

func TestGetLogger_NilFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	result := GetLogger(c, nil)
	assert.Nil(t, result)
}

func TestGetLogger_WrongType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	c.Set("logger", "not-a-logger")
	fallback := zap.NewNop()
	result := GetLogger(c, fallback)
	assert.Equal(t, fallback, result)
}

func TestTraceIDMiddleware_PropagatesToRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	logger := zap.NewNop()
	svc := NewService(logger)

	router.Use(func(c *gin.Context) {
		c.Set("request_id", "propagated-id")
		c.Next()
	})
	router.Use(svc.TraceIDMiddleware())
	router.GET("/test", func(c *gin.Context) {
		ctxID := RequestIDFromCtx(c.Request.Context())
		assert.Equal(t, "propagated-id", ctxID)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
}

func TestTracingMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(zap.NewNop())

	handler := svc.TracingMiddleware()
	assert.NotNil(t, handler)

	router.Use(handler)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

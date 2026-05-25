package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSetErrorLogger(t *testing.T) {
	log := zap.NewNop()
	SetErrorLogger(log)
	errLoggerMu.RLock()
	stored := errLogger
	errLoggerMu.RUnlock()
	assert.Equal(t, log, stored)
}

func TestAPIError_WithDetail(t *testing.T) {
	err := APIError{Error: "test error", Code: "TEST_CODE"}
	result := err.WithDetail("some detail")
	assert.Equal(t, "some detail", result.Detail)
	assert.Equal(t, "test error", result.Error)
	assert.Equal(t, "TEST_CODE", result.Code)
	assert.Empty(t, err.Detail)
}

func TestRequestIDMiddleware_SetsRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/test", func(c *gin.Context) {
		reqID, exists := c.Get("request_id")
		assert.True(t, exists)
		id, ok := reqID.(string)
		assert.True(t, ok)
		assert.NotEmpty(t, id)
		assert.Contains(t, id, "req-")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequestIDMiddleware_SetsHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
}

func TestRequestIDMiddleware_UniquePerRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	var ids []string
	r.GET("/test", func(c *gin.Context) {
		id, _ := c.Get("request_id")
		ids = append(ids, id.(string))
		c.Status(http.StatusOK)
	})

	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", http.NoBody)
		r.ServeHTTP(w, req)
	}

	seen := make(map[string]bool)
	for _, id := range ids {
		assert.False(t, seen[id], "request IDs should be unique")
		seen[id] = true
	}
}

func TestAbortWithError_IncludesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", http.NoBody)
	c.Set("request_id", "req-test-123")

	abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "bad request")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp APIError
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "req-test-123", resp.RequestID)
	assert.Equal(t, ErrInvalidRequest, resp.Code)
}

func TestAbortWithError_NoRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", http.NoBody)

	abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "bad request")

	var resp APIError
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Empty(t, resp.RequestID)
}

func TestAbortWithErrorDetail_5xx_HidesDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", http.NoBody)
	c.Set("request_id", "req-500")

	abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, "internal error", "secret db connection string")

	var resp APIError
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Empty(t, resp.Detail, "5xx errors should not expose detail to client")
	assert.Equal(t, ErrInternalError, resp.Code)
}

func TestAbortWithErrorDetail_4xx_ShowsDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", http.NoBody)

	abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "bad request", "field X is required")

	var resp APIError
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "field X is required", resp.Detail)
}

func TestAbortWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", http.NoBody)
	c.Set("request_id", "req-val")

	abortWithValidationError(c, map[string]string{
		"email":   "invalid format",
		"wallet":  "required",
		"_error":  "custom error message",
	})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "custom error message", resp["error"])
	assert.Equal(t, ErrInvalidRequest, resp["code"])
	assert.NotNil(t, resp["validation"])
}

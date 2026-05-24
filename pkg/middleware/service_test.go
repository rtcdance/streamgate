package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewService(t *testing.T) {
	svc := NewService(zap.NewNop())

	require.NotNil(t, svc)
	assert.NotNil(t, svc.cbManager)
	assert.NotNil(t, svc.rateLimiter)
}

func TestNewService_NilLogger(t *testing.T) {
	svc := NewService(nil)

	require.NotNil(t, svc)
	assert.NotNil(t, svc.cbManager)
	assert.NotNil(t, svc.rateLimiter)
}

func TestNewServiceWithRedis(t *testing.T) {
	svc := NewServiceWithRedis(zap.NewNop(), nil)

	require.NotNil(t, svc)
	assert.NotNil(t, svc.cbManager)
	assert.NotNil(t, svc.rateLimiter)
}

func TestService_CircuitBreakerManager(t *testing.T) {
	svc := NewService(zap.NewNop())

	mgr := svc.CircuitBreakerManager()
	require.NotNil(t, mgr)
}

func TestService_DependencyCircuitBreaker(t *testing.T) {
	svc := NewService(zap.NewNop())
	config := DefaultCircuitBreakerConfig()

	cb := svc.DependencyCircuitBreaker("test-dep", config)
	require.NotNil(t, cb)
	assert.Equal(t, "test-dep", cb.Stats().Name)
}

func TestService_DependencyCircuitBreaker_SameName(t *testing.T) {
	svc := NewService(zap.NewNop())
	config := DefaultCircuitBreakerConfig()

	cb1 := svc.DependencyCircuitBreaker("same-name", config)
	cb2 := svc.DependencyCircuitBreaker("same-name", config)

	assert.Same(t, cb1, cb2)
}

func TestService_ExecuteWithCB_Success(t *testing.T) {
	svc := NewService(zap.NewNop())
	config := DefaultCircuitBreakerConfig()

	err := svc.ExecuteWithCB(context.Background(), "test-cb", config, func() error {
		return nil
	})

	assert.NoError(t, err)
}

func TestService_ExecuteWithCB_Failure(t *testing.T) {
	svc := NewService(zap.NewNop())
	config := DefaultCircuitBreakerConfig()

	err := svc.ExecuteWithCB(context.Background(), "test-cb", config, func() error {
		return errors.New("test error")
	})

	assert.Error(t, err)
}

func TestService_AllCircuitBreakerStats(t *testing.T) {
	svc := NewService(zap.NewNop())
	config := DefaultCircuitBreakerConfig()

	svc.DependencyCircuitBreaker("cb1", config)
	svc.DependencyCircuitBreaker("cb2", config)

	stats := svc.AllCircuitBreakerStats()
	assert.Len(t, stats, 2)
	assert.Contains(t, stats, "cb1")
	assert.Contains(t, stats, "cb2")
}

func TestService_Close(t *testing.T) {
	svc := NewService(zap.NewNop())
	svc.Close()
}

func TestService_Close_NilRateLimiter(t *testing.T) {
	svc := &Service{rateLimiter: nil}
	svc.Close()
}

func TestServiceChain_NoMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	result := ServiceChain(handler)
	require.NotNil(t, result)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	result.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServiceChain_WithMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-MW1", "true")
			next.ServeHTTP(w, r)
		})
	}

	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-MW2", "true")
			next.ServeHTTP(w, r)
		})
	}

	result := ServiceChain(handler, mw1, mw2)
	require.NotNil(t, result)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	result.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "true", w.Header().Get("X-MW1"))
	assert.Equal(t, "true", w.Header().Get("X-MW2"))
}

func TestServiceChain_MiddlewareOrder(t *testing.T) {
	var order []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw1-after")
		})
	}

	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw2-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw2-after")
		})
	}

	result := ServiceChain(handler, mw1, mw2)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	result.ServeHTTP(w, req)

	assert.Equal(t, []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}, order)
}

func TestCircuitBreakerMiddleware_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(zap.NewNop())
	config := DefaultCircuitBreakerConfig()

	router.Use(svc.CircuitBreakerMiddleware("test", config))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCircuitBreakerMiddleware_ServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := NewService(zap.NewNop())
	config := CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          1 * time.Second,
	}

	router.Use(svc.CircuitBreakerMiddleware("test-err", config))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "fail"})
	})

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	req2 := httptest.NewRequest("GET", "/test", http.NoBody)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusServiceUnavailable, w2.Code)
}

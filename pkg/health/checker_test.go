package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewHealthChecker(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	assert.NotNil(t, hc)
	assert.NotNil(t, hc.checks)
	assert.NotNil(t, hc.results)
	assert.Equal(t, "1.0.0", hc.version)
}

func TestRegisterCheck(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })

	_, exists := hc.checks["db"]
	assert.True(t, exists)
}

func TestUnregisterCheck(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })
	hc.UnregisterCheck("db")

	_, exists := hc.checks["db"]
	assert.False(t, exists)
}

func TestCheck_Healthy(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })

	result := hc.Check(context.Background(), "db")
	assert.Equal(t, "db", result.Name)
	assert.Equal(t, StatusHealthy, result.Status)
	assert.Equal(t, "OK", result.Message)
	assert.GreaterOrEqual(t, result.Duration, int64(0))
}

func TestCheck_Unhealthy(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error {
		return errors.New("connection refused")
	})

	result := hc.Check(context.Background(), "db")
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.Equal(t, "connection refused", result.Message)
}

func TestCheck_NotFound(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())

	result := hc.Check(context.Background(), "nonexistent")
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.Equal(t, "check not found", result.Message)
}

func TestCheckAll_Healthy(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })
	hc.RegisterCheck("redis", func(ctx context.Context) error { return nil })

	resp := hc.CheckAll(context.Background())
	assert.Equal(t, StatusHealthy, resp.Status)
	assert.Len(t, resp.Checks, 2)
	assert.Equal(t, "1.0.0", resp.Version)
}

func TestCheckAll_Degraded(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })
	hc.RegisterCheck("redis", func(ctx context.Context) error {
		return errors.New("timeout")
	})

	resp := hc.CheckAll(context.Background())
	assert.Equal(t, StatusUnhealthy, resp.Status)
	assert.Len(t, resp.Checks, 2)
}

func TestCheckAll_Unhealthy(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error {
		return errors.New("connection refused")
	})

	resp := hc.CheckAll(context.Background())
	assert.Equal(t, StatusUnhealthy, resp.Status)
}

func TestLiveness(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	resp := hc.Liveness(context.Background())
	assert.True(t, resp.Alive)
	assert.NotZero(t, resp.Timestamp)
}

func TestReadiness_Ready(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })

	resp := hc.Readiness(context.Background())
	assert.True(t, resp.Ready)
	assert.Len(t, resp.Checks, 1)
}

func TestReadiness_NotReady(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error {
		return errors.New("down")
	})

	resp := hc.Readiness(context.Background())
	assert.False(t, resp.Ready)
}

func TestGetResults(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })
	hc.Check(context.Background(), "db")

	results := hc.GetResults()
	assert.Len(t, results, 1)
	assert.Contains(t, results, "db")
}

func TestSetVersion(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.SetVersion("2.0.0")

	resp := hc.CheckAll(context.Background())
	assert.Equal(t, "2.0.0", resp.Version)
}

func TestSetRelease(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.SetRelease("v2.0.0-beta")

	resp := hc.CheckAll(context.Background())
	assert.Equal(t, "v2.0.0-beta", resp.Release)
}

func TestSetTimeout(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.SetTimeout(10 * time.Second)
	assert.Equal(t, 10*time.Second, hc.timeout)
}

func TestHTTPHandler_Liveness(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	server := httptest.NewServer(hc.HTTPHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/health/live")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var liveResp LivenessResponse
	body, _ := io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(body, &liveResp))
	assert.True(t, liveResp.Alive)
	assert.NotZero(t, liveResp.Timestamp)
}

func TestHTTPHandler_Readiness_Ready(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })
	server := httptest.NewServer(hc.HTTPHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/health/ready")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var readyResp ReadinessResponse
	body, _ := io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(body, &readyResp))
	assert.True(t, readyResp.Ready)
}

func TestHTTPHandler_Readiness_NotReady(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error {
		return errors.New("down")
	})
	server := httptest.NewServer(hc.HTTPHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/health/ready")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var readyResp ReadinessResponse
	body, _ := io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(body, &readyResp))
	assert.False(t, readyResp.Ready)
}

func TestHTTPHandler_Health_Healthy(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })
	server := httptest.NewServer(hc.HTTPHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var healthResp HealthResponse
	body, _ := io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(body, &healthResp))
	assert.Equal(t, StatusHealthy, healthResp.Status)
}

func TestHTTPHandler_Health_Unhealthy(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error {
		return errors.New("down")
	})
	server := httptest.NewServer(hc.HTTPHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var healthResp HealthResponse
	body, _ := io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(body, &healthResp))
	assert.Equal(t, StatusUnhealthy, healthResp.Status)
}

func TestHTTPHandler_Health_Degraded(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func(ctx context.Context) error { return nil })
	hc.RegisterCheck("cache", func(ctx context.Context) error { return nil })
	server := httptest.NewServer(hc.HTTPHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHTTPHandler_NotFound(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	server := httptest.NewServer(hc.HTTPHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/health/unknown")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCommonHealthChecks_DatabaseCheck_Success(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.DatabaseCheck(func() error { return nil })
	assert.NoError(t, check(context.Background()))
}

func TestCommonHealthChecks_DatabaseCheck_Error(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.DatabaseCheck(func() error { return errors.New("db down") })
	err := check(context.Background())
	assert.Error(t, err)
	assert.Equal(t, "db down", err.Error())
}

func TestCommonHealthChecks_RedisCheck_Success(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.RedisCheck(func() error { return nil })
	assert.NoError(t, check(context.Background()))
}

func TestCommonHealthChecks_RedisCheck_Error(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.RedisCheck(func() error { return errors.New("redis down") })
	err := check(context.Background())
	assert.Error(t, err)
	assert.Equal(t, "redis down", err.Error())
}

func TestCommonHealthChecks_StorageCheck_Success(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.StorageCheck(func() error { return nil })
	assert.NoError(t, check(context.Background()))
}

func TestCommonHealthChecks_StorageCheck_Error(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.StorageCheck(func() error { return errors.New("storage down") })
	err := check(context.Background())
	assert.Error(t, err)
	assert.Equal(t, "storage down", err.Error())
}

func TestCommonHealthChecks_ExternalServiceCheck_Success(t *testing.T) {
	extServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer extServer.Close()

	cc := &CommonHealthChecks{}
	check := cc.ExternalServiceCheck(extServer.URL)
	assert.NoError(t, check(context.Background()))
}

func TestCommonHealthChecks_ExternalServiceCheck_Unhealthy(t *testing.T) {
	extServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer extServer.Close()

	cc := &CommonHealthChecks{}
	check := cc.ExternalServiceCheck(extServer.URL)
	err := check(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service unhealthy")
}

func TestCommonHealthChecks_ExternalServiceCheck_Unreachable(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.ExternalServiceCheck("http://127.0.0.1:1")
	err := check(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service unavailable")
}

func TestCommonHealthChecks_DiskSpaceCheck_Success(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.DiskSpaceCheck(os.TempDir(), 0)
	assert.NoError(t, check(context.Background()))
}

func TestCommonHealthChecks_DiskSpaceCheck_LowSpace(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.DiskSpaceCheck(os.TempDir(), 1e18)
	err := check(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disk space low")
}

func TestCommonHealthChecks_DiskSpaceCheck_InvalidPath(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.DiskSpaceCheck("/nonexistent/path/that/does/not/exist", 0)
	err := check(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disk check failed")
}

func TestCommonHealthChecks_MemoryCheck_Success(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.MemoryCheck(100.0)
	assert.NoError(t, check(context.Background()))
}

func TestCommonHealthChecks_GoroutineCheck_Success(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.GoroutineCheck(1000000)
	assert.NoError(t, check(context.Background()))
}

func TestCommonHealthChecks_GoroutineCheck_Exceeded(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.GoroutineCheck(1)
	err := check(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "goroutine count")
}

func TestCommonHealthChecks_PluginCheck_Loaded(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.PluginCheck("auth", func() bool { return true })
	assert.NoError(t, check(context.Background()))
}

func TestCommonHealthChecks_PluginCheck_NotLoaded(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.PluginCheck("auth", func() bool { return false })
	err := check(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plugin 'auth' not loaded")
}

func TestCommonHealthChecks_DependencyCheck_Success(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.DependencyCheck("payment", func() error { return nil })
	assert.NoError(t, check(context.Background()))
}

func TestCommonHealthChecks_DependencyCheck_Error(t *testing.T) {
	cc := &CommonHealthChecks{}
	check := cc.DependencyCheck("payment", func() error { return errors.New("timeout") })
	err := check(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dependency 'payment' check failed")
	assert.Contains(t, err.Error(), "timeout")
}

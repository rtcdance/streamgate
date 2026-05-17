package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
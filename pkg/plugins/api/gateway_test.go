package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10)
	defer rl.Stop()

	assert.NotNil(t, rl)
	assert.Equal(t, float64(10), rl.limit)
	assert.Equal(t, 10, rl.burst)
}

func TestNewRateLimiter_ZeroLimit(t *testing.T) {
	rl := NewRateLimiter(0)
	defer rl.Stop()

	assert.Equal(t, float64(1), rl.limit)
	assert.Equal(t, 1, rl.burst)
}

func TestNewRateLimiter_NegativeLimit(t *testing.T) {
	rl := NewRateLimiter(-5)
	defer rl.Stop()

	assert.Equal(t, float64(1), rl.limit)
}

func TestRateLimiter_Allow_FirstRequest(t *testing.T) {
	rl := NewRateLimiter(10)
	defer rl.Stop()

	result := rl.Allow("192.168.1.1")
	assert.True(t, result)
}

func TestRateLimiter_Allow_WithinBurst(t *testing.T) {
	rl := NewRateLimiter(100)
	defer rl.Stop()

	for i := 0; i < 10; i++ {
		result := rl.Allow("192.168.1.1")
		assert.True(t, result, "request %d should be allowed", i)
	}
}

func TestRateLimiter_Allow_DifferentClients(t *testing.T) {
	rl := NewRateLimiter(1)
	defer rl.Stop()

	assert.True(t, rl.Allow("client-a"))
	assert.True(t, rl.Allow("client-b"))
}

func TestRateLimiter_Stop(t *testing.T) {
	rl := NewRateLimiter(10)
	rl.Stop()
}

func TestBucket_RefillTokens(t *testing.T) {
	b := &bucket{
		tokens:     5,
		maxTokens:  10,
		refill:     1,
		lastRefill: time.Now().Add(-5 * time.Second),
	}

	b.refillTokens()

	assert.GreaterOrEqual(t, b.tokens, float64(5))
	assert.LessOrEqual(t, b.tokens, float64(10))
}

func TestBucket_RefillTokens_CappedAtMax(t *testing.T) {
	b := &bucket{
		tokens:     9,
		maxTokens:  10,
		refill:     100,
		lastRefill: time.Now().Add(-1 * time.Second),
	}

	b.refillTokens()

	assert.Equal(t, float64(10), b.tokens)
}

func TestBucketHeap(t *testing.T) {
	h := &bucketHeap{}

	now := time.Now()
	entry1 := &bucketEntry{ip: "1", lastAccess: now.Add(-2 * time.Minute)}
	entry2 := &bucketEntry{ip: "2", lastAccess: now.Add(-1 * time.Minute)}
	entry3 := &bucketEntry{ip: "3", lastAccess: now}

	*h = append(*h, entry3, entry1, entry2)

	assert.Equal(t, 3, h.Len())
	assert.False(t, h.Less(0, 1))

	h.Swap(0, 1)
	assert.Equal(t, entry3, (*h)[1])
	h.Swap(0, 1)

	h.Push(entry2)
	assert.Equal(t, 4, h.Len())

	popped := h.Pop()
	assert.NotNil(t, popped)
}

func TestNewGatewayPlugin(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewGatewayPlugin(cfg, zap.NewNop())

	assert.NotNil(t, plugin)
	assert.Equal(t, "api-gateway", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
}

func TestGatewayPlugin_Init(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewGatewayPlugin(cfg, zap.NewNop())

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, plugin.metricsCollector)
	assert.NotNil(t, plugin.alertManager)
}

func TestGatewayPlugin_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewGatewayPlugin(cfg, zap.NewNop())

	err := plugin.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestGatewayPlugin_DependsOn(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewGatewayPlugin(cfg, zap.NewNop())

	deps := plugin.DependsOn()
	assert.Nil(t, deps)
}

func TestRateLimiter_Allow_ExceedsBurst(t *testing.T) {
	rl := NewRateLimiter(1)
	defer rl.Stop()

	assert.True(t, rl.Allow("client-x"))
	assert.False(t, rl.Allow("client-x"))
}

func TestRateLimiter_Allow_RefillOverTime(t *testing.T) {
	rl := NewRateLimiter(1000)
	defer rl.Stop()

	assert.True(t, rl.Allow("client-y"))

	b, ok := rl.buckets.Load("client-y")
	assert.True(t, ok)
	bucket := b.(*bucket)
	bucket.lastRefill = time.Now().Add(-1 * time.Second)

	assert.True(t, rl.Allow("client-y"))
}

func TestBucketHeap_Less(t *testing.T) {
	now := time.Now()
	h := &bucketHeap{}

	entry1 := &bucketEntry{ip: "1", lastAccess: now.Add(-2 * time.Minute)}
	entry2 := &bucketEntry{ip: "2", lastAccess: now}

	*h = append(*h, entry1, entry2)

	assert.True(t, h.Less(0, 1))
	assert.False(t, h.Less(1, 0))
}

func TestGatewayPlugin_Stop_NoServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewGatewayPlugin(cfg, zap.NewNop())

	err := plugin.Stop(context.Background())
	require.NoError(t, err)
}

func TestGatewayPlugin_Init_RegistersAlertRules(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewGatewayPlugin(cfg, zap.NewNop())

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, plugin.alertManager)
}

func TestRateLimiter_Allow_MultipleClients_ExceedsBurst(t *testing.T) {
	rl := NewRateLimiter(1)
	defer rl.Stop()

	assert.True(t, rl.Allow("client-a"))
	assert.False(t, rl.Allow("client-a"))
	assert.True(t, rl.Allow("client-b"))
	assert.False(t, rl.Allow("client-b"))
}

func TestRateLimiter_Allow_RefillAllowsMore(t *testing.T) {
	rl := NewRateLimiter(1000)
	defer rl.Stop()

	assert.True(t, rl.Allow("client-z"))
	assert.True(t, rl.Allow("client-z"))

	b, ok := rl.buckets.Load("client-z")
	require.True(t, ok)
	bucket := b.(*bucket)

	rl.mu.Lock()
	bucket.lastRefill = time.Now().Add(-10 * time.Second)
	rl.mu.Unlock()

	assert.True(t, rl.Allow("client-z"))
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(10)
	defer rl.Stop()

	assert.True(t, rl.Allow("old-client"))

	rl.cleanupMu.Lock()
	for _, e := range *rl.bucketHeap {
		if e.ip == "old-client" {
			e.lastAccess = time.Now().Add(-1 * time.Hour)
		}
	}
	rl.cleanupMu.Unlock()

	rl.cleanup()

	_, exists := rl.buckets.Load("old-client")
	assert.False(t, exists)
}

func TestBucketHeap_FullCycle(t *testing.T) {
	h := &bucketHeap{}
	now := time.Now()

	entries := []*bucketEntry{
		{ip: "a", lastAccess: now.Add(-2 * time.Minute)},
		{ip: "b", lastAccess: now.Add(-1 * time.Minute)},
		{ip: "c", lastAccess: now},
	}

	for _, e := range entries {
		h.Push(e)
	}

	assert.Equal(t, 3, h.Len())

	popped := h.Pop()
	assert.NotNil(t, popped)
	assert.Equal(t, 2, h.Len())
}

func TestGatewayPlugin_Health_Started(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewGatewayPlugin(cfg, zap.NewNop())
	plugin.server = &http.Server{}

	err := plugin.Health(context.Background())
	require.NoError(t, err)
}

func TestGatewayPlugin_Stop_WithGRPC(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewGatewayPlugin(cfg, zap.NewNop())
	plugin.grpcServer = grpc.NewServer()
	healthCtx, healthCancel := context.WithCancel(context.Background())
	plugin.healthCtx = healthCtx
	plugin.healthCancel = healthCancel
	plugin.server = &http.Server{}
	plugin.resources = &gateway.AppResources{}

	err := plugin.Stop(context.Background())
	require.NoError(t, err)
}

func TestGatewayPlugin_RegisterAlertHandlers_Triggers(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewGatewayPlugin(cfg, zap.NewNop())

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)
	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	// high-error-rate rule: error_rate > 0.1
	plugin.alertManager.CheckMetric("error_rate", 0.5)

	alerts := plugin.alertManager.GetActiveAlerts()
	assert.NotEmpty(t, alerts)
}

func TestRateLimiter_Cleanup_WithRemaining(t *testing.T) {
	rl := NewRateLimiter(10)
	defer rl.Stop()

	rl.Allow("old-client")
	rl.Allow("new-client")

	rl.cleanupMu.Lock()
	for _, e := range *rl.bucketHeap {
		if e.ip == "old-client" {
			e.lastAccess = time.Now().Add(-1 * time.Hour)
		}
	}
	rl.cleanupMu.Unlock()

	rl.cleanup()

	_, exists := rl.buckets.Load("old-client")
	assert.False(t, exists, "old-client should be cleaned up")
	_, exists = rl.buckets.Load("new-client")
	assert.True(t, exists, "new-client should remain")
}

func TestRateLimiter_CleanupLoop_TickerPath(t *testing.T) {
	rl := NewRateLimiter(10)

	rl.Allow("old-client")
	rl.cleanupMu.Lock()
	for _, e := range *rl.bucketHeap {
		if e.ip == "old-client" {
			e.lastAccess = time.Now().Add(-1 * time.Hour)
		}
	}
	rl.cleanupMu.Unlock()

	rl.Stop()

	rl.cleanup()

	_, exists := rl.buckets.Load("old-client")
	assert.False(t, exists, "old-client should be cleaned up")
}

package web3

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRPCRateLimiter_TryWait(t *testing.T) {
	rl := NewRPCRateLimiter(10, 5, zap.NewNop())

	// Should be able to acquire up to burst (5) tokens immediately
	for i := 0; i < 5; i++ {
		assert.True(t, rl.TryWait(), "should acquire token %d", i+1)
	}

	// 6th should fail (bucket empty)
	assert.False(t, rl.TryWait(), "should not acquire token beyond burst")
}

func TestRPCRateLimiter_Wait(t *testing.T) {
	rl := NewRPCRateLimiter(1000, 3, zap.NewNop()) // 1000 req/s, burst 3

	// Drain the bucket
	for i := 0; i < 3; i++ {
		require.True(t, rl.TryWait())
	}

	// Wait should succeed quickly (1ms at 1000 req/s rate)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := rl.Wait(ctx)
	require.NoError(t, err, "Wait should succeed within timeout")
}

func TestRPCRateLimiter_WaitContextCancel(t *testing.T) {
	rl := NewRPCRateLimiter(1, 1, zap.NewNop()) // very slow: 1 req/s, burst 1

	// Drain the bucket
	require.True(t, rl.TryWait())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := rl.Wait(ctx)
	assert.Error(t, err, "Wait should fail when context cancelled")
	assert.Contains(t, err.Error(), "rate limiter wait cancelled")
}

func TestRPCRateLimiter_Refill(t *testing.T) {
	rl := NewRPCRateLimiter(100, 5, zap.NewNop()) // 100 req/s, burst 5

	// Drain the bucket
	for i := 0; i < 5; i++ {
		require.True(t, rl.TryWait())
	}
	assert.False(t, rl.TryWait())

	// Wait for some refill (50ms = ~5 tokens at 100/s)
	time.Sleep(60 * time.Millisecond)

	// Should have tokens again
	assert.True(t, rl.TryWait())
}

func TestRPCRateLimiter_Tokens(t *testing.T) {
	rl := NewRPCRateLimiter(10, 20, zap.NewNop())

	// After creation, should have burst tokens (may have slight refill)
	tokens := rl.Tokens()
	assert.GreaterOrEqual(t, tokens, 20.0)

	// After consuming one
	rl.TryWait()
	tokens = rl.Tokens()
	assert.GreaterOrEqual(t, tokens, 19.0)
	assert.Less(t, tokens, 20.0, "should have consumed at least one token")
}

func TestRPCRateLimiter_Defaults(t *testing.T) {
	rl := NewRPCRateLimiter(0, 0, zap.NewNop()) // should use defaults
	assert.Equal(t, 10.0, rl.rate)
	assert.Equal(t, 20.0, rl.maxTokens)
}

func TestRateLimiterConfig_Default(t *testing.T) {
	cfg := DefaultRateLimiterConfig()
	assert.False(t, cfg.Enabled)
	assert.Equal(t, 10.0, cfg.Rate)
	assert.Equal(t, 20.0, cfg.Burst)
}

func TestNewRateLimiterFromConfig_Disabled(t *testing.T) {
	cfg := RateLimiterConfig{Enabled: false}
	rl := NewRateLimiterFromConfig(cfg, zap.NewNop())
	assert.Nil(t, rl)
}

func TestNewRateLimiterFromConfig_Enabled(t *testing.T) {
	cfg := RateLimiterConfig{Enabled: true, Rate: 50, Burst: 100}
	rl := NewRateLimiterFromConfig(cfg, zap.NewNop())
	require.NotNil(t, rl)
	assert.Equal(t, 50.0, rl.rate)
	assert.Equal(t, 100.0, rl.maxTokens)
}

func TestChainClient_SetRateLimiter(t *testing.T) {
	server := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":    chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x1"} },
	})
	defer server.Close()

	client, err := NewChainClientWithFallback([]string{server.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	// No rate limiter by default
	client.mu.RLock()
	rl := client.rateLimiter
	client.mu.RUnlock()
	assert.Nil(t, rl)

	// Set rate limiter
	limiter := NewRPCRateLimiter(100, 50, zap.NewNop())
	client.SetRateLimiter(limiter)

	client.mu.RLock()
	rl = client.rateLimiter
	client.mu.RUnlock()
	assert.NotNil(t, rl)

	// RPC call should still work with rate limiter
	blockNumber, err := client.GetBlockNumber(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), blockNumber)
}

func TestChainClient_RateLimiterThrottles(t *testing.T) {
	var callCount int64

	server := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(11155111),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			atomic.AddInt64(&callCount, 1)
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x1"}
		},
	})
	defer server.Close()

	client, err := NewChainClientWithFallback([]string{server.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	// Set a very restrictive rate limiter: 5 req/s, burst 3
	limiter := NewRPCRateLimiter(5, 3, zap.NewNop())
	client.SetRateLimiter(limiter)

	// First 3 calls should succeed (burst)
	for i := 0; i < 3; i++ {
		_, err := client.GetBlockNumber(context.Background())
		require.NoError(t, err)
	}
	assert.Equal(t, int64(3), atomic.LoadInt64(&callCount))
}

package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/rtcdance/streamgate/pkg/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupRedisCache(t *testing.T) (*RedisCache, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = rc.Close()
		mr.Close()
	})

	return rc, mr
}

func TestNewRedisCache(t *testing.T) {
	rc := NewRedisCache()
	assert.NotNil(t, rc)
	assert.Nil(t, rc.client)
}

func TestRedisCache_Connect_Success(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)
	defer rc.Close()

	assert.NotNil(t, rc.client)
}

func TestRedisCache_Connect_Failure(t *testing.T) {
	rc := NewRedisCache()
	err := rc.Connect(RedisConfig{Addr: "localhost:0"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis connect failed")
}

func TestRedisCache_Connect_WithTLS(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{
		Addr:       mr.Addr(),
		TLSEnabled: true,
	})
	assert.Error(t, err)
}

func TestRedisCache_SetCircuitBreaker(t *testing.T) {
	rc := NewRedisCache()
	assert.NotPanics(t, func() { rc.SetCircuitBreaker(nil) })
}

func TestRedisCache_Get_NotConnected(t *testing.T) {
	rc := NewRedisCache()
	_, err := rc.Get(context.Background(), "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis not connected")
}

func TestRedisCache_Set_NotConnected(t *testing.T) {
	rc := NewRedisCache()
	err := rc.Set(context.Background(), "key", "value")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis not connected")
}

func TestRedisCache_SetWithExpiration_NotConnected(t *testing.T) {
	rc := NewRedisCache()
	err := rc.SetWithExpiration(context.Background(), "key", "value", time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis not connected")
}

func TestRedisCache_Delete_NotConnected(t *testing.T) {
	rc := NewRedisCache()
	err := rc.Delete(context.Background(), "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis not connected")
}

func TestRedisCache_Exists_NotConnected(t *testing.T) {
	rc := NewRedisCache()
	_, err := rc.Exists(context.Background(), "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis not connected")
}

func TestRedisCache_Expire_NotConnected(t *testing.T) {
	rc := NewRedisCache()
	err := rc.Expire(context.Background(), "key", time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis not connected")
}

func TestRedisCache_Ping_NotConnected(t *testing.T) {
	rc := NewRedisCache()
	err := rc.Ping(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis not connected")
}

func TestRedisCache_Close_NotConnected(t *testing.T) {
	rc := NewRedisCache()
	err := rc.Close()
	assert.NoError(t, err)
}

func TestRedisCache_SetAndGet(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	err := rc.Set(ctx, "test-key", "test-value")
	require.NoError(t, err)

	val, err := rc.Get(ctx, "test-key")
	require.NoError(t, err)
	assert.Equal(t, "test-value", val)
}

func TestRedisCache_Get_KeyNotFound(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	_, err := rc.Get(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestRedisCache_SetWithExpiration(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)
	defer rc.Close()

	ctx := context.Background()
	err = rc.SetWithExpiration(ctx, "exp-key", "exp-value", 100*time.Millisecond)
	require.NoError(t, err)

	val, err := rc.Get(ctx, "exp-key")
	require.NoError(t, err)
	assert.Equal(t, "exp-value", val)

	mr.FastForward(150 * time.Millisecond)

	_, err = rc.Get(ctx, "exp-key")
	assert.Error(t, err)
}

func TestRedisCache_Delete(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	err := rc.Set(ctx, "del-key", "del-value")
	require.NoError(t, err)

	err = rc.Delete(ctx, "del-key")
	require.NoError(t, err)

	_, err = rc.Get(ctx, "del-key")
	assert.Error(t, err)
}

func TestRedisCache_Exists_True(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	err := rc.Set(ctx, "exist-key", "exist-value")
	require.NoError(t, err)

	exists, err := rc.Exists(ctx, "exist-key")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestRedisCache_Exists_False(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	exists, err := rc.Exists(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRedisCache_Expire(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	err := rc.Set(ctx, "expire-key", "expire-value")
	require.NoError(t, err)

	err = rc.Expire(ctx, "expire-key", 50*time.Millisecond)
	require.NoError(t, err)
}

func TestRedisCache_Ping_Success(t *testing.T) {
	rc, _ := setupRedisCache(t)
	err := rc.Ping(context.Background())
	assert.NoError(t, err)
}

func newRedisOpenCircuitBreaker() *resilience.CircuitBreaker {
	cb := resilience.NewCircuitBreaker("test", resilience.CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          30 * time.Second,
	}, zap.NewNop())
	cb.RecordFailure()
	return cb
}

func TestRedisCache_CircuitBreaker_Open_BlocksGet(t *testing.T) {
	rc, _ := setupRedisCache(t)
	rc.SetCircuitBreaker(newRedisOpenCircuitBreaker())

	_, err := rc.Get(context.Background(), "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestRedisCache_CircuitBreaker_Open_BlocksSet(t *testing.T) {
	rc, _ := setupRedisCache(t)
	rc.SetCircuitBreaker(newRedisOpenCircuitBreaker())

	err := rc.Set(context.Background(), "key", "value")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestRedisCache_CircuitBreaker_Open_BlocksDelete(t *testing.T) {
	rc, _ := setupRedisCache(t)
	rc.SetCircuitBreaker(newRedisOpenCircuitBreaker())

	err := rc.Delete(context.Background(), "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestRedisCache_CircuitBreaker_Open_BlocksExists(t *testing.T) {
	rc, _ := setupRedisCache(t)
	rc.SetCircuitBreaker(newRedisOpenCircuitBreaker())

	_, err := rc.Exists(context.Background(), "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestRedisCache_CircuitBreaker_Open_BlocksExpire(t *testing.T) {
	rc, _ := setupRedisCache(t)
	rc.SetCircuitBreaker(newRedisOpenCircuitBreaker())

	err := rc.Expire(context.Background(), "key", time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestRedisCache_CircuitBreaker_RecordsSuccess(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	err := rc.Set(ctx, "cb-key", "cb-value")
	require.NoError(t, err)

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.RequestCount, 1)
}

func TestRedisCache_CircuitBreaker_RecordsFailure(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	mr.Close()

	_ = rc.Set(context.Background(), "cb-key", "cb-value")

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.FailureCount, 1)
}

func TestRedisCache_CircuitBreaker_GetRecordsSuccess(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	_ = rc.Set(ctx, "cb-key", "cb-value")

	_, err := rc.Get(ctx, "cb-key")
	require.NoError(t, err)

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.RequestCount, 2)
}

func TestRedisCache_CircuitBreaker_GetKeyNotFoundRecordsSuccess(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	_, _ = rc.Get(ctx, "nonexistent")

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.RequestCount, 1)
}

func TestRedisCache_Set_DelegatesToSetWithExpiration(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	err := rc.Set(ctx, "delegate-key", "delegate-value")
	require.NoError(t, err)

	val, err := rc.Get(ctx, "delegate-key")
	require.NoError(t, err)
	assert.Equal(t, "delegate-value", val)
}

func TestRedisCache_Close_WithClient(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)

	err = rc.Close()
	assert.NoError(t, err)
}

func TestRedisCache_Ping_AfterClose(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)

	rc.Close()

	err = rc.Ping(context.Background())
	assert.Error(t, err)
}

func TestRedisCache_Connect_WithPassword(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	mr.RequireAuth("secret")

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{
		Addr:     mr.Addr(),
		Password: "secret",
	})
	require.NoError(t, err)
	rc.Close()
}

func TestRedisCache_Connect_WithDB(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{
		Addr: mr.Addr(),
		DB:   1,
	})
	require.NoError(t, err)
	rc.Close()
}

func TestRedisCache_ImplementsCacheInterface(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)
	defer rc.Close()

	var _ Cache = rc
}

func TestRedisCache_Get_AfterRedisRestart(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)

	ctx := context.Background()
	err = rc.Set(ctx, "persist-key", "persist-value")
	require.NoError(t, err)

	mr.Close()

	_, err = rc.Get(ctx, "persist-key")
	assert.Error(t, err)
}

func TestRedisCache_Delete_NonexistentKey(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	err := rc.Delete(ctx, "nonexistent")
	assert.NoError(t, err)
}

func TestRedisCache_Expire_NonexistentKey(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	err := rc.Expire(ctx, "nonexistent", time.Minute)
	assert.NoError(t, err)
}

func TestRedisCache_MultipleOperations(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		val := fmt.Sprintf("value-%d", i)
		err := rc.Set(ctx, key, val)
		require.NoError(t, err)
	}

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		expectedVal := fmt.Sprintf("value-%d", i)
		val, err := rc.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, expectedVal, val)
	}
}

func TestRedisCache_CircuitBreaker_DeleteRecordsSuccess(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	_ = rc.Set(ctx, "del-key", "del-value")
	err := rc.Delete(ctx, "del-key")
	require.NoError(t, err)

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.RequestCount, 1)
}

func TestRedisCache_CircuitBreaker_ExistsRecordsSuccess(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	_ = rc.Set(ctx, "exist-key", "exist-value")
	_, err := rc.Exists(ctx, "exist-key")
	require.NoError(t, err)

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.RequestCount, 1)
}

func TestRedisCache_CircuitBreaker_ExpireRecordsSuccess(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	_ = rc.Set(ctx, "expire-key", "expire-value")
	err := rc.Expire(ctx, "expire-key", time.Minute)
	require.NoError(t, err)

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.RequestCount, 1)
}

func TestRedisCache_CircuitBreaker_DeleteRecordsFailure(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	mr.Close()

	_ = rc.Delete(context.Background(), "key")

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.FailureCount, 1)
}

func TestRedisCache_CircuitBreaker_ExistsRecordsFailure(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	mr.Close()

	_, _ = rc.Exists(context.Background(), "key")

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.FailureCount, 1)
}

func TestRedisCache_CircuitBreaker_ExpireRecordsFailure(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	rc := NewRedisCache()
	err = rc.Connect(RedisConfig{Addr: mr.Addr()})
	require.NoError(t, err)

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	mr.Close()

	_ = rc.Expire(context.Background(), "key", time.Minute)

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.FailureCount, 1)
}

func TestRedisCache_Constants(t *testing.T) {
	assert.Equal(t, 3, redisMaxRetries)
	assert.Equal(t, time.Second, redisRetryBackoff)
}

func TestRedisCache_SetWithExpiration_CircuitBreakerOpen(t *testing.T) {
	rc, _ := setupRedisCache(t)
	rc.SetCircuitBreaker(newRedisOpenCircuitBreaker())

	err := rc.SetWithExpiration(context.Background(), "key", "value", time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestRedisCache_Get_CircuitBreakerRecordsSuccessOnMiss(t *testing.T) {
	rc, _ := setupRedisCache(t)
	ctx := context.Background()

	cb := resilience.NewCircuitBreaker("test", resilience.DefaultCircuitBreakerConfig(), zap.NewNop())
	rc.SetCircuitBreaker(cb)

	_, _ = rc.Get(ctx, "nonexistent")

	stats := cb.Stats()
	assert.GreaterOrEqual(t, stats.RequestCount, 1)
}

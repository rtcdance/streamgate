package unit_test

import (
	"context"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/require"
)

func TestRedisCache_Connect(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return // Test skipped
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Test that connection is established
	err := cache.Ping(context.Background())
	require.NoError(t, err)
}

func TestRedisCache_SetGet(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value
	err := cache.Set(context.Background(), "test_key", "test_value")
	require.NoError(t, err)

	// Get value
	value, err := cache.Get(context.Background(), "test_key")
	require.NoError(t, err)
	require.Equal(t, "test_value", value)

	// Cleanup
	_ = cache.Delete(context.Background(), "test_key")
}

func TestRedisCache_SetWithExpiration(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value with expiration
	err := cache.SetWithExpiration(context.Background(), "test_exp_key", "test_value", 1*time.Second)
	require.NoError(t, err)

	// Get value immediately
	value, err := cache.Get(context.Background(), "test_exp_key")
	require.NoError(t, err)
	require.Equal(t, "test_value", value)

	// Wait for expiration
	time.Sleep(2 * time.Second)

	// Value should be expired
	_, err = cache.Get(context.Background(), "test_exp_key")
	require.Error(t, err)
}

func TestRedisCache_Delete(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value
	err := cache.Set(context.Background(), "test_del_key", "test_value")
	require.NoError(t, err)

	// Delete value
	err = cache.Delete(context.Background(), "test_del_key")
	require.NoError(t, err)

	// Value should not exist
	_, err = cache.Get(context.Background(), "test_del_key")
	require.Error(t, err)
}

func TestRedisCache_Exists(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value
	err := cache.Set(context.Background(), "test_exists_key", "test_value")
	require.NoError(t, err)

	// Check exists
	exists, err := cache.Exists(context.Background(), "test_exists_key")
	require.NoError(t, err)
	require.True(t, exists)

	// Delete and check again
	_ = cache.Delete(context.Background(), "test_exists_key")
	exists, err = cache.Exists(context.Background(), "test_exists_key")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestRedisCache_Expire(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value without expiration
	err := cache.Set(context.Background(), "test_expire_key", "test_value")
	require.NoError(t, err)

	// Set expiration
	err = cache.Expire(context.Background(), "test_expire_key", 1*time.Second)
	require.NoError(t, err)

	// Value should exist immediately
	value, err := cache.Get(context.Background(), "test_expire_key")
	require.NoError(t, err)
	require.Equal(t, "test_value", value)

	// Wait for expiration
	time.Sleep(2 * time.Second)

	// Value should be expired
	_, err = cache.Get(context.Background(), "test_expire_key")
	require.Error(t, err)
}

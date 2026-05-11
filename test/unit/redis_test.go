package unit_test

import (
	"context"
	"testing"
	"time"

	"streamgate/test/helpers"
)

func TestRedisCache_Connect(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return // Test skipped
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Test that connection is established
	err := cache.Ping(context.Background())
	helpers.AssertNoError(t, err)
}

func TestRedisCache_SetGet(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value
	err := cache.Set(context.Background(), "test_key", "test_value")
	helpers.AssertNoError(t, err)

	// Get value
	value, err := cache.Get(context.Background(), "test_key")
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "test_value", value)

	// Cleanup
	cache.Delete(context.Background(), "test_key")
}

func TestRedisCache_SetWithExpiration(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value with expiration
	err := cache.SetWithExpiration(context.Background(), "test_exp_key", "test_value", 1*time.Second)
	helpers.AssertNoError(t, err)

	// Get value immediately
	value, err := cache.Get(context.Background(), "test_exp_key")
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "test_value", value)

	// Wait for expiration
	time.Sleep(2 * time.Second)

	// Value should be expired
	_, err = cache.Get(context.Background(), "test_exp_key")
	helpers.AssertError(t, err)
}

func TestRedisCache_Delete(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value
	err := cache.Set(context.Background(), "test_del_key", "test_value")
	helpers.AssertNoError(t, err)

	// Delete value
	err = cache.Delete(context.Background(), "test_del_key")
	helpers.AssertNoError(t, err)

	// Value should not exist
	_, err = cache.Get(context.Background(), "test_del_key")
	helpers.AssertError(t, err)
}

func TestRedisCache_Exists(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value
	err := cache.Set(context.Background(), "test_exists_key", "test_value")
	helpers.AssertNoError(t, err)

	// Check exists
	exists, err := cache.Exists(context.Background(), "test_exists_key")
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, exists)

	// Delete and check again
	cache.Delete(context.Background(), "test_exists_key")
	exists, err = cache.Exists(context.Background(), "test_exists_key")
	helpers.AssertNoError(t, err)
	helpers.AssertFalse(t, exists)
}

func TestRedisCache_Expire(t *testing.T) {
	cache := helpers.SetupTestRedis(t)
	if cache == nil {
		return
	}
	defer helpers.CleanupTestRedis(t, cache)

	// Set value without expiration
	err := cache.Set(context.Background(), "test_expire_key", "test_value")
	helpers.AssertNoError(t, err)

	// Set expiration
	err = cache.Expire(context.Background(), "test_expire_key", 1*time.Second)
	helpers.AssertNoError(t, err)

	// Value should exist immediately
	value, err := cache.Get(context.Background(), "test_expire_key")
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "test_value", value)

	// Wait for expiration
	time.Sleep(2 * time.Second)

	// Value should be expired
	_, err = cache.Get(context.Background(), "test_expire_key")
	helpers.AssertError(t, err)
}

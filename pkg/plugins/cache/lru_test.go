package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLRU(t *testing.T) {
	lru := NewLRU(10)
	assert.NotNil(t, lru)
	assert.Equal(t, 0, lru.Len())
}

func TestLRU_SetAndGet(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Set("key2", 42)

	val, ok := lru.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)

	val, ok = lru.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, 42, val)
}

func TestLRU_Get_Miss(t *testing.T) {
	lru := NewLRU(10)

	_, ok := lru.Get("nonexistent")
	assert.False(t, ok)
}

func TestLRU_SetWithTTL_Expired(t *testing.T) {
	lru := NewLRU(10)

	lru.SetWithTTL("key1", "value1", 1*time.Nanosecond)
	time.Sleep(1 * time.Millisecond)

	_, ok := lru.Get("key1")
	assert.False(t, ok)
}

func TestLRU_SetWithTTL_NotExpired(t *testing.T) {
	lru := NewLRU(10)

	lru.SetWithTTL("key1", "value1", 1*time.Hour)

	val, ok := lru.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

func TestLRU_Eviction(t *testing.T) {
	lru := NewLRU(3)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")
	lru.Set("key3", "value3")
	lru.Set("key4", "value4")

	assert.Equal(t, 3, lru.Len())

	_, ok := lru.Get("key1")
	assert.False(t, ok)

	_, ok = lru.Get("key4")
	assert.True(t, ok)
}

func TestLRU_Eviction_LRUOrder(t *testing.T) {
	lru := NewLRU(3)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")
	lru.Set("key3", "value3")

	lru.Get("key1")
	lru.Set("key4", "value4")

	_, ok := lru.Get("key1")
	assert.True(t, ok)

	_, ok = lru.Get("key2")
	assert.False(t, ok)
}

func TestLRU_UpdateExisting(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Set("key1", "value2")

	val, ok := lru.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value2", val)
	assert.Equal(t, 1, lru.Len())
}

func TestLRU_UpdateExisting_WithTTL(t *testing.T) {
	lru := NewLRU(10)

	lru.SetWithTTL("key1", "value1", 1*time.Hour)
	lru.SetWithTTL("key1", "value2", 1*time.Hour)

	val, ok := lru.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value2", val)
}

func TestLRU_Delete(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Delete("key1")

	_, ok := lru.Get("key1")
	assert.False(t, ok)
}

func TestLRU_Delete_NonExistent(t *testing.T) {
	lru := NewLRU(10)
	lru.Delete("nonexistent")
}

func TestLRU_Clear(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")
	lru.Clear()

	assert.Equal(t, 0, lru.Len())
}

func TestLRU_Len(t *testing.T) {
	lru := NewLRU(10)

	assert.Equal(t, 0, lru.Len())
	lru.Set("key1", "value1")
	assert.Equal(t, 1, lru.Len())
	lru.Set("key2", "value2")
	assert.Equal(t, 2, lru.Len())
}

func TestLRU_Keys(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")

	keys := lru.Keys()
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "key1")
	assert.Contains(t, keys, "key2")
}

func TestLRU_Size(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "hello")
	assert.Equal(t, 5, lru.Size())
}

func TestLRU_GetStats(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Get("key1")
	lru.Get("nonexistent")

	stats := lru.GetStats()
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
	assert.Equal(t, int64(1), stats.Sets)
	assert.Equal(t, int64(2), stats.Gets)
}

func TestLRU_GetHitRate(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Get("key1")
	lru.Get("key1")
	lru.Get("nonexistent")

	rate := lru.GetHitRate()
	assert.InDelta(t, 0.667, rate, 0.01)
}

func TestLRU_GetHitRate_NoAccesses(t *testing.T) {
	lru := NewLRU(10)
	rate := lru.GetHitRate()
	assert.Equal(t, 0.0, rate)
}

func TestLRU_SetOnEvict(t *testing.T) {
	lru := NewLRU(2)

	evictedKey := ""
	evictedValue := ""
	lru.SetOnEvict(func(key string, value interface{}) {
		evictedKey = key
		evictedValue = value.(string)
	})

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")
	lru.Set("key3", "value3")

	assert.Equal(t, "key1", evictedKey)
	assert.Equal(t, "value1", evictedValue)
}

func TestLRU_GetOldest(t *testing.T) {
	lru := NewLRU(10)

	_, _, ok := lru.GetOldest()
	assert.False(t, ok)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")

	key, _, ok := lru.GetOldest()
	assert.True(t, ok)
	assert.Equal(t, "key1", key)
}

func TestLRU_GetNewest(t *testing.T) {
	lru := NewLRU(10)

	_, _, ok := lru.GetNewest()
	assert.False(t, ok)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")

	key, _, ok := lru.GetNewest()
	assert.True(t, ok)
	assert.Equal(t, "key2", key)
}

func TestLRU_Peek(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")

	lru.Get("key2")

	val, ok := lru.Peek("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

func TestLRU_Peek_Expired(t *testing.T) {
	lru := NewLRU(10)

	lru.SetWithTTL("key1", "value1", 1*time.Nanosecond)
	time.Sleep(1 * time.Millisecond)

	_, ok := lru.Peek("key1")
	assert.False(t, ok)
}

func TestLRU_Peek_NonExistent(t *testing.T) {
	lru := NewLRU(10)
	_, ok := lru.Peek("nonexistent")
	assert.False(t, ok)
}

func TestLRU_Contains(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	assert.True(t, lru.Contains("key1"))
	assert.False(t, lru.Contains("nonexistent"))
}

func TestLRU_Resize_Shrink(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")
	lru.Set("key3", "value3")

	lru.Resize(2)
	assert.Equal(t, 2, lru.Len())
}

func TestLRU_Resize_Grow(t *testing.T) {
	lru := NewLRU(2)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")
	lru.Resize(10)

	lru.Set("key3", "value3")
	assert.Equal(t, 3, lru.Len())
}

func TestLRU_GetItem(t *testing.T) {
	lru := NewLRU(10)

	lru.SetWithTTL("key1", "value1", 1*time.Hour)

	item, ok := lru.GetItem("key1")
	require.True(t, ok)
	assert.Equal(t, "key1", item.Key)
	assert.Equal(t, "value1", item.Value)
	assert.False(t, item.ExpiresAt.IsZero())
}

func TestLRU_GetItem_Expired(t *testing.T) {
	lru := NewLRU(10)

	lru.SetWithTTL("key1", "value1", 1*time.Nanosecond)
	time.Sleep(1 * time.Millisecond)

	_, ok := lru.GetItem("key1")
	assert.False(t, ok)
}

func TestLRU_CleanupExpired(t *testing.T) {
	lru := NewLRU(10)

	lru.SetWithTTL("key1", "value1", 1*time.Nanosecond)
	lru.SetWithTTL("key2", "value2", 1*time.Hour)
	lru.SetWithTTL("key3", "value3", 1*time.Nanosecond)

	time.Sleep(1 * time.Millisecond)

	expired := lru.CleanupExpired()
	assert.Equal(t, 2, expired)
	assert.Equal(t, 1, lru.Len())

	_, ok := lru.Get("key2")
	assert.True(t, ok)
}

func TestLRU_GetTopKeys(t *testing.T) {
	lru := NewLRU(10)

	lru.Set("key1", "value1")
	lru.Set("key2", "value2")
	lru.Set("key3", "value3")

	lru.Get("key1")
	lru.Get("key1")
	lru.Get("key1")
	lru.Get("key2")
	lru.Get("key2")

	topKeys := lru.GetTopKeys(2)
	assert.Len(t, topKeys, 2)
	assert.Equal(t, "key1", topKeys[0])
	assert.Equal(t, "key2", topKeys[1])
}

func TestLRU_ConcurrentAccess(t *testing.T) {
	lru := NewLRU(100)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			lru.Set("key", idx)
		}(i)
		go func(idx int) {
			defer wg.Done()
			lru.Get("key")
		}(i)
	}
	wg.Wait()
}

func TestEstimateSize(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected int
	}{
		{"string", "hello", 5},
		{"bytes", []byte("hello"), 5},
		{"int", 42, 8},
		{"float64", 3.14, 8},
		{"bool", true, 1},
		{"complex", map[string]int{"a": 1}, 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := estimateSize(tc.value)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestTTLCache_SetAndGet(t *testing.T) {
	cache := NewTTLCache()

	cache.Set("key1", "value1", 1*time.Hour)

	val, ok := cache.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

func TestTTLCache_Get_Expired(t *testing.T) {
	cache := NewTTLCache()

	cache.Set("key1", "value1", 1*time.Nanosecond)
	time.Sleep(1 * time.Millisecond)

	_, ok := cache.Get("key1")
	assert.False(t, ok)
}

func TestTTLCache_Get_Miss(t *testing.T) {
	cache := NewTTLCache()
	_, ok := cache.Get("nonexistent")
	assert.False(t, ok)
}

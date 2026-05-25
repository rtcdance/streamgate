package storage

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCacheStorage(t *testing.T) {
	cs := NewCacheStorage(100)
	assert.NotNil(t, cs)
	cs.Close()
}

func TestCacheStorage_SetAndGet(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.Set("key1", "value1")
	require.NoError(t, err)

	val, err := cs.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)
}

func TestCacheStorage_Get_NotFound(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	_, err := cs.Get("missing")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestCacheStorage_SetWithExpiration_Expired(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.SetWithExpiration("key1", "value1", 1*time.Nanosecond)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	_, err = cs.Get("key1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key expired")
}

func TestCacheStorage_Delete(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.Set("key1", "value1")
	require.NoError(t, err)

	err = cs.Delete("key1")
	require.NoError(t, err)

	_, err = cs.Get("key1")
	assert.Error(t, err)
}

func TestCacheStorage_Exists(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	assert.False(t, cs.Exists("key1"))

	err := cs.Set("key1", "value1")
	require.NoError(t, err)
	assert.True(t, cs.Exists("key1"))
}

func TestCacheStorage_Exists_Expired(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.SetWithExpiration("key1", "value1", 1*time.Nanosecond)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	assert.False(t, cs.Exists("key1"))
}

func TestCacheStorage_GetString(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.Set("key1", "hello")
	require.NoError(t, err)

	val, err := cs.GetString("key1")
	require.NoError(t, err)
	assert.Equal(t, "hello", val)
}

func TestCacheStorage_GetString_NotString(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.Set("key1", 123)
	require.NoError(t, err)

	_, err = cs.GetString("key1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a string")
}

func TestCacheStorage_GetBytes(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.Set("key1", []byte("data"))
	require.NoError(t, err)

	val, err := cs.GetBytes("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), val)
}

func TestCacheStorage_GetBytes_NotBytes(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.Set("key1", "string")
	require.NoError(t, err)

	_, err = cs.GetBytes("key1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a byte slice")
}

func TestCacheStorage_Clear(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.Set("key1", "value1")
	require.NoError(t, err)
	err = cs.Set("key2", "value2")
	require.NoError(t, err)

	err = cs.Clear()
	require.NoError(t, err)

	assert.Equal(t, 0, cs.Size())
}

func TestCacheStorage_Size(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	assert.Equal(t, 0, cs.Size())

	_ = cs.Set("key1", "value1")
	assert.Equal(t, 1, cs.Size())

	_ = cs.Set("key2", "value2")
	assert.Equal(t, 2, cs.Size())
}

func TestCacheStorage_LRU_Eviction(t *testing.T) {
	cs := NewCacheStorage(3)
	defer cs.Close()

	_ = cs.Set("key1", "value1")
	_ = cs.Set("key2", "value2")
	_ = cs.Set("key3", "value3")
	_ = cs.Set("key4", "value4")

	assert.True(t, cs.Size() <= 3)
}

func TestCacheStorage_SetJSON(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	data := map[string]string{"name": "test"}
	err := cs.SetJSON("key1", data, time.Hour)
	require.NoError(t, err)

	raw, err := cs.GetBytes("key1")
	require.NoError(t, err)

	var result map[string]string
	err = json.Unmarshal(raw, &result)
	require.NoError(t, err)
	assert.Equal(t, "test", result["name"])
}

func TestCacheStorage_GetJSON(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	data := map[string]string{"name": "test"}
	err := cs.SetJSON("key1", data, time.Hour)
	require.NoError(t, err)

	var result map[string]string
	err = cs.GetJSON("key1", &result)
	require.NoError(t, err)
	assert.Equal(t, "test", result["name"])
}

func TestCacheStorage_GetJSON_NotFound(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	var result map[string]string
	err := cs.GetJSON("missing", &result)
	assert.Error(t, err)
}

func TestCacheStorage_SetJSON_MarshalError(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.SetJSON("key", make(chan int), time.Hour)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal JSON")
}

func TestCacheStorage_GetJSON_UnmarshalError(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.Set("key", []byte("not-json-bytes"))
	require.NoError(t, err)

	var result map[string]string
	err = cs.GetJSON("key", &result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal JSON")
}

func TestCacheStorage_GetBytes_NotFound(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	_, err := cs.GetBytes("missing")
	assert.Error(t, err)
}

func TestCacheStorage_GetString_NotFound(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	_, err := cs.GetString("missing")
	assert.Error(t, err)
}

func TestCacheStorage_LRU_Eviction_SmallMaxSize(t *testing.T) {
	cs := NewCacheStorage(1)
	defer cs.Close()

	_ = cs.Set("key1", "value1")
	_ = cs.Set("key2", "value2")

	assert.Equal(t, 1, cs.Size())
}

func TestCacheStorage_DoubleClose(t *testing.T) {
	cs := NewCacheStorage(100)
	cs.Close()
	cs.Close()
}

func TestCacheStorage_Delete_Nonexistent(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.Delete("nonexistent")
	assert.NoError(t, err)
}

func TestCacheStorage_SetWithExpiration_ZeroTTL(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	err := cs.SetWithExpiration("key1", "value1", 0)
	require.NoError(t, err)

	val, err := cs.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)
}

func TestCacheStorage_ConcurrentAccess(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", id)
			_ = cs.Set(key, fmt.Sprintf("value-%d", id))
			_, _ = cs.Get(key)
			_ = cs.Exists(key)
		}(i)
	}
	wg.Wait()
}



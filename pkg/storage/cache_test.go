package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCacheStorage(t *testing.T) {
	cs := NewCacheStorage(100)
	assert.NotNil(t, cs)
	assert.Equal(t, 0, cs.Size())
}

func TestCacheStorage_Get(t *testing.T) {
	cs := NewCacheStorage(100)

	t.Run("get existing key", func(t *testing.T) {
		err := cs.Set("key1", "value1")
		require.NoError(t, err)

		val, err := cs.Get("key1")
		require.NoError(t, err)
		assert.Equal(t, "value1", val)
	})

	t.Run("get non-existing key", func(t *testing.T) {
		_, err := cs.Get("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("get expired key", func(t *testing.T) {
		err := cs.SetWithExpiration("expired", "value", 10*time.Millisecond)
		require.NoError(t, err)

		time.Sleep(20 * time.Millisecond)

		_, err = cs.Get("expired")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "key expired")
	})
}

func TestCacheStorage_GetString(t *testing.T) {
	cs := NewCacheStorage(100)

	t.Run("get string value", func(t *testing.T) {
		err := cs.Set("strkey", "stringvalue")
		require.NoError(t, err)

		val, err := cs.GetString("strkey")
		require.NoError(t, err)
		assert.Equal(t, "stringvalue", val)
	})

	t.Run("get non-string value", func(t *testing.T) {
		err := cs.Set("intkey", 123)
		require.NoError(t, err)

		_, err = cs.GetString("intkey")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "value is not a string")
	})

	t.Run("get non-existing key", func(t *testing.T) {
		_, err := cs.GetString("nonexistent")
		assert.Error(t, err)
	})
}

func TestCacheStorage_GetBytes(t *testing.T) {
	cs := NewCacheStorage(100)

	t.Run("get bytes value", func(t *testing.T) {
		data := []byte("test data")
		err := cs.Set("byteskey", data)
		require.NoError(t, err)

		val, err := cs.GetBytes("byteskey")
		require.NoError(t, err)
		assert.Equal(t, data, val)
	})

	t.Run("get non-bytes value", func(t *testing.T) {
		err := cs.Set("strkey", "string")
		require.NoError(t, err)

		_, err = cs.GetBytes("strkey")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "value is not a byte slice")
	})

	t.Run("get non-existing key", func(t *testing.T) {
		_, err := cs.GetBytes("nonexistent")
		assert.Error(t, err)
	})
}

func TestCacheStorage_Set(t *testing.T) {
	cs := NewCacheStorage(100)

	err := cs.Set("key", "value")
	require.NoError(t, err)
	assert.Equal(t, 1, cs.Size())

	val, err := cs.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "value", val)
}

func TestCacheStorage_SetWithExpiration(t *testing.T) {
	cs := NewCacheStorage(100)

	t.Run("set with expiration", func(t *testing.T) {
		err := cs.SetWithExpiration("expkey", "value", 100*time.Millisecond)
		require.NoError(t, err)

		val, err := cs.Get("expkey")
		require.NoError(t, err)
		assert.Equal(t, "value", val)

		time.Sleep(150 * time.Millisecond)

		_, err = cs.Get("expkey")
		assert.Error(t, err)
	})

	t.Run("set without expiration", func(t *testing.T) {
		err := cs.SetWithExpiration("noexp", "value", 0)
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		val, err := cs.Get("noexp")
		require.NoError(t, err)
		assert.Equal(t, "value", val)
	})
}

func TestCacheStorage_SetJSON(t *testing.T) {
	cs := NewCacheStorage(100)

	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	t.Run("set and get JSON", func(t *testing.T) {
		data := TestStruct{Name: "test", Value: 42}
		err := cs.SetJSON("jsonkey", data, 0)
		require.NoError(t, err)

		var result TestStruct
		err = cs.GetJSON("jsonkey", &result)
		require.NoError(t, err)
		assert.Equal(t, "test", result.Name)
		assert.Equal(t, 42, result.Value)
	})

	t.Run("set JSON with expiration", func(t *testing.T) {
		data := TestStruct{Name: "exp", Value: 10}
		err := cs.SetJSON("expjson", data, 50*time.Millisecond)
		require.NoError(t, err)

		var result TestStruct
		err = cs.GetJSON("expjson", &result)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		err = cs.GetJSON("expjson", &result)
		assert.Error(t, err)
	})
}

func TestCacheStorage_GetJSON(t *testing.T) {
	cs := NewCacheStorage(100)

	type TestStruct struct {
		Name string `json:"name"`
	}

	t.Run("get JSON value", func(t *testing.T) {
		data := TestStruct{Name: "test"}
		err := cs.SetJSON("jsonkey", data, 0)
		require.NoError(t, err)

		var result TestStruct
		err = cs.GetJSON("jsonkey", &result)
		require.NoError(t, err)
		assert.Equal(t, "test", result.Name)
	})

	t.Run("get non-existing key", func(t *testing.T) {
		var result TestStruct
		err := cs.GetJSON("nonexistent", &result)
		assert.Error(t, err)
	})

	t.Run("get invalid JSON", func(t *testing.T) {
		err := cs.Set("invalid", []byte("not json"))
		require.NoError(t, err)

		var result TestStruct
		err = cs.GetJSON("invalid", &result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal JSON")
	})
}

func TestCacheStorage_Delete(t *testing.T) {
	cs := NewCacheStorage(100)

	t.Run("delete existing key", func(t *testing.T) {
		err := cs.Set("key", "value")
		require.NoError(t, err)
		assert.Equal(t, 1, cs.Size())

		err = cs.Delete("key")
		require.NoError(t, err)
		assert.Equal(t, 0, cs.Size())

		_, err = cs.Get("key")
		assert.Error(t, err)
	})

	t.Run("delete non-existing key", func(t *testing.T) {
		err := cs.Delete("nonexistent")
		assert.NoError(t, err)
	})
}

func TestCacheStorage_Exists(t *testing.T) {
	cs := NewCacheStorage(100)

	t.Run("existing key", func(t *testing.T) {
		err := cs.Set("key", "value")
		require.NoError(t, err)

		assert.True(t, cs.Exists("key"))
	})

	t.Run("non-existing key", func(t *testing.T) {
		assert.False(t, cs.Exists("nonexistent"))
	})

	t.Run("expired key", func(t *testing.T) {
		err := cs.SetWithExpiration("expired", "value", 10*time.Millisecond)
		require.NoError(t, err)

		time.Sleep(20 * time.Millisecond)

		assert.False(t, cs.Exists("expired"))
	})
}

func TestCacheStorage_Clear(t *testing.T) {
	cs := NewCacheStorage(100)

	err := cs.Set("key1", "value1")
	require.NoError(t, err)
	err = cs.Set("key2", "value2")
	require.NoError(t, err)
	assert.Equal(t, 2, cs.Size())

	err = cs.Clear()
	require.NoError(t, err)
	assert.Equal(t, 0, cs.Size())

	_, err = cs.Get("key1")
	assert.Error(t, err)
	_, err = cs.Get("key2")
	assert.Error(t, err)
}

func TestCacheStorage_Size(t *testing.T) {
	cs := NewCacheStorage(100)

	assert.Equal(t, 0, cs.Size())

	cs.Set("key1", "value1")
	assert.Equal(t, 1, cs.Size())

	cs.Set("key2", "value2")
	assert.Equal(t, 2, cs.Size())

	cs.Delete("key1")
	assert.Equal(t, 1, cs.Size())
}

func TestCacheStorage_LRUEviction(t *testing.T) {
	cs := NewCacheStorage(3)

	cs.Set("key1", "value1")
	cs.Set("key2", "value2")
	cs.Set("key3", "value3")
	assert.Equal(t, 3, cs.Size())

	cs.Set("key4", "value4")
	assert.Equal(t, 3, cs.Size())

	_, err := cs.Get("key1")
	assert.Error(t, err)

	val, err := cs.Get("key2")
	require.NoError(t, err)
	assert.Equal(t, "value2", val)
}

func TestCacheStorage_UpdateLastAccess(t *testing.T) {
	cs := NewCacheStorage(100)

	cs.Set("key", "value")
	time.Sleep(10 * time.Millisecond)

	val, err := cs.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "value", val)

	cs.Set("key2", "value2")
	time.Sleep(10 * time.Millisecond)

	cs.Set("key3", "value3")

	cs.SetWithExpiration("key", "newvalue", 100*time.Millisecond)
	val, err = cs.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "newvalue", val)
}

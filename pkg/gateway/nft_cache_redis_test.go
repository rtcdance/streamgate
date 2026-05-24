package gateway

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRedisNFTAccessCache(t *testing.T) {
	local := NewNFTAccessCache()
	defer local.Stop()
	cache := NewRedisNFTAccessCache(local, nil)
	require.NotNil(t, cache)
}

func TestRedisNFTAccessCache_Get_LocalHit(t *testing.T) {
	local := NewNFTAccessCache()
	defer local.Stop()
	cache := NewRedisNFTAccessCache(local, nil)

	local.Set("key1", CachedNFTAccess{
		HasNFT:    true,
		Balance:   big.NewInt(3),
		ExpiresAt: time.Now().Add(time.Hour),
	})

	entry, ok := cache.Get(context.Background(), "key1")
	assert.True(t, ok)
	assert.True(t, entry.HasNFT)
	assert.Equal(t, big.NewInt(3), entry.Balance)
}

func TestRedisNFTAccessCache_Get_Miss(t *testing.T) {
	local := NewNFTAccessCache()
	defer local.Stop()
	cache := NewRedisNFTAccessCache(local, nil)

	_, ok := cache.Get(context.Background(), "nonexistent")
	assert.False(t, ok)
}

func TestRedisNFTAccessCache_Set(t *testing.T) {
	local := NewNFTAccessCache()
	defer local.Stop()
	cache := NewRedisNFTAccessCache(local, nil)

	cache.Set(context.Background(), "key1", middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(5),
		Expires: time.Now().Add(time.Hour),
	})

	entry, ok := local.Get("key1")
	assert.True(t, ok)
	assert.True(t, entry.HasNFT)
	assert.Equal(t, big.NewInt(5), entry.Balance)
}

func TestRedisNFTAccessCache_Delete(t *testing.T) {
	local := NewNFTAccessCache()
	defer local.Stop()
	cache := NewRedisNFTAccessCache(local, nil)

	cache.Set(context.Background(), "key1", middleware.NFTAccessEntry{
		HasNFT:  true,
		Expires: time.Now().Add(time.Hour),
	})
	cache.Delete(context.Background(), "key1")
	_, ok := local.Get("key1")
	assert.False(t, ok)
}

func TestRedisNFTAccessCache_DeleteByPrefix(t *testing.T) {
	local := NewNFTAccessCache()
	defer local.Stop()
	cache := NewRedisNFTAccessCache(local, nil)

	cache.Set(context.Background(), "1:0xA:0xB:1", middleware.NFTAccessEntry{HasNFT: true, Expires: time.Now().Add(time.Hour)})
	cache.Set(context.Background(), "1:0xA:0xB:2", middleware.NFTAccessEntry{HasNFT: false, Expires: time.Now().Add(time.Hour)})
	cache.Set(context.Background(), "2:0xC:0xD:1", middleware.NFTAccessEntry{HasNFT: true, Expires: time.Now().Add(time.Hour)})

	cache.DeleteByPrefix(context.Background(), "1:0xA:")

	_, ok := local.Get("1:0xA:0xB:1")
	assert.False(t, ok)
	_, ok = local.Get("2:0xC:0xD:1")
	assert.True(t, ok)
}

func TestToMiddlewareNFTAccessEntry(t *testing.T) {
	cached := CachedNFTAccess{
		HasNFT:      true,
		Balance:     big.NewInt(10),
		BlockNumber: 500,
		BlockHash:   "0xhash",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	entry := toMiddlewareNFTAccessEntry(cached)
	assert.True(t, entry.HasNFT)
	assert.Equal(t, big.NewInt(10), entry.Balance)
	assert.Equal(t, uint64(500), entry.BlockNumber)
	assert.Equal(t, "0xhash", entry.BlockHash)
}

func TestFromMiddlewareNFTAccessEntry(t *testing.T) {
	entry := middleware.NFTAccessEntry{
		HasNFT:      false,
		Balance:     big.NewInt(0),
		BlockNumber: 100,
		BlockHash:   "0xabc",
		Expires:     time.Now().Add(time.Hour),
	}
	cached := fromMiddlewareNFTAccessEntry(entry)
	assert.False(t, cached.HasNFT)
	assert.Equal(t, big.NewInt(0), cached.Balance)
	assert.Equal(t, uint64(100), cached.BlockNumber)
	assert.Equal(t, "0xabc", cached.BlockHash)
}

func TestRedisNFTAccessCache_Get_RedisHit(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	local := NewNFTAccessCache()
	defer local.Stop()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cache := NewRedisNFTAccessCache(local, rdb)

	entry := middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(7),
		Expires: time.Now().Add(time.Hour),
	}
	data, _ := json.Marshal(entry)
	mr.Set("nft_cache:key1", string(data))

	result, ok := cache.Get(context.Background(), "key1")
	assert.True(t, ok)
	assert.True(t, result.HasNFT)
	assert.Equal(t, big.NewInt(7), result.Balance)

	entryInLocal, localOk := local.Get("key1")
	assert.True(t, localOk)
	assert.True(t, entryInLocal.HasNFT)
}

func TestRedisNFTAccessCache_Get_RedisExpired(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	local := NewNFTAccessCache()
	defer local.Stop()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cache := NewRedisNFTAccessCache(local, rdb)

	expiredEntry := middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(7),
		Expires: time.Now().Add(-time.Hour),
	}
	data, _ := json.Marshal(expiredEntry)
	mr.Set("nft_cache:key1", string(data))

	_, ok := cache.Get(context.Background(), "key1")
	assert.False(t, ok)
}

func TestRedisNFTAccessCache_Get_RedisCorruptData(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	local := NewNFTAccessCache()
	defer local.Stop()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cache := NewRedisNFTAccessCache(local, rdb)

	mr.Set("nft_cache:key1", "not-valid-json")

	_, ok := cache.Get(context.Background(), "key1")
	assert.False(t, ok)
}

func TestRedisNFTAccessCache_Set_WithRedis(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	local := NewNFTAccessCache()
	defer local.Stop()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cache := NewRedisNFTAccessCache(local, rdb)

	entry := middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(5),
		Expires: time.Now().Add(time.Hour),
	}
	cache.Set(context.Background(), "key1", entry)

	_, ok := local.Get("key1")
	assert.True(t, ok)

	val, err := mr.Get("nft_cache:key1")
	require.NoError(t, err)
	assert.Contains(t, val, "HasNFT")
}

func TestRedisNFTAccessCache_Set_WithRedisExpiredTTL(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	local := NewNFTAccessCache()
	defer local.Stop()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cache := NewRedisNFTAccessCache(local, rdb)

	entry := middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(5),
		Expires: time.Now().Add(-time.Hour),
	}
	cache.Set(context.Background(), "key1", entry)

	_, err = mr.Get("nft_cache:key1")
	assert.Error(t, err)

	_, ok := local.Get("key1")
	assert.False(t, ok, "local cache Get also returns false for expired entries")
}

func TestRedisNFTAccessCache_Delete_WithRedis(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	local := NewNFTAccessCache()
	defer local.Stop()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cache := NewRedisNFTAccessCache(local, rdb)

	mr.Set("nft_cache:key1", "some-data")
	local.Set("key1", CachedNFTAccess{HasNFT: true, ExpiresAt: time.Now().Add(time.Hour)})

	cache.Delete(context.Background(), "key1")

	_, ok := local.Get("key1")
	assert.False(t, ok)

	_, err = mr.Get("nft_cache:key1")
	assert.Error(t, err)
}

func TestRedisNFTAccessCache_DeleteByPrefix_WithRedis(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	local := NewNFTAccessCache()
	defer local.Stop()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cache := NewRedisNFTAccessCache(local, rdb)

	mr.Set("nft_cache:1:0xA:0xB:1", `{"HasNFT":true}`)
	mr.Set("nft_cache:1:0xA:0xB:2", `{"HasNFT":false}`)
	mr.Set("nft_cache:2:0xC:0xD:1", `{"HasNFT":true}`)

	local.Set("1:0xA:0xB:1", CachedNFTAccess{HasNFT: true, ExpiresAt: time.Now().Add(time.Hour)})
	local.Set("1:0xA:0xB:2", CachedNFTAccess{HasNFT: false, ExpiresAt: time.Now().Add(time.Hour)})
	local.Set("2:0xC:0xD:1", CachedNFTAccess{HasNFT: true, ExpiresAt: time.Now().Add(time.Hour)})

	cache.DeleteByPrefix(context.Background(), "1:0xA:")

	_, ok := local.Get("1:0xA:0xB:1")
	assert.False(t, ok)

	_, ok = local.Get("2:0xC:0xD:1")
	assert.True(t, ok)

	_, err = mr.Get("nft_cache:1:0xA:0xB:1")
	assert.Error(t, err)

	val2, err := mr.Get("nft_cache:2:0xC:0xD:1")
	assert.NoError(t, err)
	assert.NotEmpty(t, val2)
}

package gateway

import (
	"context"
	"encoding/json"
	"time"

	"streamgate/pkg/middleware"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	tieredNFTCacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "streamgate_nft_tiered_cache_hits_total",
		Help: "NFT cache hits by tier (l1_memory, l2_redis)",
	}, []string{"tier"})
)

const nftRedisKeyPrefix = "nft_cache:"

type RedisNFTAccessCache struct {
	local *NFTAccessCache
	redis *redis.Client
}

func NewRedisNFTAccessCache(local *NFTAccessCache, redisClient *redis.Client) *RedisNFTAccessCache {
	return &RedisNFTAccessCache{local: local, redis: redisClient}
}

func (c *RedisNFTAccessCache) Get(key string) (middleware.NFTAccessEntry, bool) {
	if entry, ok := c.local.Get(key); ok {
		tieredNFTCacheHits.WithLabelValues("l1_memory").Inc()
		return toMiddlewareNFTAccessEntry(entry), true
	}

	if c.redis != nil {
		data, err := c.redis.Get(context.Background(), nftRedisKeyPrefix+key).Bytes()
		if err == nil {
			var entry middleware.NFTAccessEntry
			if json.Unmarshal(data, &entry) == nil && entry.Expires.After(time.Now()) {
				c.local.Set(key, fromMiddlewareNFTAccessEntry(entry))
				tieredNFTCacheHits.WithLabelValues("l2_redis").Inc()
				return entry, true
			}
		}
	}

	return middleware.NFTAccessEntry{}, false
}

func (c *RedisNFTAccessCache) Set(key string, entry middleware.NFTAccessEntry) {
	c.local.Set(key, fromMiddlewareNFTAccessEntry(entry))

	if c.redis != nil {
		data, err := json.Marshal(entry)
		if err == nil {
			ttl := time.Until(entry.Expires)
			if ttl > 0 {
				c.redis.Set(context.Background(), nftRedisKeyPrefix+key, data, ttl)
			}
		}
	}
}

func (c *RedisNFTAccessCache) Delete(key string) {
	c.local.Delete(key)
	if c.redis != nil {
		c.redis.Del(context.Background(), nftRedisKeyPrefix+key)
	}
}

func (c *RedisNFTAccessCache) DeleteByPrefix(prefix string) {
	c.local.DeleteByPrefix(prefix)
	if c.redis != nil {
		var cursor uint64
		for {
			keys, next, err := c.redis.Scan(context.Background(), cursor, nftRedisKeyPrefix+prefix+"*", 100).Result()
			if err != nil {
				break
			}
			if len(keys) > 0 {
				c.redis.Del(context.Background(), keys...)
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
}

func toMiddlewareNFTAccessEntry(e CachedNFTAccess) middleware.NFTAccessEntry {
	return middleware.NFTAccessEntry{
		HasNFT: e.HasNFT, Balance: e.Balance,
		BlockNumber: e.BlockNumber, BlockHash: e.BlockHash,
		Expires: e.ExpiresAt,
	}
}

func fromMiddlewareNFTAccessEntry(e middleware.NFTAccessEntry) CachedNFTAccess {
	return CachedNFTAccess{
		HasNFT: e.HasNFT, Balance: e.Balance,
		BlockNumber: e.BlockNumber, BlockHash: e.BlockHash,
		ExpiresAt: e.Expires,
	}
}

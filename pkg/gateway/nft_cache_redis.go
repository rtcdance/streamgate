package gateway

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	tieredNFTCacheHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "streamgate_nft_tiered_cache_hits_total",
		Help: "NFT cache hits by tier (l1_memory, l2_redis)",
	}, []string{"tier"})
)

func init() {
	if err := prometheus.Register(tieredNFTCacheHits); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
			panic(err)
		}
	}
}

const nftRedisKeyPrefix = "nft_cache:"

type RedisNFTAccessCache struct {
	local *NFTAccessCache
	redis *redis.Client
}

func NewRedisNFTAccessCache(local *NFTAccessCache, redisClient *redis.Client) *RedisNFTAccessCache {
	return &RedisNFTAccessCache{local: local, redis: redisClient}
}

func (c *RedisNFTAccessCache) Get(ctx context.Context, key string) (middleware.NFTAccessEntry, bool) {
	if entry, ok := c.local.Get(key); ok {
		tieredNFTCacheHits.WithLabelValues("l1_memory").Inc()
		return toMiddlewareNFTAccessEntry(entry), true
	}

	if c.redis != nil {
		data, err := c.redis.Get(ctx, nftRedisKeyPrefix+key).Bytes()
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

func (c *RedisNFTAccessCache) Set(ctx context.Context, key string, entry middleware.NFTAccessEntry) {
	c.local.Set(key, fromMiddlewareNFTAccessEntry(entry))

	if c.redis != nil {
		data, err := json.Marshal(entry)
		if err == nil {
			ttl := time.Until(entry.Expires)
			if ttl > 0 {
				c.redis.Set(ctx, nftRedisKeyPrefix+key, data, ttl)
			}
		}
	}
}

func (c *RedisNFTAccessCache) Delete(ctx context.Context, key string) {
	c.local.Delete(key)
	if c.redis != nil {
		c.redis.Del(ctx, nftRedisKeyPrefix+key)
	}
}

func (c *RedisNFTAccessCache) DeleteByPrefix(ctx context.Context, prefix string) {
	c.local.DeleteByPrefix(prefix)
	if c.redis != nil {
		var cursor uint64
		for {
			keys, next, err := c.redis.Scan(ctx, cursor, nftRedisKeyPrefix+prefix+"*", 100).Result()
			if err != nil {
				break
			}
			if len(keys) > 0 {
				c.redis.Del(ctx, keys...)
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

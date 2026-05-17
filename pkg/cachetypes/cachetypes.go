// Package cachetypes defines the CacheBackend interface used across
// streaming, content, and NFT services. Extracted here to avoid
// import cycles between pkg/storage and pkg/service.
package cachetypes

// CacheBackend defines the interface for cache storage backends.
// *storage.CacheStorage satisfies this interface implicitly.
//
//go:generate mockgen -destination=mocks/mock_cache_backend.go -package=mocks streamgate/pkg/cachetypes CacheBackend
import "time"

type CacheBackend interface {
	Get(key string) (interface{}, error)
	Set(key string, value interface{}) error
	SetWithExpiration(key string, value interface{}, ttl time.Duration) error
	Delete(key string) error
}

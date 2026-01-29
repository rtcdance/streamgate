package streaming

import "go.uber.org/zap"

// StreamCache caches streaming data
type StreamCache struct {
	logger *zap.Logger
}

// NewStreamCache creates a new stream cache
func NewStreamCache(logger *zap.Logger) *StreamCache {
	return &StreamCache{
		logger: logger,
	}
}

// Get gets cached stream
func (c *StreamCache) Get(key string) (interface{}, bool) {
	return nil, false
}

// Set sets stream cache
func (c *StreamCache) Set(key string, value interface{}) {
}

// Delete deletes cached stream
func (c *StreamCache) Delete(key string) {
}

// Close closes the cache
func (c *StreamCache) Close() {
}

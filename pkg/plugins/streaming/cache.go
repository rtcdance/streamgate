package streaming

// StreamCache caches streaming data
type StreamCache struct{}

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

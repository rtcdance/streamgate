package cache

// LRU implements LRU cache
type LRU struct {
	maxSize int
	cache   map[string]interface{}
}

// NewLRU creates a new LRU cache
func NewLRU(maxSize int) *LRU {
	return &LRU{
		maxSize: maxSize,
		cache:   make(map[string]interface{}),
	}
}

// Get gets a value from cache
func (l *LRU) Get(key string) (interface{}, bool) {
	val, ok := l.cache[key]
	return val, ok
}

// Set sets a value in cache
func (l *LRU) Set(key string, value interface{}) {
	l.cache[key] = value
}

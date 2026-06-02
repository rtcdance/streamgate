package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsValidContentID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"content-1", true},
		{"abc123", true},
		{"my_content", true},
		{"CONTENT", true},
		{"", false},
		{"content with space", false},
		{"content/slash", false},
		{"content.dot", false},
		{string(make([]byte, 257)), false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if tt.id == "" {
				assert.Equal(t, tt.want, isValidContentID(""))
			} else if len(tt.id) > 256 {
				longID := string(make([]byte, 257))
				for i := range longID {
					longID = longID[:i] + "a" + longID[i+1:]
				}
				assert.False(t, isValidContentID(longID))
			} else {
				assert.Equal(t, tt.want, isValidContentID(tt.id))
			}
		})
	}
}

func TestValidateSegmentName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"segment0.ts", true},
		{"seg.ts", true},
		{"../etc/passwd.ts", false},
		{"seg..ts", false},
		{"seg\\file.ts", false},
		{"segment0.mp4", false},
		{"/absolute.ts", false},
		{"./relative.ts", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validateSegmentName(tt.name))
		})
	}
}

func TestExtractSegmentNumber(t *testing.T) {
	tests := []struct {
		segName string
		want    int
	}{
		{"segment0.ts", 0},
		{"segment5.ts", 5},
		{"seg123.ts", 123},
		{"hls/720p/seg3.ts", 3},
		{"720p/seg10.ts", 10},
		{"no-number.ts", 0},
	}

	for _, tt := range tests {
		t.Run(tt.segName, func(t *testing.T) {
			assert.Equal(t, tt.want, extractSegmentNumber(tt.segName))
		})
	}
}

func TestExtractPlaybackToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		query  string
		want   string
	}{
		{"bearer token", "Bearer my-token", "", "my-token"},
		{"query param", "", "my-token", "my-token"},
		{"query takes precedence over bearer", "Bearer bearer-tok", "query-tok", "query-tok"},
		{"no token", "", "", ""},
		{"non-bearer auth", "Basic dXNlcjpwYXNz", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptestNewRecorder()
			c := newTestContext(w)
			if tt.header != "" {
				c.Request.Header.Set("Authorization", tt.header)
			}
			if tt.query != "" {
				c.Request.URL.RawQuery = "playback_token=" + tt.query
			}
			assert.Equal(t, tt.want, extractPlaybackToken(c))
		})
	}
}

func TestNewStreamingCache(t *testing.T) {
	cache := NewStreamingCache()
	assert.NotNil(t, cache)
	assert.NotNil(t, cache.manifests)
	assert.NotNil(t, cache.segmentIdx)
}

func TestStreamingCache_Manifest(t *testing.T) {
	cache := NewStreamingCache()

	_, ok := cache.GetManifest("content-1", "wallet-1")
	assert.False(t, ok)

	cache.SetManifest("content-1", "#EXTM3U", "wallet-1")

	manifest, ok := cache.GetManifest("content-1", "wallet-1")
	assert.True(t, ok)
	assert.Equal(t, "#EXTM3U", manifest)

	_, ok = cache.GetManifest("content-1", "wallet-2")
	assert.False(t, ok)
}

func TestStreamingCache_SegmentIndex(t *testing.T) {
	cache := NewStreamingCache()

	_, ok := cache.GetSegmentIndex("content-1")
	assert.False(t, ok)

	qualities := map[string][]string{
		"720p": {"seg0.ts", "seg1.ts"},
	}
	cache.SetSegmentIndex("content-1", qualities)

	result, ok := cache.GetSegmentIndex("content-1")
	assert.True(t, ok)
	assert.Equal(t, qualities, result)
}

func TestStreamingCache_Invalidate(t *testing.T) {
	cache := NewStreamingCache()

	cache.SetManifest("content-1", "#EXTM3U", "wallet-1")
	cache.SetSegmentIndex("content-1", map[string][]string{"720p": {"seg0.ts"}})

	cache.Invalidate("content-1")

	_, ok := cache.GetManifest("content-1", "wallet-1")
	assert.False(t, ok)

	_, ok = cache.GetSegmentIndex("content-1")
	assert.False(t, ok)
}

func TestStreamingCache_Manifest_Expiry(t *testing.T) {
	cache := NewStreamingCache()
	cache.manifests.Set("content-1", manifestCacheEntry{
		manifest:   "#EXTM3U",
		expiresAt:  time.Now().Add(-time.Hour),
		walletAddr: "wallet-1",
	})

	_, ok := cache.GetManifest("content-1", "wallet-1")
	assert.False(t, ok)
}

func TestBuildSegmentCandidates(t *testing.T) {
	t.Run("no quality cache no quality param", func(t *testing.T) {
		cache := NewStreamingCache()
		candidates := buildSegmentCandidates("c1", "seg0.ts", "", cache)
		assert.NotEmpty(t, candidates)
		assert.Equal(t, "c1/seg0.ts", candidates[0].key)
	})

	t.Run("with quality param no cache", func(t *testing.T) {
		cache := NewStreamingCache()
		candidates := buildSegmentCandidates("c1", "seg0.ts", "720p", cache)
		assert.GreaterOrEqual(t, len(candidates), 2)
	})

	t.Run("with quality cache", func(t *testing.T) {
		cache := NewStreamingCache()
		cache.SetSegmentIndex("c1", map[string][]string{
			"720p":  {"seg0.ts"},
			"1080p": {"seg0.ts"},
		})
		candidates := buildSegmentCandidates("c1", "seg0.ts", "720p", cache)
		assert.GreaterOrEqual(t, len(candidates), 3)
	})
}

func TestNewStreamLimiter(t *testing.T) {
	t.Run("default value", func(t *testing.T) {
		limiter := newStreamLimiter(0)
		assert.Equal(t, 1000, cap(limiter.sem))
	})

	t.Run("custom value", func(t *testing.T) {
		limiter := newStreamLimiter(10)
		assert.Equal(t, 10, cap(limiter.sem))
	})
}

func TestStreamLimiter_AcquireAndRelease(t *testing.T) {
	limiter := newStreamLimiter(2)

	assert.True(t, limiter.tryAcquire())
	assert.True(t, limiter.tryAcquire())
	assert.False(t, limiter.tryAcquire())

	limiter.release()
	assert.True(t, limiter.tryAcquire())
	limiter.release()
	limiter.release()
}

func TestLRUCache_GetSet(t *testing.T) {
	cache := newLRUCache[string](10, time.Minute)

	_, ok := cache.Get("key1")
	assert.False(t, ok)

	cache.Set("key1", "value1")
	val, ok := cache.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

func TestLRUCache_Delete(t *testing.T) {
	cache := newLRUCache[string](10, time.Minute)
	cache.Set("key1", "value1")
	cache.Delete("key1")

	_, ok := cache.Get("key1")
	assert.False(t, ok)
}

func TestLRUCache_Eviction(t *testing.T) {
	cache := newLRUCache[string](2, time.Minute)
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.Set("key3", "value3")

	_, ok := cache.Get("key1")
	assert.False(t, ok)

	_, ok = cache.Get("key3")
	assert.True(t, ok)
}

func TestLRUCache_Overwrite(t *testing.T) {
	cache := newLRUCache[string](10, time.Minute)
	cache.Set("key1", "value1")
	cache.Set("key1", "value2")

	val, ok := cache.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value2", val)
}

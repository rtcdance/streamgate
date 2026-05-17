package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectStreamType(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"video.m3u8", "hls"},
		{"stream.M3U8", "hls"},
		{"manifest.mpd", "dash"},
		{"video.mp4", "progressive"},
		{"clip.webm", "progressive"},
		{"document.txt", "unknown"},
		{"", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := DetectStreamType(tt.filename)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStreamingService_GenerateHLSPlaylist(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "https://cdn.example.com")

	qualities := []Quality{
		{Name: "1080p", Resolution: "1920x1080", Bitrate: 5000},
		{Name: "720p", Resolution: "1280x720", Bitrate: 3000},
		{Name: "480p", Resolution: "854x480", Bitrate: 1500},
	}

	playlist, err := svc.GenerateHLSPlaylist("content-123", qualities)
	require.NoError(t, err)

	// Must start with HLS header
	assert.True(t, strings.HasPrefix(playlist, "#EXTM3U"))
	assert.Contains(t, playlist, "#EXT-X-VERSION:3")

	// Each quality gets a STREAM-INF line and a URL line
	for _, q := range qualities {
		assert.Contains(t, playlist, q.Resolution, "missing resolution %s", q.Resolution)
		assert.Contains(t, playlist, "content-123/"+q.Name, "missing URL path for %s", q.Name)
	}

	// Bandwidth should be bitrate * 1000
	assert.Contains(t, playlist, "BANDWIDTH=5000000")
	assert.Contains(t, playlist, "BANDWIDTH=3000000")
	assert.Contains(t, playlist, "BANDWIDTH=1500000")
}

func TestStreamingService_GenerateDASHManifest(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "https://cdn.example.com")

	qualities := []Quality{
		{Name: "1080p", Resolution: "1920x1080", Bitrate: 5000},
		{Name: "720p", Resolution: "1280x720", Bitrate: 3000},
	}

	manifest, err := svc.GenerateDASHManifest("content-456", qualities)
	require.NoError(t, err)

	// Must contain DASH XML structure
	assert.Contains(t, manifest, `<?xml version="1.0"`)
	assert.Contains(t, manifest, `urn:mpeg:dash:schema:mpd:2011`)
	assert.Contains(t, manifest, `<Period>`)
	assert.Contains(t, manifest, `<AdaptationSet`)

	// Each quality gets a Representation
	for _, q := range qualities {
		width := strings.Split(q.Resolution, "x")[0]
		assert.Contains(t, manifest, width, "missing width for %s", q.Name)
		assert.Contains(t, manifest, "content-456/"+q.Name+".mp4")
	}
}

func TestStreamingService_GetStream_CacheHit(t *testing.T) {
	cached := &StreamInfo{
		ID:        "stream-1",
		ContentID: "content-123",
		Type:      "hls",
		Status:    "ready",
	}

	cache := &mockStreamingCache{
		data: map[string]interface{}{
			"stream:content-123": cached,
		},
	}

	svc := NewStreamingService(nil, nil, cache, "https://cdn.example.com")

	result, err := svc.GetStream(context.Background(), "content-123")
	require.NoError(t, err)
	assert.Equal(t, "stream-1", result.ID)
	assert.Equal(t, "hls", result.Type)
}

func TestStreamingService_GetStream_CacheMiss_NoDB(t *testing.T) {
	cache := &mockStreamingCache{data: map[string]interface{}{}}
	svc := NewStreamingService(nil, nil, cache, "https://cdn.example.com")

	// No DB set, should return error
	_, err := svc.GetStream(context.Background(), "content-nonexistent")
	assert.Error(t, err)
}

// mockStreamingCache implements StreamingCacheStorage for testing
type mockStreamingCache struct {
	data map[string]interface{}
}

func (m *mockStreamingCache) Get(key string) (interface{}, error) {
	if v, ok := m.data[key]; ok {
		return v, nil
	}
	return nil, errCacheMiss
}

func (m *mockStreamingCache) Set(key string, value interface{}) error {
	m.data[key] = value
	return nil
}

func (m *mockStreamingCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockStreamingCache) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	return m.Set(key, value)
}

var errCacheMiss = &cacheMissError{}

type cacheMissError struct{}

func (e *cacheMissError) Error() string { return "cache miss" }

package streaming

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestParseRangeHeader_SingleRange(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		fileSize  int64
		wantStart int64
		wantEnd   int64
	}{
		{"standard range", "bytes=0-499", 1000, 0, 499},
		{"middle range", "bytes=200-699", 1000, 200, 699},
		{"open end", "bytes=500-", 1000, 500, 999},
		{"last N bytes", "bytes=-500", 1000, 500, 999},
		{"single byte", "bytes=0-0", 1000, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ranges, err := parseRangeHeader(tc.header, tc.fileSize)
			require.NoError(t, err)
			require.Len(t, ranges, 1)
			assert.Equal(t, tc.wantStart, ranges[0].Start)
			assert.Equal(t, tc.wantEnd, ranges[0].End)
		})
	}
}

func TestParseRangeHeader_MultipleRanges(t *testing.T) {
	ranges, err := parseRangeHeader("bytes=0-99,200-299", 1000)
	require.NoError(t, err)
	require.Len(t, ranges, 2)
	assert.Equal(t, int64(0), ranges[0].Start)
	assert.Equal(t, int64(99), ranges[0].End)
	assert.Equal(t, int64(200), ranges[1].Start)
	assert.Equal(t, int64(299), ranges[1].End)
}

func TestParseRangeHeader_InvalidFormat(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"missing bytes prefix", "0-499"},
		{"empty", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRangeHeader(tc.header, 1000)
			require.Error(t, err)
		})
	}
}

func TestParseRangeHeader_InvalidRange(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		fileSize int64
	}{
		{"start beyond file", "bytes=2000-2999", 1000},
		{"start greater than end", "bytes=500-100", 1000},
		{"negative start", "bytes=-1-100", 1000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRangeHeader(tc.header, tc.fileSize)
			require.Error(t, err)
		})
	}
}

func TestParseRangeSpec(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		fileSize  int64
		wantStart int64
		wantEnd   int64
		wantErr   bool
	}{
		{"standard", "0-499", 1000, 0, 499, false},
		{"open end", "500-", 1000, 500, 999, false},
		{"suffix", "-200", 1000, 800, 999, false},
		{"end beyond file", "0-2000", 1000, 0, 999, false},
		{"invalid spec", "abc", 1000, 0, 0, true},
		{"invalid start", "abc-100", 1000, 0, 0, true},
		{"invalid end", "0-abc", 1000, 0, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fileRange, err := parseRangeSpec(tc.spec, tc.fileSize)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantStart, fileRange.Start)
				assert.Equal(t, tc.wantEnd, fileRange.End)
			}
		})
	}
}

func TestNewRangeHandler(t *testing.T) {
	handler := NewRangeHandler("/tmp", zap.NewNop())
	assert.NotNil(t, handler)
	assert.Equal(t, "/tmp", handler.storageDir)
	assert.NotNil(t, handler.cache)
}

func TestNewRangeCache(t *testing.T) {
	cache := NewRangeCache(1024)
	assert.NotNil(t, cache)
	assert.Equal(t, int64(1024), cache.maxSize)
}

func TestRangeHandler_GetContentType(t *testing.T) {
	handler := NewRangeHandler("/tmp", zap.NewNop())

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"mp4", "video.mp4", "video/mp4"},
		{"webm", "video.webm", "video/webm"},
		{"mp3", "audio.mp3", "audio/mpeg"},
		{"m3u8", "stream.m3u8", "application/vnd.apple.mpegurl"},
		{"mpd", "manifest.mpd", "application/dash+xml"},
		{"ts", "segment.ts", "video/mp2t"},
		{"unknown", "file.xyz", "application/octet-stream"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.getContentType(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestRangeHandler_CacheRangeAndGetCachedRange(t *testing.T) {
	cache := NewRangeCache(1024)
	handler := &RangeHandler{storageDir: "/tmp", logger: zap.NewNop(), cache: cache}
	ctx := context.Background()

	data := []byte("hello world")
	cacheKey := handler.getCacheKey("test.txt", 0, 10)

	cache.mu.Lock()
	cache.entries[cacheKey] = &CacheEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(5 * time.Minute),
		Accessed:  time.Now(),
	}
	cache.mu.Unlock()

	cached, ok := handler.GetCachedRange(ctx, "test.txt", 0, 10)
	assert.True(t, ok)
	assert.Equal(t, data, cached)
}

func TestRangeHandler_GetCachedRange_Miss(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())
	ctx := context.Background()

	_, ok := handler.GetCachedRange(ctx, "nonexistent.txt", 0, 10)
	assert.False(t, ok)
}

func TestRangeHandler_GetCacheKey(t *testing.T) {
	handler := NewRangeHandler("/tmp", zap.NewNop())

	key := handler.getCacheKey("test.txt", 0, 499)
	assert.Equal(t, "test.txt:0-499", key)
}

func TestRangeHandler_ValidateRange(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())
	ctx := context.Background()

	valid, err := handler.ValidateRange(ctx, "nonexistent.txt", 0, 10)
	require.Error(t, err)
	assert.False(t, valid)
}

func TestRangeHandler_GetFileInfo_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())
	ctx := context.Background()

	_, err := handler.GetFileInfo(ctx, "nonexistent.txt")
	require.Error(t, err)
}

func TestRangeHandler_ServeRange_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewRangeHandler(tmpDir, zap.NewNop())
	ctx := context.Background()

	_, err := handler.ServeRange(ctx, "nonexistent.txt", 0, 10)
	require.Error(t, err)
}

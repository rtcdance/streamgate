package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStreamingExt_GenerateHLSPlaylist_EmptySegments(t *testing.T) {
	svc := NewStreamingService(nil, nil, newMockCache(), "http://cdn.example.com")
	_, err := svc.GenerateHLSPlaylist("content-1", map[string][]string{}, "token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no segments available")
}

func TestStreamingExt_GenerateHLSPlaylist_SingleQuality(t *testing.T) {
	svc := NewStreamingService(nil, nil, newMockCache(), "http://cdn.example.com")
	segments := map[string][]string{
		"720p": {"segment0.ts", "segment1.ts"},
	}
	playlist, err := svc.GenerateHLSPlaylist("content-1", segments, "playback-token")
	require.NoError(t, err)
	assert.Contains(t, playlist, "#EXTM3U")
	assert.Contains(t, playlist, "#EXTINF")
	assert.Contains(t, playlist, "playback-token")
	assert.Contains(t, playlist, "#EXT-X-ENDLIST")
}

func TestStreamingExt_GenerateHLSPlaylist_MultiQuality(t *testing.T) {
	svc := NewStreamingService(nil, nil, newMockCache(), "http://cdn.example.com")
	segments := map[string][]string{
		"1080p": {"segment0.ts"},
		"720p":  {"segment0.ts"},
		"480p":  {"segment0.ts"},
	}
	playlist, err := svc.GenerateHLSPlaylist("content-1", segments, "token")
	require.NoError(t, err)
	assert.Contains(t, playlist, "#EXTM3U")
	assert.Contains(t, playlist, "#EXT-X-STREAM-INF")
	assert.Contains(t, playlist, "BANDWIDTH=5000000")
	assert.Contains(t, playlist, "BANDWIDTH=2800000")
	assert.Contains(t, playlist, "BANDWIDTH=1400000")
}

func TestStreamingExt_GenerateDASHManifest_MultipleQualities(t *testing.T) {
	svc := NewStreamingService(nil, nil, newMockCache(), "http://cdn.example.com")
	qualities := []Quality{
		{Name: "1080p", Resolution: "1920x1080", Bitrate: 5000},
		{Name: "720p", Resolution: "1280x720", Bitrate: 2800},
	}
	manifest, err := svc.GenerateDASHManifest("content-1", qualities, "token")
	require.NoError(t, err)
	assert.Contains(t, manifest, "<MPD")
	assert.Contains(t, manifest, "bandwidth=\"5000000\"")
	assert.Contains(t, manifest, "bandwidth=\"2800000\"")
	assert.Contains(t, manifest, "width=\"1920\"")
	assert.Contains(t, manifest, "width=\"1280\"")
}

func TestStreamingExt_GenerateDASHManifest_EmptyQualities(t *testing.T) {
	svc := NewStreamingService(nil, nil, newMockCache(), "http://cdn.example.com")
	manifest, err := svc.GenerateDASHManifest("content-1", []Quality{}, "token")
	require.NoError(t, err)
	assert.Contains(t, manifest, "<MPD")
}

func TestStreamingExt_DetectStreamType_Table(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"video.m3u8", "hls"},
		{"manifest.mpd", "dash"},
		{"movie.mp4", "progressive"},
		{"clip.webm", "progressive"},
		{"unknown.bin", "unknown"},
		{"", "unknown"},
		{"VIDEO.M3U8", "hls"},
		{"clip.WebM", "progressive"},
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			assert.Equal(t, tt.want, DetectStreamType(tt.filename))
		})
	}
}

func TestStreamingExt_GetStream_NilDB_NoCache(t *testing.T) {
	svc := NewStreamingService(nil, newMockStreamingStorage(), nil, "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "content-id")
	assert.Error(t, err)
}

func TestStreamingExt_GetStream_CacheMiss_NilDB(t *testing.T) {
	cache := newMockCache()
	svc := NewStreamingService(nil, newMockStreamingStorage(), cache, "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "missing")
	assert.Error(t, err)
}

func TestStreamingExt_NewWithLogger(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://localhost", zap.NewNop())
	assert.NotNil(t, svc)
}

func TestStreamingExt_Close(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://localhost")
	svc.Close()
}

func TestStreamingExt_Quality_Fields(t *testing.T) {
	q := Quality{
		Name:       "4K",
		Resolution: "3840x2160",
		Bitrate:    15000,
		URL:        "http://cdn.example.com/4k.m3u8",
	}
	assert.Equal(t, "4K", q.Name)
	assert.Equal(t, "3840x2160", q.Resolution)
	assert.Equal(t, 15000, q.Bitrate)
	assert.Equal(t, "http://cdn.example.com/4k.m3u8", q.URL)
}

func TestStreamingExt_StreamInfo_Fields(t *testing.T) {
	si := StreamInfo{
		ID:        "s1",
		ContentID: "c1",
		Type:      "hls",
		URL:       "http://cdn.example.com/playlist.m3u8",
		Playlist:  "#EXTM3U",
		Status:    "ready",
		Duration:  300,
	}
	assert.Equal(t, "hls", si.Type)
	assert.Equal(t, "ready", si.Status)
	assert.Equal(t, 300, si.Duration)
}

func TestStreamingExt_GenerateHLSPlaylist_UnknownQuality(t *testing.T) {
	svc := NewStreamingService(nil, nil, newMockCache(), "http://cdn.example.com")
	segments := map[string][]string{
		"4k":   {"segment0.ts"},
		"720p": {"segment0.ts"},
	}
	playlist, err := svc.GenerateHLSPlaylist("content-1", segments, "token")
	require.NoError(t, err)
	assert.Contains(t, playlist, "#EXTM3U")
	assert.Contains(t, playlist, "#EXT-X-STREAM-INF")
}

package streaming_test

import (
	"context"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/test/helpers"

	"github.com/stretchr/testify/require"
)

// MockCacheStorage implements service.StreamingCacheStorage for testing
type MockCacheStorage struct {
	cache map[string]interface{}
}

func NewMockCacheStorage() *MockCacheStorage {
	return &MockCacheStorage{
		cache: make(map[string]interface{}),
	}
}

func (m *MockCacheStorage) Get(key string) (interface{}, error) {
	val, exists := m.cache[key]
	if !exists {
		return nil, nil
	}
	return val, nil
}

func (m *MockCacheStorage) Set(key string, value interface{}) error {
	m.cache[key] = value
	return nil
}

func (m *MockCacheStorage) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	m.cache[key] = value
	return nil
}

func (m *MockCacheStorage) Delete(key string) error {
	delete(m.cache, key)
	return nil
}

func newStreamingService(t *testing.T, db storage.DB, cache *MockCacheStorage) *service.StreamingService {
	t.Helper()
	objStorage := helpers.SetupTestStorage(t)
	return service.NewStreamingService(db, objStorage, cache, "http://localhost:8080")
}

func TestStreamingService_CreateStream(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	cache := NewMockCacheStorage()
	streamingService := newStreamingService(t, db, cache)

	streamID, err := streamingService.CreateStream(context.Background(), "content-123", "hls")
	require.NoError(t, err)
	require.NotEmpty(t, streamID)
}

func TestStreamingService_GetStream(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	cache := NewMockCacheStorage()
	streamingService := newStreamingService(t, db, cache)

	streamID, err := streamingService.CreateStream(context.Background(), "content-123", "hls")
	require.NoError(t, err)

	err = streamingService.UpdateStreamStatus(context.Background(), streamID, "ready")
	require.NoError(t, err)

	stream, err := streamingService.GetStream(context.Background(), "content-123")
	require.NoError(t, err)
	require.NotNil(t, stream)
	require.Equal(t, "content-123", stream.ContentID)
}

func TestStreamingService_HLSFormat(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	cache := NewMockCacheStorage()
	streamingService := newStreamingService(t, db, cache)

	qualitySegments := map[string][]string{
		"1080p": {"seg000.ts", "seg001.ts"},
		"720p":  {"seg000.ts", "seg001.ts"},
	}
	playlist, err := streamingService.GenerateHLSPlaylist("content-123", qualitySegments, "test-token")
	require.NoError(t, err)
	require.NotEmpty(t, playlist)
	require.Contains(t, playlist, ".m3u8")
}

func TestStreamingService_DASHFormat(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	cache := NewMockCacheStorage()
	streamingService := newStreamingService(t, db, cache)

	manifest, err := streamingService.GenerateDASHManifest("content-123", []service.Quality{
		{Name: "1080p", Resolution: "1920x1080", Bitrate: 5000, URL: "http://localhost:8080/stream/1080p.mpd"},
		{Name: "720p", Resolution: "1280x720", Bitrate: 3000, URL: "http://localhost:8080/stream/720p.mpd"},
	}, "test-playback-token")
	require.NoError(t, err)
	require.NotEmpty(t, manifest)
	require.Contains(t, manifest, ".mpd")
}

func TestStreamingService_AdaptiveBitrate(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	cache := NewMockCacheStorage()
	streamingService := newStreamingService(t, db, cache)

	streamID, err := streamingService.CreateStream(context.Background(), "content-123", "hls")
	require.NoError(t, err)

	err = streamingService.AddStreamQuality(context.Background(), streamID, service.Quality{
		Name: "1080p", Resolution: "1920x1080", Bitrate: 5000, URL: "http://localhost:8080/stream/1080p.m3u8",
	})
	require.NoError(t, err)

	err = streamingService.AddStreamQuality(context.Background(), streamID, service.Quality{
		Name: "720p", Resolution: "1280x720", Bitrate: 3000, URL: "http://localhost:8080/stream/720p.m3u8",
	})
	require.NoError(t, err)

	stream, err := streamingService.GetStreamByID(context.Background(), streamID)
	require.NoError(t, err)
	require.True(t, len(stream.Qualities) > 0)
}

func TestStreamingService_DeleteStream(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	cache := NewMockCacheStorage()
	streamingService := newStreamingService(t, db, cache)

	streamID, err := streamingService.CreateStream(context.Background(), "content-123", "hls")
	require.NoError(t, err)

	err = streamingService.DeleteStream(context.Background(), streamID)
	require.NoError(t, err)

	_, err = streamingService.GetStreamByID(context.Background(), streamID)
	require.Error(t, err)
}

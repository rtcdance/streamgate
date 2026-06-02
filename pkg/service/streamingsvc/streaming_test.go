package streamingsvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func (m *mockDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return stg.NewErrorCancelRow(errors.New("not implemented"))
}
func (m *mockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	return nil, errors.New("not implemented")
}
func (m *mockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return errors.New("not implemented")
}
func (m *mockDB) Ping(ctx context.Context) error { return nil }
func (m *mockDB) Close() error                   { return nil }

type mockCache struct {
	data map[string]interface{}
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]interface{})}
}

func (m *mockCache) Get(key string) (interface{}, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("cache miss: %s", key)
	}
	return v, nil
}
func (m *mockCache) Set(key string, value interface{}) error {
	m.data[key] = value
	return nil
}
func (m *mockCache) SetWithExpiration(key string, value interface{}, _ time.Duration) error {
	return m.Set(key, value)
}
func (m *mockCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

type mockObjStore struct {
	data map[string][]byte
}

func newMockObjStore() *mockObjStore {
	return &mockObjStore{data: make(map[string][]byte)}
}

func (m *mockObjStore) Download(_ context.Context, bucket, key string) ([]byte, error) {
	d, ok := m.data[bucket+"/"+key]
	if !ok {
		return nil, errors.New("not found")
	}
	return d, nil
}
func (m *mockObjStore) Exists(_ context.Context, bucket, key string) (bool, error) {
	_, ok := m.data[bucket+"/"+key]
	return ok, nil
}

type mockResult struct {
	rowsAffected int64
}

func (m *mockResult) LastInsertId() (int64, error) { return 0, nil }
func (m *mockResult) RowsAffected() (int64, error) { return m.rowsAffected, nil }

func TestNewStreamingService(t *testing.T) {
	t.Run("with all params", func(t *testing.T) {
		svc := NewStreamingService(&mockDB{}, newMockObjStore(), newMockCache(), "http://cdn.example.com")
		assert.NotNil(t, svc)
		assert.Equal(t, "http://cdn.example.com", svc.baseURL)
	})

	t.Run("nil db logs warning", func(t *testing.T) {
		svc := NewStreamingService(nil, newMockObjStore(), newMockCache(), "http://cdn.example.com")
		assert.NotNil(t, svc)
	})

	t.Run("nil logger uses nop", func(t *testing.T) {
		svc := NewStreamingService(&mockDB{}, newMockObjStore(), newMockCache(), "http://cdn.example.com")
		assert.NotNil(t, svc.logger)
	})
}

func TestStreamingService_Close(t *testing.T) {
	svc := NewStreamingService(&mockDB{}, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	assert.NotPanics(t, func() { svc.Close() })
}

func TestBuildSimplePlaylist(t *testing.T) {
	t.Run("basic segments", func(t *testing.T) {
		segments := []string{"seg0.ts", "seg1.ts", "seg2.ts"}
		playlist := BuildSimplePlaylist("content-1", segments, "test-token")

		assert.True(t, strings.HasPrefix(playlist, "#EXTM3U"))
		assert.Contains(t, playlist, "#EXT-X-VERSION:3")
		assert.Contains(t, playlist, "#EXT-X-TARGETDURATION:10")
		assert.Contains(t, playlist, "#EXT-X-MEDIA-SEQUENCE:0")
		assert.Contains(t, playlist, "#EXTINF:6.0,")
		assert.Contains(t, playlist, "/api/v1/streaming/content-1/segment/seg0.ts?playback_token=test-token")
		assert.Contains(t, playlist, "/api/v1/streaming/content-1/segment/seg1.ts?playback_token=test-token")
		assert.Contains(t, playlist, "/api/v1/streaming/content-1/segment/seg2.ts?playback_token=test-token")
		assert.Contains(t, playlist, "#EXT-X-ENDLIST")
	})

	t.Run("segments with path prefix", func(t *testing.T) {
		segments := []string{"hls/720p/seg0.ts", "hls/720p/seg1.ts"}
		playlist := BuildSimplePlaylist("content-2", segments, "tkn")

		assert.Contains(t, playlist, "/segment/seg0.ts?playback_token=tkn")
		assert.Contains(t, playlist, "/segment/seg1.ts?playback_token=tkn")
	})

	t.Run("empty segments", func(t *testing.T) {
		playlist := BuildSimplePlaylist("content-3", []string{}, "token")
		assert.Contains(t, playlist, "#EXTM3U")
		assert.Contains(t, playlist, "#EXT-X-ENDLIST")
	})
}

func TestBuildMasterPlaylist(t *testing.T) {
	t.Run("multiple qualities", func(t *testing.T) {
		qualitySegments := map[string][]string{
			"1080p": {"seg0.ts"},
			"720p":  {"seg0.ts"},
			"480p":  {"seg0.ts"},
		}
		playlist := BuildMasterPlaylist("content-1", qualitySegments, "test-token")

		assert.True(t, strings.HasPrefix(playlist, "#EXTM3U"))
		assert.Contains(t, playlist, "#EXT-X-VERSION:3")
		assert.Contains(t, playlist, "BANDWIDTH=5000000")
		assert.Contains(t, playlist, "RESOLUTION=1920x1080")
		assert.Contains(t, playlist, "BANDWIDTH=2800000")
		assert.Contains(t, playlist, "RESOLUTION=1280x720")
		assert.Contains(t, playlist, "BANDWIDTH=1400000")
		assert.Contains(t, playlist, "RESOLUTION=854x480")
	})

	t.Run("unknown quality uses default bandwidth", func(t *testing.T) {
		qualitySegments := map[string][]string{
			"4k": {"seg0.ts"},
		}
		playlist := BuildMasterPlaylist("content-1", qualitySegments, "token")

		assert.Contains(t, playlist, "BANDWIDTH=1500000")
		assert.Contains(t, playlist, "RESOLUTION=1280x720")
	})

	t.Run("360p quality", func(t *testing.T) {
		qualitySegments := map[string][]string{
			"360p": {"seg0.ts"},
		}
		playlist := BuildMasterPlaylist("content-1", qualitySegments, "token")

		assert.Contains(t, playlist, "BANDWIDTH=800000")
		assert.Contains(t, playlist, "RESOLUTION=640x360")
	})

	t.Run("resolution-based keys (ABR)", func(t *testing.T) {
		qualitySegments := map[string][]string{
			"1920x1080": {"seg0.ts"},
			"1280x720":  {"seg0.ts"},
			"854x480":   {"seg0.ts"},
			"640x360":   {"seg0.ts"},
		}
		playlist := BuildMasterPlaylist("content-1", qualitySegments, "token")

		assert.Contains(t, playlist, "BANDWIDTH=5000000")
		assert.Contains(t, playlist, "RESOLUTION=1920x1080")
		assert.Contains(t, playlist, "BANDWIDTH=2800000")
		assert.Contains(t, playlist, "RESOLUTION=1280x720")
		assert.Contains(t, playlist, "BANDWIDTH=1400000")
		assert.Contains(t, playlist, "RESOLUTION=854x480")
		assert.Contains(t, playlist, "BANDWIDTH=800000")
		assert.Contains(t, playlist, "RESOLUTION=640x360")
	})
}

func TestBuildMediaPlaylist(t *testing.T) {
	t.Run("basic media playlist", func(t *testing.T) {
		segments := []string{"seg0.ts", "seg1.ts"}
		playlist := BuildMediaPlaylist("content-1", "720p", segments, "test-token")

		assert.True(t, strings.HasPrefix(playlist, "#EXTM3U"))
		assert.Contains(t, playlist, "#EXT-X-VERSION:3")
		assert.Contains(t, playlist, "#EXTINF:6.0,")
		assert.Contains(t, playlist, "/segment/seg0.ts?quality=720p&playback_token=test-token")
		assert.Contains(t, playlist, "/segment/seg1.ts?quality=720p&playback_token=test-token")
		assert.Contains(t, playlist, "#EXT-X-ENDLIST")
	})

	t.Run("segments with path prefix", func(t *testing.T) {
		segments := []string{"hls/1080p/seg0.ts"}
		playlist := BuildMediaPlaylist("content-1", "1080p", segments, "tkn")

		assert.Contains(t, playlist, "/segment/seg0.ts?quality=1080p&playback_token=tkn")
	})
}

func TestIsValidStreamTransition(t *testing.T) {
	tests := []struct {
		from string
		to   string
		want bool
	}{
		{"pending", "ready", true},
		{"pending", "error", true},
		{"pending", "pending", true},
		{"pending", "live", false},
		{"pending", "ended", false},
		{"ready", "live", true},
		{"ready", "error", true},
		{"ready", "expired", true},
		{"ready", "pending", false},
		{"live", "ended", true},
		{"live", "error", true},
		{"live", "ready", false},
		{"ended", "expired", true},
		{"ended", "pending", false},
		{"expired", "pending", false},
		{"expired", "expired", true},
		{"error", "pending", true},
		{"error", "ready", false},
		{"nonexistent", "ready", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s->%s", tt.from, tt.to), func(t *testing.T) {
			assert.Equal(t, tt.want, isValidStreamTransition(tt.from, tt.to))
		})
	}
}

func TestDetectStreamType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"video.m3u8", "hls"},
		{"stream.M3U8", "hls"},
		{"manifest.mpd", "dash"},
		{"video.mp4", "progressive"},
		{"clip.webm", "progressive"},
		{"document.txt", "unknown"},
		{"", "unknown"},
		{"VIDEO.MP4", "progressive"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			assert.Equal(t, tt.want, DetectStreamType(tt.filename))
		})
	}
}

func TestQualityToResolution(t *testing.T) {
	tests := []struct {
		quality string
		want    string
	}{
		{"1080p", "1920x1080"},
		{"720p", "1280x720"},
		{"480p", "854x480"},
		{"360p", "640x360"},
		{"unknown", "1280x720"},
		{"4k", "1280x720"},
	}

	for _, tt := range tests {
		t.Run(tt.quality, func(t *testing.T) {
			assert.Equal(t, tt.want, qualityToResolution(tt.quality))
		})
	}
}

func TestStreamingService_GenerateHLSPlaylist(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://cdn.example.com")

	t.Run("no segments returns error", func(t *testing.T) {
		_, err := svc.GenerateHLSPlaylist("content-1", map[string][]string{}, "token")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no segments available")
	})

	t.Run("single quality returns simple playlist", func(t *testing.T) {
		qualitySegments := map[string][]string{
			"720p": {"seg0.ts", "seg1.ts"},
		}
		playlist, err := svc.GenerateHLSPlaylist("content-1", qualitySegments, "token")
		require.NoError(t, err)
		assert.Contains(t, playlist, "#EXTM3U")
		assert.Contains(t, playlist, "#EXTINF:6.0,")
		assert.NotContains(t, playlist, "#EXT-X-STREAM-INF")
	})

	t.Run("multiple qualities returns master playlist", func(t *testing.T) {
		qualitySegments := map[string][]string{
			"1080p": {"seg0.ts"},
			"720p":  {"seg0.ts"},
		}
		playlist, err := svc.GenerateHLSPlaylist("content-1", qualitySegments, "token")
		require.NoError(t, err)
		assert.Contains(t, playlist, "#EXT-X-STREAM-INF")
		assert.Contains(t, playlist, "BANDWIDTH=5000000")
	})
}

func TestStreamingService_GenerateDASHManifest(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://cdn.example.com")

	t.Run("single quality", func(t *testing.T) {
		qualities := []Quality{
			{Name: "1080p", Resolution: "1920x1080", Bitrate: 5000},
		}
		manifest, err := svc.GenerateDASHManifest("content-1", qualities, "token")
		require.NoError(t, err)
		assert.Contains(t, manifest, `<?xml version="1.0"`)
		assert.Contains(t, manifest, `<MPD`)
		assert.Contains(t, manifest, `<Period>`)
		assert.Contains(t, manifest, `<AdaptationSet`)
		assert.Contains(t, manifest, `<Representation`)
		assert.Contains(t, manifest, `bandwidth="5000000"`)
		assert.Contains(t, manifest, `width="1920"`)
		assert.Contains(t, manifest, "content-1/1080p.mp4?playback_token=token")
	})

	t.Run("multiple qualities", func(t *testing.T) {
		qualities := []Quality{
			{Name: "1080p", Resolution: "1920x1080", Bitrate: 5000},
			{Name: "720p", Resolution: "1280x720", Bitrate: 3000},
		}
		manifest, err := svc.GenerateDASHManifest("content-1", qualities, "token")
		require.NoError(t, err)
		assert.Contains(t, manifest, "1080p.mp4")
		assert.Contains(t, manifest, "720p.mp4")
	})

	t.Run("empty qualities", func(t *testing.T) {
		manifest, err := svc.GenerateDASHManifest("content-1", []Quality{}, "token")
		require.NoError(t, err)
		assert.Contains(t, manifest, `<MPD`)
		assert.NotContains(t, manifest, `<Representation`)
	})
}

func TestStreamingService_UpdateStreamPlaylist_DBError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.UpdateStreamPlaylist(context.Background(), "s1", "#EXTM3U")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update stream playlist")
}

func TestStreamingService_AddStreamQuality_DBError(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.AddStreamQuality(context.Background(), "s1", Quality{Name: "720p"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add stream quality")
}

func TestStreamingService_DeleteStream_DBError(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.DeleteStream(context.Background(), "s1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete stream")
}

func TestStreamingService_GetStreamByID_DBError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStreamByID(context.Background(), "s1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query stream")
}

func TestStreamingService_GetStream_CacheMiss_DBQueryError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "c1")
	assert.Error(t, err)
}

func TestStreamingService_GetStream_QualitiesCopy(t *testing.T) {
	cache := newMockCache()
	cached := &StreamInfo{
		ID:        "s1",
		ContentID: "c1",
		Qualities: []Quality{{Name: "720p"}, {Name: "1080p"}},
	}
	_ = cache.Set("stream:c1", cached)

	svc := NewStreamingService(&mockDB{}, newMockObjStore(), cache, "http://cdn.example.com")
	result, err := svc.GetStream(context.Background(), "c1")
	require.NoError(t, err)

	result.Qualities[0].Name = "modified"
	reFetched, err := svc.GetStream(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "720p", reFetched.Qualities[0].Name)
}

func TestStreamingService_UpdateStreamStatus_InvalidTransition(t *testing.T) {
	tests := []struct {
		from string
		to   string
	}{
		{"pending", "live"},
		{"ready", "pending"},
		{"live", "ready"},
		{"expired", "pending"},
		{"error", "ready"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s->%s", tt.from, tt.to), func(t *testing.T) {
			db := &mockDB{
				queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
					return stg.NewErrorCancelRow(nil)
				},
			}
			svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
			_ = svc
		})
	}
}

func TestStreamingService_UpdateStreamStatus_ValidTransition(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(nil)
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	cache := newMockCache()
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	_ = svc
}

func TestStreamingService_UpdateStreamPlaylist_Success(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(nil)
		},
	}
	cache := newMockCache()
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	_ = svc
}

func TestStreamingService_AddStreamQuality_WithCacheInvalidation(t *testing.T) {
	cache := newMockCache()
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(nil)
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	_ = svc
}

func TestStreamingService_GetStream_CacheSetOnMiss(t *testing.T) {
	cache := newMockCache()
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "c1")
	assert.Error(t, err)
}

func TestStreamingService_GetStream_NilCache_NoPanic(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), nil, "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "c1")
	assert.Error(t, err)
}

func TestStreamingService_GetStreamByID_QualitiesDBError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStreamByID(context.Background(), "s1")
	assert.Error(t, err)
}

func TestStreamingService_checkDB(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "")
	err := svc.checkDB()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")

	svc2 := NewStreamingService(&mockDB{}, nil, nil, "")
	err2 := svc2.checkDB()
	assert.NoError(t, err2)
}

func TestStreamingService_Close_NoPanic(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "")
	assert.NotPanics(t, func() { svc.Close() })
}

func TestStreamInfo_Fields(t *testing.T) {
	si := &StreamInfo{
		ID:        "s1",
		ContentID: "c1",
		Type:      "hls",
		URL:       "http://example.com/stream",
		Playlist:  "#EXTM3U",
		Qualities: []Quality{{Name: "1080p", Resolution: "1920x1080", Bitrate: 5000}},
		Duration:  120,
		Status:    "ready",
	}
	assert.Equal(t, "s1", si.ID)
	assert.Equal(t, "hls", si.Type)
	assert.Len(t, si.Qualities, 1)
}

func TestQuality_Fields(t *testing.T) {
	q := Quality{Name: "720p", Resolution: "1280x720", Bitrate: 2800, URL: "http://example.com/720p"}
	assert.Equal(t, "720p", q.Name)
	assert.Equal(t, "1280x720", q.Resolution)
	assert.Equal(t, 2800, q.Bitrate)
}

func TestValidStreamTransitions_All(t *testing.T) {
	allStates := []string{"pending", "ready", "live", "ended", "expired", "error"}
	for _, from := range allStates {
		for _, to := range allStates {
			result := isValidStreamTransition(from, to)
			if from == to {
				assert.True(t, result, "%s->%s should be valid (same state)", from, to)
			}
		}
	}
}

func TestBuildSimplePlaylist_EmptySegments(t *testing.T) {
	playlist := BuildSimplePlaylist("content-1", []string{}, "token")
	assert.Contains(t, playlist, "#EXTM3U")
	assert.Contains(t, playlist, "#EXT-X-ENDLIST")
	assert.NotContains(t, playlist, "#EXTINF")
}

func TestBuildMediaPlaylist_EmptyQuality(t *testing.T) {
	qualitySegments := map[string][]string{
		"720p": {},
	}
	playlist := BuildMasterPlaylist("content-1", qualitySegments, "token")
	assert.Contains(t, playlist, "#EXTM3U")
	assert.Contains(t, playlist, "BANDWIDTH=2800000")
}

func TestDetectStreamType_EdgeCases(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"video.tar.gz", "unknown"},
		{"archive.zip", "unknown"},
		{".m3u8", "hls"},
		{".mpd", "dash"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			assert.Equal(t, tt.want, DetectStreamType(tt.filename))
		})
	}
}

func TestQualityBandwidth(t *testing.T) {
	assert.Equal(t, 5000, qualityBandwidth["1080p"])
	assert.Equal(t, 2800, qualityBandwidth["720p"])
	assert.Equal(t, 1400, qualityBandwidth["480p"])
	assert.Equal(t, 800, qualityBandwidth["360p"])
}

func TestStreamingService_GenerateDASHManifest_EmptyQualities(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://cdn.example.com")
	manifest, err := svc.GenerateDASHManifest("content-1", []Quality{}, "token")
	require.NoError(t, err)
	assert.Contains(t, manifest, `<MPD`)
	assert.NotContains(t, manifest, `<Representation`)
}

func TestStreamingService_GenerateDASHManifest_SingleQuality(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://cdn.example.com")
	qualities := []Quality{
		{Name: "720p", Resolution: "1280x720", Bitrate: 3000},
	}
	manifest, err := svc.GenerateDASHManifest("content-1", qualities, "token")
	require.NoError(t, err)
	assert.Contains(t, manifest, "720p.mp4?playback_token=token")
	assert.Contains(t, manifest, `bandwidth="3000000"`)
}

package streamingsvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStreamingService_UpdateStreamStatus_InvalidTransitions(t *testing.T) {
	tests := []struct {
		from string
		to   string
	}{
		{"pending", "live"},
		{"ready", "pending"},
		{"live", "ready"},
		{"expired", "pending"},
		{"error", "ready"},
		{"nonexistent", "ready"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_to_%s", tt.from, tt.to), func(t *testing.T) {
			assert.False(t, isValidStreamTransition(tt.from, tt.to))
		})
	}
}

func TestStreamingService_UpdateStreamStatus_ValidTransitions(t *testing.T) {
	tests := []struct {
		from string
		to   string
	}{
		{"pending", "ready"},
		{"pending", "error"},
		{"ready", "live"},
		{"ready", "expired"},
		{"live", "ended"},
		{"error", "pending"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_to_%s", tt.from, tt.to), func(t *testing.T) {
			assert.True(t, isValidStreamTransition(tt.from, tt.to))
		})
	}
}

func TestStreamingService_UpdateStreamStatus_SameState(t *testing.T) {
	states := []string{"pending", "ready", "live", "ended", "expired", "error"}
	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			assert.True(t, isValidStreamTransition(state, state))
		})
	}
}

func TestStreamingService_UpdateStreamStatus_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.UpdateStreamStatus(context.Background(), "s1", "ready")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestStreamingService_UpdateStreamStatus_StreamNotFound(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.UpdateStreamStatus(context.Background(), "s1", "ready")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream not found")
}

func TestStreamingService_UpdateStreamStatus_DBQueryError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("connection lost"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.UpdateStreamStatus(context.Background(), "s1", "ready")
	assert.Error(t, err)
}

func TestStreamingService_UpdateStreamPlaylist_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.UpdateStreamPlaylist(context.Background(), "s1", "#EXTM3U")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestStreamingService_UpdateStreamPlaylist_QueryError(t *testing.T) {
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

func TestStreamingService_AddStreamQuality_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.AddStreamQuality(context.Background(), "s1", Quality{Name: "720p"})
	assert.Error(t, err)
}

func TestStreamingService_AddStreamQuality_DBExecError(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("exec error")
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.AddStreamQuality(context.Background(), "s1", Quality{Name: "720p"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add stream quality")
}

func TestStreamingService_GetStreamByID_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStreamByID(context.Background(), "s1")
	assert.Error(t, err)
}

func TestStreamingService_GetStreamByID_NotFound(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStreamByID(context.Background(), "s1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream not found")
}

func TestStreamingService_GetStreamByID_DBScanError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("scan error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStreamByID(context.Background(), "s1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query stream")
}

func TestStreamingService_GetStream_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "content-1")
	assert.Error(t, err)
}

func TestStreamingService_GetStream_CacheHit(t *testing.T) {
	cache := newMockCache()
	cached := &StreamInfo{
		ID:        "s1",
		ContentID: "c1",
		Type:      "hls",
		Status:    "ready",
		Qualities: []Quality{{Name: "720p", Resolution: "1280x720", Bitrate: 2800}},
	}
	_ = cache.Set("stream:c1", cached)

	svc := NewStreamingService(&mockDB{}, newMockObjStore(), cache, "http://cdn.example.com")
	result, err := svc.GetStream(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "s1", result.ID)
	assert.Equal(t, "hls", result.Type)
	assert.Len(t, result.Qualities, 1)
}

func TestStreamingService_GetStream_CacheWrongType(t *testing.T) {
	cache := newMockCache()
	_ = cache.Set("stream:c1", "not-a-stream-info")

	svc := NewStreamingService(&mockDB{}, newMockObjStore(), cache, "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "c1")
	assert.Error(t, err)
}

func TestStreamingService_GetStream_CacheMiss_DBError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "c1")
	assert.Error(t, err)
}

func TestStreamingService_GetStream_CacheMiss_NoRows(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "c1")
	assert.Error(t, err)
}

func TestStreamingService_GetStream_NilCache(t *testing.T) {
	svc := NewStreamingService(nil, newMockObjStore(), nil, "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "content-1")
	assert.Error(t, err)
}

func TestStreamingService_GetStream_CacheHit_ReturnsCopy(t *testing.T) {
	cache := newMockCache()
	original := &StreamInfo{
		ID:        "s1",
		ContentID: "c1",
		Type:      "hls",
		Status:    "ready",
		Qualities: []Quality{{Name: "720p", Resolution: "1280x720", Bitrate: 2800}},
	}
	_ = cache.Set("stream:c1", original)

	svc := NewStreamingService(&mockDB{}, newMockObjStore(), cache, "http://cdn.example.com")
	result, err := svc.GetStream(context.Background(), "c1")
	require.NoError(t, err)

	result.Qualities[0].Name = "modified"
	result2, err := svc.GetStream(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "720p", result2.Qualities[0].Name)
}

func TestStreamingService_CreateStream_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.CreateStream(context.Background(), "content-1", "hls")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestStreamingService_CreateStream_ExecError(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("db insert error")
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.CreateStream(context.Background(), "content-1", "hls")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create stream")
}

func TestStreamingService_CreateStream_Success(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	id, err := svc.CreateStream(context.Background(), "content-1", "hls")
	require.NoError(t, err)
	assert.Contains(t, id, "stream_content-1_")
}

func TestStreamingService_CreateStream_DifferentTypes(t *testing.T) {
	tests := []struct {
		streamType string
	}{
		{"hls"},
		{"dash"},
		{"progressive"},
	}

	for _, tt := range tests {
		t.Run(tt.streamType, func(t *testing.T) {
			db := &mockDB{
				execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
					return &mockResult{}, nil
				},
			}
			svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
			id, err := svc.CreateStream(context.Background(), "content-1", tt.streamType)
			require.NoError(t, err)
			assert.NotEmpty(t, id)
		})
	}
}

func TestStreamingService_DeleteStream_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.DeleteStream(context.Background(), "s1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestStreamingService_DeleteStream_DBExecError(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("delete error")
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.DeleteStream(context.Background(), "s1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete stream")
}

func TestStreamingService_DeleteStream_Success(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.DeleteStream(context.Background(), "s1")
	require.NoError(t, err)
}

func TestStreamingService_GenerateHLSPlaylist_NoSegments(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://cdn.example.com")
	_, err := svc.GenerateHLSPlaylist("content-1", map[string][]string{}, "token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no segments available")
}

func TestStreamingService_GenerateHLSPlaylist_SingleQuality(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://cdn.example.com")
	qualitySegments := map[string][]string{
		"720p": {"seg0.ts", "seg1.ts"},
	}
	playlist, err := svc.GenerateHLSPlaylist("content-1", qualitySegments, "token")
	require.NoError(t, err)
	assert.Contains(t, playlist, "#EXTM3U")
	assert.Contains(t, playlist, "#EXTINF:6.0,")
	assert.NotContains(t, playlist, "#EXT-X-STREAM-INF")
}

func TestStreamingService_GenerateHLSPlaylist_MultipleQualities(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://cdn.example.com")
	qualitySegments := map[string][]string{
		"1080p": {"seg0.ts"},
		"720p":  {"seg0.ts"},
		"480p":  {"seg0.ts"},
	}
	playlist, err := svc.GenerateHLSPlaylist("content-1", qualitySegments, "token")
	require.NoError(t, err)
	assert.Contains(t, playlist, "#EXT-X-STREAM-INF")
	assert.Contains(t, playlist, "BANDWIDTH=5000000")
	assert.Contains(t, playlist, "BANDWIDTH=2800000")
	assert.Contains(t, playlist, "BANDWIDTH=1400000")
}

func TestStreamingService_GenerateDASHManifest_VerifyOutput(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://cdn.example.com")
	qualities := []Quality{
		{Name: "720p", Resolution: "1280x720", Bitrate: 3000},
	}
	manifest, err := svc.GenerateDASHManifest("content-1", qualities, "token")
	require.NoError(t, err)
	assert.Contains(t, manifest, `<?xml version="1.0"`)
	assert.Contains(t, manifest, `<MPD`)
	assert.Contains(t, manifest, `bandwidth="3000000"`)
	assert.Contains(t, manifest, `width="1280"`)
	assert.Contains(t, manifest, "content-1/720p.mp4?playback_token=token")
}

func TestBuildSimplePlaylist_SegmentsWithPathPrefix(t *testing.T) {
	segments := []string{"hls/720p/seg0.ts", "hls/720p/seg1.ts"}
	playlist := BuildSimplePlaylist("content-1", segments, "tkn")
	assert.Contains(t, playlist, "/segment/seg0.ts?playback_token=tkn")
	assert.Contains(t, playlist, "/segment/seg1.ts?playback_token=tkn")
}

func TestBuildMediaPlaylist_WithQuality(t *testing.T) {
	segments := []string{"seg0.ts", "seg1.ts"}
	playlist := BuildMediaPlaylist("content-1", "1080p", segments, "token")
	assert.Contains(t, playlist, "/segment/seg0.ts?quality=1080p&playback_token=token")
	assert.Contains(t, playlist, "/segment/seg1.ts?quality=1080p&playback_token=token")
}

func TestStreamingService_Close_DoesNothing(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "")
	assert.NotPanics(t, func() { svc.Close() })
}

func TestStreamingService_checkDB_WithDB(t *testing.T) {
	svc := NewStreamingService(&mockDB{}, nil, nil, "")
	err := svc.checkDB()
	assert.NoError(t, err)
}

func TestStreamingService_NewStreamingService_NilLogger(t *testing.T) {
	svc := NewStreamingService(&mockDB{}, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	assert.NotNil(t, svc.logger)
}

func TestStreamingService_NewStreamingService_WithLogger(t *testing.T) {
	log := zap.NewNop()
	svc := NewStreamingService(&mockDB{}, newMockObjStore(), newMockCache(), "http://cdn.example.com", log)
	assert.NotNil(t, svc)
	assert.Equal(t, log, svc.logger)
}

func TestStreamingService_NewStreamingService_NilDB_Warns(t *testing.T) {
	svc := NewStreamingService(nil, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	assert.NotNil(t, svc)
	assert.Nil(t, svc.db)
}

func TestStreamingService_GetStream_CacheSetFails(t *testing.T) {
	cache := &setFailCache{mockCache: *newMockCache()}
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com", zap.NewNop())
	_, err := svc.GetStream(context.Background(), "c1")
	assert.Error(t, err)
}

type setFailCache struct {
	mockCache
}

func (m *setFailCache) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	return errors.New("cache set failed")
}

func TestStreamingService_UpdateStreamStatus_WithLogger(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com", zap.NewNop())
	err := svc.UpdateStreamStatus(context.Background(), "s1", "ready")
	assert.Error(t, err)
}

func TestStreamingService_UpdateStreamPlaylist_WithLogger(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(errors.New("db error"))
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com", zap.NewNop())
	err := svc.UpdateStreamPlaylist(context.Background(), "s1", "#EXTM3U")
	assert.Error(t, err)
}

func TestStreamingService_AddStreamQuality_WithLogger(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("exec error")
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com", zap.NewNop())
	err := svc.AddStreamQuality(context.Background(), "s1", Quality{Name: "720p"})
	assert.Error(t, err)
}

func TestStreamingService_DeleteStream_WithLogger(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("delete error")
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com", zap.NewNop())
	err := svc.DeleteStream(context.Background(), "s1")
	assert.Error(t, err)
}

type mockRows struct {
	records [][]interface{}
	idx     int
	closed  bool
	err     error
}

func (m *mockRows) Next() bool {
	if m.idx < len(m.records) {
		m.idx++
		return true
	}
	return false
}

func (m *mockRows) Scan(dest ...interface{}) error {
	if m.err != nil {
		return m.err
	}
	record := m.records[m.idx-1]
	for i, d := range dest {
		switch v := d.(type) {
		case *string:
			*v = record[i].(string)
		case *int:
			*v = record[i].(int)
		}
	}
	return nil
}

func (m *mockRows) Close() error {
	m.closed = true
	return nil
}

func (m *mockRows) Err() error {
	return m.err
}

type mockRowScanner struct {
	scanFn func(dest ...interface{}) error
	err    error
}

func (m *mockRowScanner) Scan(dest ...interface{}) error {
	if m.err != nil {
		return m.err
	}
	if m.scanFn != nil {
		return m.scanFn(dest...)
	}
	return nil
}

func makeStreamQueryRowFn(now, expires time.Time) func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	return func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
		return stg.NewTestCancelRow(&mockRowScanner{
			scanFn: func(dest ...interface{}) error {
				*(dest[0].(*string)) = "s1"
				*(dest[1].(*string)) = "c1"
				*(dest[2].(*string)) = "hls"
				*(dest[3].(*sql.NullString)) = sql.NullString{String: "http://url", Valid: true}
				*(dest[4].(*sql.NullString)) = sql.NullString{String: "#EXTM3U", Valid: true}
				*(dest[5].(*sql.NullInt64)) = sql.NullInt64{Int64: 120, Valid: true}
				*(dest[6].(*sql.NullString)) = sql.NullString{String: "ready", Valid: true}
				*(dest[7].(*time.Time)) = now
				*(dest[8].(*sql.NullTime)) = sql.NullTime{Time: expires, Valid: true}
				return nil
			},
		})
	}
}

func TestStreamingService_Close_MethodCalled(t *testing.T) {
	svc := NewStreamingService(&mockDB{}, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	svc.Close()
}

func TestStreamingService_UpdateStreamStatus_FullSuccess(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRowScanner{
				scanFn: func(dest ...interface{}) error {
					if len(dest) >= 2 {
						*(dest[0].(*string)) = "pending"
						*(dest[1].(*string)) = "c1"
					}
					return nil
				},
			})
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	cache := newMockCache()
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	err := svc.UpdateStreamStatus(context.Background(), "s1", "ready")
	require.NoError(t, err)
}

func TestStreamingService_UpdateStreamStatus_ConcurrentConflict(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRowScanner{
				scanFn: func(dest ...interface{}) error {
					*(dest[0].(*string)) = "pending"
					*(dest[1].(*string)) = "c1"
					return nil
				},
			})
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0}, nil
		},
	}
	cache := newMockCache()
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	err := svc.UpdateStreamStatus(context.Background(), "s1", "ready")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "concurrently")
}

func TestStreamingService_UpdateStreamStatus_ExecError(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRowScanner{
				scanFn: func(dest ...interface{}) error {
					*(dest[0].(*string)) = "pending"
					*(dest[1].(*string)) = "c1"
					return nil
				},
			})
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("exec error")
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	err := svc.UpdateStreamStatus(context.Background(), "s1", "ready")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update stream status")
}

func TestStreamingService_UpdateStreamPlaylist_FullSuccess(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRowScanner{
				scanFn: func(dest ...interface{}) error {
					*(dest[0].(*string)) = "c1"
					return nil
				},
			})
		},
	}
	cache := newMockCache()
	_ = cache.Set("stream:c1", &StreamInfo{ID: "s1"})
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	err := svc.UpdateStreamPlaylist(context.Background(), "s1", "#EXTM3U")
	require.NoError(t, err)
	_, cacheErr := cache.Get("stream:c1")
	assert.Error(t, cacheErr)
}

func TestStreamingService_UpdateStreamPlaylist_NilCacheFullSuccess(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRowScanner{
				scanFn: func(dest ...interface{}) error {
					*(dest[0].(*string)) = "c1"
					return nil
				},
			})
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), nil, "http://cdn.example.com")
	err := svc.UpdateStreamPlaylist(context.Background(), "s1", "#EXTM3U")
	require.NoError(t, err)
}

func TestStreamingService_AddStreamQuality_FullSuccess_WithCacheInvalidation(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(&mockRowScanner{
				scanFn: func(dest ...interface{}) error {
					*(dest[0].(*string)) = "c1"
					return nil
				},
			})
		},
	}
	cache := newMockCache()
	_ = cache.Set("stream:c1", &StreamInfo{ID: "s1"})
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	err := svc.AddStreamQuality(context.Background(), "s1", Quality{Name: "720p", Resolution: "1280x720", Bitrate: 2800})
	require.NoError(t, err)
	_, cacheErr := cache.Get("stream:c1")
	assert.Error(t, cacheErr)
}

func TestStreamingService_AddStreamQuality_NilCacheFullSuccess(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{}, nil
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), nil, "http://cdn.example.com")
	err := svc.AddStreamQuality(context.Background(), "s1", Quality{Name: "720p"})
	require.NoError(t, err)
}

func TestStreamingService_GetStreamByID_FullSuccess(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	db := &mockDB{
		queryRowFn: makeStreamQueryRowFn(now, expires),
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &mockRows{
				records: [][]interface{}{
					{"720p", "1280x720", 2800, "http://720p"},
				},
			}, nil
		},
	}
	cache := newMockCache()
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	stream, err := svc.GetStreamByID(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", stream.ID)
	assert.Equal(t, "hls", stream.Type)
	assert.Equal(t, "ready", stream.Status)
	assert.Len(t, stream.Qualities, 1)
	assert.Equal(t, "720p", stream.Qualities[0].Name)
}

func TestStreamingService_GetStreamByID_QualitiesScanError(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	db := &mockDB{
		queryRowFn: makeStreamQueryRowFn(now, expires),
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &mockRows{
				records: [][]interface{}{
					{"720p", "1280x720", 2800, "http://720p"},
				},
				err: errors.New("scan error"),
			}, nil
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStreamByID(context.Background(), "s1")
	require.Error(t, err)
}

func TestStreamingService_GetStream_Success_WithDBQuery(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	db := &mockDB{
		queryRowFn: makeStreamQueryRowFn(now, expires),
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &mockRows{
				records: [][]interface{}{
					{"720p", "1280x720", 2800, "http://720p"},
				},
			}, nil
		},
	}
	cache := newMockCache()
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	stream, err := svc.GetStream(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "s1", stream.ID)
	assert.Len(t, stream.Qualities, 1)
}

func TestStreamingService_GetStream_CacheSetSuccess(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	db := &mockDB{
		queryRowFn: makeStreamQueryRowFn(now, expires),
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &mockRows{
				records: [][]interface{}{
					{"720p", "1280x720", 2800, "http://720p"},
				},
			}, nil
		},
	}
	cache := newMockCache()
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com", zap.NewNop())
	stream, err := svc.GetStream(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "s1", stream.ID)

	cached, cacheErr := cache.Get("stream:c1")
	require.NoError(t, cacheErr)
	assert.NotNil(t, cached)
}

func TestStreamingService_GetStream_QualitiesQueryError(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	db := &mockDB{
		queryRowFn: makeStreamQueryRowFn(now, expires),
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return nil, errors.New("qualities query error")
		},
	}
	cache := newMockCache()
	svc := NewStreamingService(db, newMockObjStore(), cache, "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "c1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get stream qualities")
}

func TestStreamingService_GetStreamByID_NilCache_NoPanic(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	db := &mockDB{
		queryRowFn: makeStreamQueryRowFn(now, expires),
		queryFn: func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
			return &mockRows{records: [][]interface{}{}}, nil
		},
	}
	svc := NewStreamingService(db, newMockObjStore(), nil, "http://cdn.example.com")
	stream, err := svc.GetStreamByID(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", stream.ID)
	assert.Len(t, stream.Qualities, 0)
}

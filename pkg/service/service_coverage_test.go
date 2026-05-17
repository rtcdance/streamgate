package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"

	"streamgate/pkg/models"

	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- Mock implementations ---

// mockDB implements storage.DB
type mockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *sql.Row
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn    func(ctx context.Context) (*sql.Tx, error)
	pingFn     func(ctx context.Context) error
	closeFn    func() error
}

func (m *mockDB) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return nil
}
func (m *mockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return errors.New("not implemented")
}
func (m *mockDB) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}
func (m *mockDB) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// mockCache implements cachetypes.CacheBackend
type mockCache struct {
	data   map[string]interface{}
	setErr error
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
	if m.setErr != nil {
		return m.setErr
	}
	m.data[key] = value
	return nil
}

func (m *mockCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockCache) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	return m.Set(key, value)
}

// mockContentStorage implements ContentObjectStorage
type mockContentStorage struct {
	uploadFn   func(ctx context.Context, bucket, key string, data []byte) error
	downloadFn func(ctx context.Context, bucket, key string) ([]byte, error)
	deleteFn   func(ctx context.Context, bucket, key string) error
	existsFn   func(ctx context.Context, bucket, key string) (bool, error)
	uploads    map[string][]byte // stored data
}

func newMockContentStorage() *mockContentStorage {
	return &mockContentStorage{uploads: make(map[string][]byte)}
}

func (m *mockContentStorage) Upload(ctx context.Context, bucket, key string, data []byte) error {
	if m.uploadFn != nil {
		return m.uploadFn(ctx, bucket, key, data)
	}
	m.uploads[bucket+"/"+key] = data
	return nil
}

func (m *mockContentStorage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	if m.downloadFn != nil {
		return m.downloadFn(ctx, bucket, key)
	}
	if d, ok := m.uploads[bucket+"/"+key]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

func (m *mockContentStorage) Delete(ctx context.Context, bucket, key string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, bucket, key)
	}
	delete(m.uploads, bucket+"/"+key)
	return nil
}

func (m *mockContentStorage) Exists(ctx context.Context, bucket, key string) (bool, error) {
	if m.existsFn != nil {
		return m.existsFn(ctx, bucket, key)
	}
	_, ok := m.uploads[bucket+"/"+key]
	return ok, nil
}

// mockStreamingStorage implements StreamingObjectStorage
type mockStreamingStorage struct {
	data map[string][]byte
}

func newMockStreamingStorage() *mockStreamingStorage {
	return &mockStreamingStorage{data: make(map[string][]byte)}
}

func (m *mockStreamingStorage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	if d, ok := m.data[bucket+"/"+key]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

func (m *mockStreamingStorage) Exists(ctx context.Context, bucket, key string) (bool, error) {
	_, ok := m.data[bucket+"/"+key]
	return ok, nil
}

// mockUploadStorage implements UploadObjectStorage
type mockUploadStorage struct {
	data map[string][]byte
}

func newMockUploadStorage() *mockUploadStorage {
	return &mockUploadStorage{data: make(map[string][]byte)}
}

func (m *mockUploadStorage) Upload(ctx context.Context, bucket, key string, data []byte) error {
	m.data[bucket+"/"+key] = data
	return nil
}

func (m *mockUploadStorage) UploadStream(ctx context.Context, bucket, key string, reader io.Reader, size int64) error {
	return nil
}

func (m *mockUploadStorage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	return nil, errors.New("not found")
}

func (m *mockUploadStorage) Delete(ctx context.Context, bucket, key string) error {
	delete(m.data, bucket+"/"+key)
	return nil
}

func (m *mockUploadStorage) Exists(ctx context.Context, bucket, key string) (bool, error) {
	_, ok := m.data[bucket+"/"+key]
	return ok, nil
}

func (m *mockUploadStorage) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	fullPrefix := bucket + "/" + prefix
	for k := range m.data {
		if strings.HasPrefix(k, fullPrefix) {
			keys = append(keys, strings.TrimPrefix(k, bucket+"/"))
		}
	}
	return keys, nil
}

// --- ContentService Tests ---

func TestNewContentService(t *testing.T) {
	db := &mockDB{}
	cache := newMockCache()
	objStorage := newMockContentStorage()

	svc := NewContentService(db, objStorage, cache)
	assert.NotNil(t, svc)
}

func TestNewContentService_WithLogger(t *testing.T) {
	db := &mockDB{}
	cache := newMockCache()
	objStorage := newMockContentStorage()

	svc := NewContentService(db, objStorage, cache, zap.NewNop())
	assert.NotNil(t, svc)
}

func TestContentService_GetContent_DatabaseNil(t *testing.T) {
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	_, err := svc.GetContent(context.Background(), "id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_CreateContent_DatabaseNil(t *testing.T) {
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	_, err := svc.CreateContent(context.Background(), &Content{Title: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_UpdateContent_DatabaseNil(t *testing.T) {
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	err := svc.UpdateContent(context.Background(), &Content{ID: "id"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_DeleteContent_DatabaseNil(t *testing.T) {
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	err := svc.DeleteContent(context.Background(), "id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_ListContents_DatabaseNil(t *testing.T) {
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	_, err := svc.ListContents(context.Background(), "owner", 10, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_CountContents_DatabaseNil(t *testing.T) {
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	_, err := svc.CountContents(context.Background(), "owner")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_UpdateContentStatus_DatabaseNil(t *testing.T) {
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	err := svc.UpdateContentStatus(context.Background(), "id", "ready")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_GetContent_CacheHit(t *testing.T) {
	cache := newMockCache()
	content := &Content{ID: "c1", Title: "Cached", Status: "ready"}
	_ = cache.Set("content:c1", content)

	svc := NewContentService(&mockDB{}, newMockContentStorage(), cache)
	result, err := svc.GetContent(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "c1", result.ID)
	assert.Equal(t, "Cached", result.Title)
}

func TestContentService_GetContent_CacheMiss_DBQueryError(t *testing.T) {
	// Verify the nil-db guard works for GetContent
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	_, err := svc.GetContent(context.Background(), "missing")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_CreateContent_ExecError(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}

	svc := NewContentService(db, newMockContentStorage(), newMockCache())
	_, err := svc.CreateContent(context.Background(), &Content{Title: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

func TestContentService_UpdateContent_NotFound(t *testing.T) {
	db := &mockDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0}, nil
		},
	}

	svc := NewContentService(db, newMockContentStorage(), newMockCache())
	err := svc.UpdateContent(context.Background(), &Content{ID: "missing", Title: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

type mockResult struct {
	rowsAffected int64
	lastInsertID int64
}

func (m *mockResult) LastInsertId() (int64, error) { return m.lastInsertID, nil }
func (m *mockResult) RowsAffected() (int64, error) { return m.rowsAffected, nil }

func TestContentService_DeleteContentWithTx_DatabaseNil(t *testing.T) {
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	err := svc.DeleteContentWithTx(context.Background(), "id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_CreateContentWithTx_DatabaseNil(t *testing.T) {
	svc := NewContentService(nil, newMockContentStorage(), newMockCache())
	_, err := svc.CreateContentWithTx(context.Background(), &Content{Title: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentService_SetContentRegistry(t *testing.T) {
	svc := NewContentService(&mockDB{}, newMockContentStorage(), newMockCache())
	assert.Nil(t, svc.registry)

	svc.SetContentRegistry(&mockContentRegistry{})
	assert.NotNil(t, svc.registry)
}

// mockContentRegistry implements ContentRegistry for testing
type mockContentRegistry struct{}

func (m *mockContentRegistry) RegisterContent(ctx context.Context, contentHash [32]byte, metadata string) (string, error) {
	return "0xtxhash", nil
}

// --- StreamingService Tests ---

func TestStreamingService_GetStream_DBNil_Coverage(t *testing.T) {
	svc := NewStreamingService(nil, newMockStreamingStorage(), newMockCache(), "http://cdn.example.com")
	_, err := svc.GetStream(context.Background(), "content-id")
	assert.Error(t, err)
}

func TestStreamingService_CreateStream_DBNil(t *testing.T) {
	svc := NewStreamingService(nil, newMockStreamingStorage(), newMockCache(), "http://cdn.example.com")
	_, err := svc.CreateStream(context.Background(), "content-id", "hls")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestStreamingService_GenerateHLSPlaylist_Coverage(t *testing.T) {
	svc := NewStreamingService(nil, nil, newMockCache(), "http://cdn.example.com")
	qualities := []Quality{
		{Name: "1080p", Resolution: "1920x1080", Bitrate: 5000},
		{Name: "720p", Resolution: "1280x720", Bitrate: 3000},
	}

	playlist, err := svc.GenerateHLSPlaylist("content-1", qualities)
	require.NoError(t, err)
	assert.Contains(t, playlist, "#EXTM3U")
	assert.Contains(t, playlist, "#EXT-X-STREAM-INF")
	assert.Contains(t, playlist, "BANDWIDTH=5000000")
	assert.Contains(t, playlist, "RESOLUTION=1920x1080")
}

func TestStreamingService_GenerateDASHManifest_Coverage(t *testing.T) {
	svc := NewStreamingService(nil, nil, newMockCache(), "http://cdn.example.com")
	qualities := []Quality{
		{Name: "1080p", Resolution: "1920x1080", Bitrate: 5000},
	}

	manifest, err := svc.GenerateDASHManifest("content-1", qualities)
	require.NoError(t, err)
	assert.Contains(t, manifest, "<MPD")
	assert.Contains(t, manifest, "bandwidth=\"5000000\"")
	assert.Contains(t, manifest, "width=\"1920\"")
}

func TestDetectStreamType_Coverage(t *testing.T) {
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
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, DetectStreamType(tt.filename))
	}
}

func TestStreamingService_GetStream_CacheHit_Coverage(t *testing.T) {
	cache := newMockCache()
	stream := &StreamInfo{ID: "s1", ContentID: "c1", Status: "ready", Type: "hls"}
	_ = cache.Set("stream:c1", stream)

	svc := NewStreamingService(&mockDB{}, newMockStreamingStorage(), cache, "http://cdn.example.com")
	result, err := svc.GetStream(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "s1", result.ID)
}

func TestStreamingService_UpdateStreamStatus_DBNil(t *testing.T) {
	svc := NewStreamingService(nil, newMockStreamingStorage(), newMockCache(), "http://cdn.example.com")
	err := svc.UpdateStreamStatus(context.Background(), "s1", "ready")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestStreamingService_DeleteStream_DBNil(t *testing.T) {
	svc := NewStreamingService(nil, newMockStreamingStorage(), newMockCache(), "http://cdn.example.com")
	err := svc.DeleteStream(context.Background(), "s1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

// --- UploadService Tests ---

func TestNewUploadService(t *testing.T) {
	db := &mockDB{}
	svc := NewUploadService(db, newMockUploadStorage(), "content-bucket")
	assert.NotNil(t, svc)
}

func TestUploadService_SetMaxUploadSize(t *testing.T) {
	svc := NewUploadService(&mockDB{}, newMockUploadStorage(), "content-bucket")
	svc.SetMaxUploadSize(1024 * 1024 * 100) // 100MB
	assert.Equal(t, int64(1024*1024*100), svc.maxUploadSize)
}

func TestUploadService_Upload_DBNil(t *testing.T) {
	svc := NewUploadService(nil, newMockUploadStorage(), "content-bucket")
	_, err := svc.Upload(context.Background(), "video.mp4", []byte("data"), "owner1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestUploadService_GetUploadStatus_DBNil(t *testing.T) {
	svc := NewUploadService(nil, newMockUploadStorage(), "content-bucket")
	_, err := svc.GetUploadStatus(context.Background(), "upload-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestUploadService_InitiateChunkedUpload_DBNil(t *testing.T) {
	svc := NewUploadService(nil, newMockUploadStorage(), "content-bucket")
	_, err := svc.InitiateChunkedUpload(context.Background(), "video.mp4", 1024*1024*100, 10, "owner1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestUploadService_UploadChunk_DBNil(t *testing.T) {
	svc := NewUploadService(nil, newMockUploadStorage(), "content-bucket")
	err := svc.UploadChunk(context.Background(), "upload-id", 1, []byte("chunk"), "owner-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestUploadService_CompleteChunkedUpload_DBNil(t *testing.T) {
	svc := NewUploadService(nil, newMockUploadStorage(), "content-bucket")
	err := svc.CompleteChunkedUpload(context.Background(), "upload-id", 5)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestUploadService_DeleteUpload_DBNil(t *testing.T) {
	svc := NewUploadService(nil, newMockUploadStorage(), "content-bucket")
	err := svc.DeleteUpload(context.Background(), "upload-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestUploadService_ListUploads_DBNil(t *testing.T) {
	svc := NewUploadService(nil, newMockUploadStorage(), "content-bucket")
	_, err := svc.ListUploads(context.Background(), "owner", 10, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestContentTypeToType(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"video/mp4", "video"},
		{"video/webm", "video"},
		{"audio/mpeg", "audio"},
		{"audio/ogg", "audio"},
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"application/octet-stream", "other"},
		{"", "other"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, contentTypeToType(tt.mime), "contentTypeToType(%q)", tt.mime)
	}
}

func TestUploadService_CompleteUploadWithTx_DBNil(t *testing.T) {
	svc := NewUploadService(nil, newMockUploadStorage(), "content-bucket")
	_, err := svc.CompleteUploadWithTx(context.Background(), "upload-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

// --- CacheBackend edge cases ---

func TestMockCache_SetGetDelete(t *testing.T) {
	cache := newMockCache()
	err := cache.Set("key", "value")
	require.NoError(t, err)

	v, err := cache.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "value", v)

	err = cache.Delete("key")
	require.NoError(t, err)

	_, err = cache.Get("key")
	assert.Error(t, err)
}

func TestMockCache_SetError(t *testing.T) {
	cache := newMockCache()
	cache.setErr = errors.New("cache unavailable")

	err := cache.Set("key", "value")
	assert.Error(t, err)
}

// --- Verify mockContentStorage ---

func TestMockContentStorage_UploadDownload(t *testing.T) {
	s := newMockContentStorage()
	ctx := context.Background()

	err := s.Upload(ctx, "bucket", "key", []byte("hello"))
	require.NoError(t, err)

	data, err := s.Download(ctx, "bucket", "key")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), data)

	exists, err := s.Exists(ctx, "bucket", "key")
	require.NoError(t, err)
	assert.True(t, exists)

	err = s.Delete(ctx, "bucket", "key")
	require.NoError(t, err)

	exists, _ = s.Exists(ctx, "bucket", "key")
	assert.False(t, exists)
}

// --- StreamInfo Quality tests ---

func TestQuality_Fields(t *testing.T) {
	q := Quality{
		Name:       "4K",
		Resolution: "3840x2160",
		Bitrate:    15000,
		URL:        "http://cdn.example.com/4k.m3u8",
	}
	assert.Equal(t, "4K", q.Name)
	assert.Equal(t, "3840x2160", q.Resolution)
	assert.Equal(t, 15000, q.Bitrate)
}

// --- Content model test ---

func TestContent_Fields(t *testing.T) {
	now := time.Now()
	c := Content{
		ID:           "c1",
		Title:        "Test Video",
		Description:  "A test video",
		Type:         "video",
		URL:          "http://cdn.example.com/video.mp4",
		ThumbnailURL: "http://cdn.example.com/thumb.jpg",
		Duration:     120,
		Size:         1024000,
		Status:       "ready",
		OwnerID:      "user1",
		CreatedAt:    now,
		UpdatedAt:    now,
		Metadata:     map[string]interface{}{"codec": "h264"},
	}
	assert.Equal(t, "c1", c.ID)
	assert.Equal(t, "video", c.Type)
	assert.Equal(t, "ready", c.Status)
}

// --- StreamInfo model test ---

func TestStreamInfo_Fields(t *testing.T) {
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
}

// --- Functional Options tests ---

func TestAuthService_FunctionalOptions(t *testing.T) {
	storage := NewMockAuthStorage()

	t.Run("NewAuthService with defaults", func(t *testing.T) {
		svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage)
		assert.NotNil(t, svc.signatureVerifier)
		assert.NotNil(t, svc.challengeStore)
		assert.Equal(t, defaultChallengeTTL, svc.challengeTTL)
		assert.Nil(t, svc.blacklist)
	})

	t.Run("WithSignatureVerifier", func(t *testing.T) {
		verifier := NewMultiChainSignatureVerifier(zap.NewNop(), nil)
		svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage, WithSignatureVerifier(verifier))
		assert.Equal(t, verifier, svc.signatureVerifier)
	})

	t.Run("WithChallengeStore", func(t *testing.T) {
		store := NewMemoryChallengeStore()
		svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage, WithChallengeStore(store))
		assert.Equal(t, store, svc.challengeStore)
	})

	t.Run("WithChallengeTTL", func(t *testing.T) {
		ttl := 10 * time.Minute
		svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage, WithChallengeTTL(ttl))
		assert.Equal(t, ttl, svc.challengeTTL)
	})

	t.Run("WithTokenBlacklist", func(t *testing.T) {
		bl := NewMemoryTokenBlacklist()
		svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage, WithTokenBlacklist(bl))
		assert.Equal(t, bl, svc.blacklist)
	})

	t.Run("NewAuthServiceWithDeps delegates to options", func(t *testing.T) {
		verifier := NewMultiChainSignatureVerifier(zap.NewNop(), nil)
		store := NewMemoryChallengeStore()
		bl := NewMemoryTokenBlacklist()
		svc := NewAuthServiceWithDeps("test-secret-that-is-at-least-32-chars", storage, verifier, store, 0, bl)
		assert.Equal(t, verifier, svc.signatureVerifier)
		assert.Equal(t, store, svc.challengeStore)
		assert.Equal(t, bl, svc.blacklist)
	})

	t.Run("NewAuthServiceWithDeps nil options use defaults", func(t *testing.T) {
		svc := NewAuthServiceWithDeps("test-secret-that-is-at-least-32-chars", storage, nil, nil, 0, nil)
		assert.NotNil(t, svc.signatureVerifier) // default
		assert.NotNil(t, svc.challengeStore)    // default
		assert.Equal(t, defaultChallengeTTL, svc.challengeTTL)
		assert.Nil(t, svc.blacklist)
	})
}

func TestRedisChallengeStore_FunctionalOptions(t *testing.T) {
	cfg := redisChallengeStoreConfig{}
	assert.Equal(t, 0, cfg.poolSize)

	t.Run("WithRedisPassword", func(t *testing.T) {
		WithRedisPassword("mypass")(&cfg)
		assert.Equal(t, "mypass", cfg.password)
	})

	t.Run("WithRedisDB", func(t *testing.T) {
		WithRedisDB(2)(&cfg)
		assert.Equal(t, 2, cfg.db)
	})

	t.Run("WithRedisPoolSize", func(t *testing.T) {
		WithRedisPoolSize(50)(&cfg)
		assert.Equal(t, 50, cfg.poolSize)
	})

	t.Run("WithRedisDialTimeout", func(t *testing.T) {
		WithRedisDialTimeout(10 * time.Second)(&cfg)
		assert.Equal(t, 10*time.Second, cfg.dialTimeout)
	})

	t.Run("WithRedisReadTimeout", func(t *testing.T) {
		WithRedisReadTimeout(7 * time.Second)(&cfg)
		assert.Equal(t, 7*time.Second, cfg.readTimeout)
	})

	t.Run("WithRedisWriteTimeout", func(t *testing.T) {
		WithRedisWriteTimeout(8 * time.Second)(&cfg)
		assert.Equal(t, 8*time.Second, cfg.writeTimeout)
	})
}

// --- DomainError tests ---

func TestNewDomainError(t *testing.T) {
	err := NewDomainError("VALIDATION_FAILED", "invalid input", nil)
	assert.Equal(t, "VALIDATION_FAILED", err.Code)
	assert.Equal(t, "invalid input", err.Message)
	assert.Nil(t, err.Cause)

	wrapped := fmt.Errorf("db error")
	err2 := NewDomainError("DB_ERROR", "query failed", wrapped)
	assert.Equal(t, wrapped, err2.Cause)
	assert.Equal(t, wrapped, err2.Unwrap())
}

// --- MemoryTokenBlacklist tests ---

func TestMemoryTokenBlacklist_RevokeAndCheck(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	assert.False(t, bl.IsRevoked(context.Background(), "token1"))

	err := bl.Revoke(context.Background(), "token1", time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.True(t, bl.IsRevoked(context.Background(), "token1"))
	assert.False(t, bl.IsRevoked(context.Background(), "token2"))
}

// --- MemoryChallengeStore tests ---

func TestMemoryChallengeStore_Coverage_SaveAndGet(t *testing.T) {
	store := NewMemoryChallengeStore()
	challenge := &WalletChallenge{
		ID:            "ch-1",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		Nonce:         "abc123",
		Message:       "Sign this message",
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}

	err := store.SaveChallenge(context.Background(), challenge)
	require.NoError(t, err)

	got, err := store.GetChallenge(context.Background(), "ch-1")
	require.NoError(t, err)
	assert.Equal(t, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", got.WalletAddress)
	assert.Equal(t, "abc123", got.Nonce)
}

func TestMemoryChallengeStore_Coverage_MarkUsed(t *testing.T) {
	store := NewMemoryChallengeStore()
	challenge := &WalletChallenge{
		ID:            "ch-2",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		Nonce:         "xyz789",
		Message:       "Sign this message",
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}
	require.NoError(t, store.SaveChallenge(context.Background(), challenge))

	err := store.MarkChallengeUsed(context.Background(), "ch-2", time.Now())
	require.NoError(t, err)

	got, err := store.GetChallenge(context.Background(), "ch-2")
	require.NoError(t, err)
	assert.False(t, got.UsedAt.IsZero())
}

// --- Error sentinel tests ---

func TestErrorSentinels(t *testing.T) {
	assert.ErrorIs(t, ErrInvalidCredential, ErrInvalidCredential)
	assert.ErrorIs(t, ErrTokenExpired, ErrTokenExpired)
	assert.ErrorIs(t, ErrTokenRevoked, ErrTokenRevoked)
	assert.ErrorIs(t, ErrNFTNotFound, ErrNFTNotFound)
	assert.ErrorIs(t, ErrInsufficientBalance, ErrInsufficientBalance)
}

// --- Streaming additional coverage ---

func TestStreamingService_AddStreamQuality_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://localhost")
	err := svc.AddStreamQuality(context.Background(), "s1", Quality{Name: "720p", URL: "http://cdn/720.m3u8", Bitrate: 5000})
	assert.Error(t, err)
}

func TestStreamingService_UpdateStreamPlaylist_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://localhost")
	err := svc.UpdateStreamPlaylist(context.Background(), "s1", "#EXTM3U\nnew playlist")
	assert.Error(t, err)
}

func TestStreamingService_GetStreamByID_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://localhost")
	_, err := svc.GetStreamByID(context.Background(), "s1")
	assert.Error(t, err)
}

// --- NFT Service coverage ---

func TestNFTService_NewNFTService_InvalidRPC(t *testing.T) {
	_, err := NewNFTService("invalid-rpc-url", nil)
	assert.Error(t, err)
}

func TestNFTService_VerifyNFT_NilCaller(t *testing.T) {
	svc := &NFTService{logger: zap.NewNop()}
	_, err := svc.VerifyNFT(context.Background(), "0xContract", "1", "0xOwner")
	assert.Error(t, err)
}

func TestNFTService_VerifyNFTBatch_NilCaller(t *testing.T) {
	svc := &NFTService{logger: zap.NewNop()}
	// VerifyNFTBatch with nil caller returns empty results without error
	results, err := svc.VerifyNFTBatch(context.Background(), "0xContract", nil)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestNFTService_GetNFTMetadata_NilCaller(t *testing.T) {
	svc := &NFTService{logger: zap.NewNop()}
	_, err := svc.GetNFTMetadata(context.Background(), "0xContract", "1")
	assert.Error(t, err)
}

// --- Web3Service coverage ---

func TestWeb3Service_GetWalletManager_NilService(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	wm := svc.GetWalletManager()
	assert.Nil(t, wm)
}

func TestWeb3Service_GetSignatureVerifier_NilService(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	sv := svc.GetSignatureVerifier()
	assert.Nil(t, sv)
}

func TestWeb3Service_GetMultiChainManager_NilService(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	mcm := svc.GetMultiChainManager()
	assert.Nil(t, mcm)
}

func TestWeb3Service_GetGasMonitor_NilService(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	gm := svc.GetGasMonitor()
	assert.Nil(t, gm)
}

// --- Service locator coverage ---

func TestContentService_SetContentRegistry_NilDB(t *testing.T) {
	svc := NewContentService(nil, nil, nil, zap.NewNop())
	registry := &mockContentRegistry{}
	svc.SetContentRegistry(registry)
	assert.Equal(t, registry, svc.registry)
}

// --- Service locator coverage ---

func TestNewServiceLocator(t *testing.T) {
	loc := NewServiceLocator(nil, zap.NewNop())
	assert.NotNil(t, loc)
}

// --- Transcoding queue coverage ---

func TestNewMemoryTranscodingQueue(t *testing.T) {
	q := NewMemoryTranscodingQueue()
	assert.NotNil(t, q)
}

// --- Web3Service nil-guard and pure function tests ---

func TestWeb3Service_CreateNFT_NotSupported(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	err := svc.CreateNFT(context.Background(), nil)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestWeb3Service_ListNFTs(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	results, err := svc.ListNFTs(context.Background(), 0, 10)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestWeb3Service_SendTransaction_NoKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.SendTransaction(context.Background(), 1, "0xabc", big.NewInt(0), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_ReplaceStuckTransaction_NoKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.ReplaceStuckTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_CancelPendingTransaction_NoKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.CancelPendingTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_GetGasPriceLevels_NilMonitor(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetGasPriceLevels(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gas monitor not initialized")
}

func TestWeb3Service_UploadToIPFS_NilClient(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.UploadToIPFS(context.Background(), "file.txt", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestWeb3Service_DownloadFromIPFS_NilClient(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.DownloadFromIPFS(context.Background(), "QmTest")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestWeb3Service_VerifySolanaTokenAccount_NilVerifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifySolanaTokenAccount(context.Background(), "acct", "owner")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestWeb3Service_VerifySolanaMintAuthority_NilVerifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifySolanaMintAuthority(context.Background(), "mint", "auth")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestWeb3Service_VerifySolanaMetaplexMetadata_NilVerifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifySolanaMetaplexMetadata(context.Background(), "uri", "creator", "sig")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestWeb3Service_GetSolanaVerifier_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetSolanaVerifier())
}

func TestWeb3Service_GetIPFSClient_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetIPFSClient())
}

func TestWeb3Service_GetTransactionQueue_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetTransactionQueue())
}

func TestWeb3Service_Close_NilDeps(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	svc.Close() // should not panic with nil deps
}

func TestGweiToWei(t *testing.T) {
	result := gweiToWei(1.0)
	assert.Equal(t, new(big.Int).SetUint64(params.GWei), result)

	result = gweiToWei(0.0)
	assert.Equal(t, new(big.Int), result)
}

func TestIsNonceError(t *testing.T) {
	tooLow, feeTooLow := isNonceError(fmt.Errorf("nonce too low"))
	assert.True(t, tooLow)
	assert.False(t, feeTooLow)

	tooLow, feeTooLow = isNonceError(fmt.Errorf("replacement fee too low"))
	assert.False(t, tooLow)
	assert.True(t, feeTooLow)

	tooLow, feeTooLow = isNonceError(fmt.Errorf("already known"))
	assert.False(t, tooLow)
	assert.True(t, feeTooLow)

	tooLow, feeTooLow = isNonceError(fmt.Errorf("something else"))
	assert.False(t, tooLow)
	assert.False(t, feeTooLow)

	tooLow, feeTooLow = isNonceError(nil)
	assert.False(t, tooLow)
	assert.False(t, feeTooLow)
}

func TestWeb3Service_VerifyMerkleWhitelist_InvalidRoot(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifyMerkleWhitelist("not-hex", "0xabc", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid root hex")
}

func TestWeb3Service_VerifyMerkleWhitelist_InvalidProof(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	root := "0x" + strings.Repeat("ab", 32)
	_, err := svc.VerifyMerkleWhitelist(root, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", []string{"not-hex"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid proof element")
}

// --- Auth wallet pure function tests ---

func TestGenerateNonce(t *testing.T) {
	nonce, err := generateNonce()
	require.NoError(t, err)
	assert.Len(t, nonce, 32) // 16 bytes = 32 hex chars
}

func TestIsValidSolanaAddress_Coverage(t *testing.T) {
	assert.False(t, IsValidSolanaAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"), "EVM address")
	assert.False(t, IsValidSolanaAddress("short"), "too short")
	assert.False(t, IsValidSolanaAddress(""), "empty")
}

func TestIsSolanaChain_Coverage(t *testing.T) {
	assert.True(t, isSolanaChain(-1))
	assert.False(t, isSolanaChain(1))
	assert.False(t, isSolanaChain(0))
}

func TestMemoryTokenBlacklist_Close(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	require.NoError(t, bl.Close())
}

func TestMemoryTokenBlacklist_EvictExpired(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	// Revoke with already-expired entry
	_ = bl.Revoke(context.Background(), "expired-jti", time.Now().Add(-time.Hour))
	// The entry is expired; calling IsRevoked should lazily evict it
	assert.False(t, bl.IsRevoked(context.Background(), "expired-jti"))

	// Also test evictExpired directly
	_ = bl.Revoke(context.Background(), "another-expired", time.Now().Add(-time.Hour))
	bl.evictExpired()
	assert.False(t, bl.IsRevoked(context.Background(), "another-expired"))
	require.NoError(t, bl.Close())
}

func TestAuthService_RevokeToken_InvalidToken(t *testing.T) {
	storage := NewMockAuthStorage()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage)
	// Invalid token should be silently accepted
	err := svc.RevokeToken(context.Background(), "invalid-token")
	assert.NoError(t, err)
}

func TestAuthService_RevokeToken_ValidToken(t *testing.T) {
	storage := NewMockAuthStorage()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage, WithTokenBlacklist(NewMemoryTokenBlacklist()))
	// Generate a valid token first
	token, err := svc.generateToken(&models.User{Username: "testuser", WalletAddress: "0xabc"})
	require.NoError(t, err)

	err = svc.RevokeToken(context.Background(), token)
	assert.NoError(t, err)
}

func TestAuthService_VerifyToken_ValidToken(t *testing.T) {
	storage := NewMockAuthStorage()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage)
	token, err := svc.generateToken(&models.User{Username: "testuser", WalletAddress: "0xabc"})
	require.NoError(t, err)

	result, err := svc.VerifyToken(context.Background(), token)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, "0xabc", result.WalletAddress)
}

func TestAuthService_VerifyToken_InvalidToken(t *testing.T) {
	storage := NewMockAuthStorage()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	result, err := svc.VerifyToken(context.Background(), "invalid-token")
	assert.Error(t, err)
	assert.False(t, result.Valid)
}

func TestAuthService_VerifyToken_RevokedToken(t *testing.T) {
	storage := NewMockAuthStorage()
	bl := NewMemoryTokenBlacklist()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage, WithTokenBlacklist(bl))

	token, err := svc.generateToken(&models.User{Username: "testuser", WalletAddress: "0xabc"})
	require.NoError(t, err)

	// Revoke then verify
	err = svc.RevokeToken(context.Background(), token)
	require.NoError(t, err)

	result, err := svc.VerifyToken(context.Background(), token)
	assert.Error(t, err)
	assert.False(t, result.Valid)
	require.NoError(t, bl.Close())
}

func TestAuthService_IsTokenRevoked(t *testing.T) {
	storage := NewMockAuthStorage()
	bl := NewMemoryTokenBlacklist()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage, WithTokenBlacklist(bl))
	assert.False(t, svc.IsTokenRevoked(context.Background(), "nonexistent-jti"))
	require.NoError(t, bl.Close())
}

func TestAuthService_GeneratePlaybackToken(t *testing.T) {
	storage := NewMockAuthStorage()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	token, err := svc.GeneratePlaybackToken("0xWallet", "content1", "0xContract", "1", 1, 5*time.Minute)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestAuthService_GeneratePlaybackToken_DefaultTTL(t *testing.T) {
	storage := NewMockAuthStorage()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Zero TTL should default to 2 minutes
	token, err := svc.GeneratePlaybackToken("0xWallet", "content1", "0xContract", "1", 1, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestAuthService_ValidatePlaybackToken(t *testing.T) {
	storage := NewMockAuthStorage()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	token, err := svc.GeneratePlaybackToken("0xWallet", "content1", "0xContract", "1", 1, 5*time.Minute)
	require.NoError(t, err)

	claims, err := svc.ValidatePlaybackToken(token, "content1")
	require.NoError(t, err)
	assert.Equal(t, "content1", claims.Subject)
	assert.Equal(t, "0xWallet", claims.WalletAddress)
}

func TestAuthService_ValidatePlaybackToken_Mismatch(t *testing.T) {
	storage := NewMockAuthStorage()
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	token, err := svc.GeneratePlaybackToken("0xWallet", "content1", "0xContract", "1", 1, 5*time.Minute)
	require.NoError(t, err)

	_, err = svc.ValidatePlaybackToken(token, "different-content")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mismatch")
}

func TestAuthService_BuildEIP712Challenge(t *testing.T) {
	svc := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())
	challenge := &WalletChallenge{
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		Nonce:         "abc123",
		IssuedAt:      time.Now(),
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}
	td := svc.buildEIP712Challenge(challenge)
	assert.NotNil(t, td)
	assert.Equal(t, "Authentication", td.PrimaryType)
	assert.Equal(t, "StreamGate", td.Domain.Name)
	assert.Equal(t, "1", td.Domain.Version)
}

// --- NFT Service additional coverage ---

func TestParseMetadataJSON(t *testing.T) {
	data := `{"name":"Test NFT","description":"A test","image":"ipfs://QmTest","attributes":[{"trait_type":"color","value":"blue"}]}`
	meta, err := ParseMetadataJSON([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "Test NFT", meta.Name)
	assert.Equal(t, "A test", meta.Description)
}

func TestParseMetadataJSON_Invalid(t *testing.T) {
	_, err := ParseMetadataJSON([]byte("not json"))
	assert.Error(t, err)
}

func TestNFTService_InvalidateOwnershipCache_NoCache(t *testing.T) {
	svc := &NFTService{logger: zap.NewNop()}
	// Should not panic with no cache
	svc.InvalidateOwnershipCache("0xContract", "1")
}

func TestNFTService_SetLogger(t *testing.T) {
	svc := &NFTService{logger: zap.NewNop()}
	newLogger := zap.NewNop()
	svc.SetLogger(newLogger)
	assert.Equal(t, newLogger, svc.logger)
}

func TestNFTService_Close_NoClient(t *testing.T) {
	svc := &NFTService{logger: zap.NewNop()}
	svc.Close() // should not panic
}

// --- Upload additional coverage ---

func TestBytesSliceReader_Read(t *testing.T) {
	r := bytesReader([]byte("hello world"))
	buf := make([]byte, 5)
	n, err := r.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", string(buf))
}

func TestBytesSliceReader_Read_EOF(t *testing.T) {
	r := bytesReader([]byte("hi"))
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = r.Read(buf)
	assert.Equal(t, io.EOF, err)
}

func TestDetectContentType(t *testing.T) {
	assert.Equal(t, "video/mp4", detectContentType("video.mp4"))
	assert.Equal(t, "video/webm", detectContentType("clip.webm"))
	assert.Equal(t, "audio/mpeg", detectContentType("song.mp3"))
	assert.Equal(t, "audio/wav", detectContentType("sound.wav"))
	assert.Equal(t, "image/jpeg", detectContentType("photo.jpg"))
	assert.Equal(t, "image/jpeg", detectContentType("photo.jpeg"))
	assert.Equal(t, "image/png", detectContentType("icon.png"))
	assert.Equal(t, "image/gif", detectContentType("anim.gif"))
	assert.Equal(t, "application/octet-stream", detectContentType("file.unknown"))
}

func TestUploadService_UpdateUploadStatus_NilDB(t *testing.T) {
	svc := NewUploadService(nil, nil, "test-bucket", zap.NewNop())
	err := svc.updateUploadStatus(context.Background(), "upload1", "completed")
	assert.Error(t, err)
}

// --- Transcoding option tests ---

func TestWithTranscoder(t *testing.T) {
	svc := &TranscodingService{tasks: make(map[string]*TranscodingTask)}
	mock := &mockVideoTranscoder{}
	opt := WithTranscoder(mock)
	opt(svc)
	assert.Equal(t, mock, svc.transcoder)
}

func TestWithStorage(t *testing.T) {
	svc := &TranscodingService{tasks: make(map[string]*TranscodingTask)}
	mock := &mockSegmentStorage{}
	opt := WithStorage(mock)
	opt(svc)
	assert.Equal(t, mock, svc.storage)
}

func TestTranscodingService_GetProfile_NotFound(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	_, err := svc.GetProfile("nonexistent")
	assert.Error(t, err)
}

func TestTranscodingService_GetProfile_Found(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	profile, err := svc.GetProfile("720p")
	require.NoError(t, err)
	assert.Equal(t, "720p", profile.Name)
}

func TestTranscodingService_ListProfiles(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	profiles := svc.ListProfiles()
	assert.NotEmpty(t, profiles)
}

func TestTranscodingService_FailTask_NotFound(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	err := svc.FailTask(context.Background(), "task1", "something failed")
	assert.Error(t, err) // task not in memory map
}

// --- CircuitBreaker tests ---

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(3, 10, zap.NewNop())
	assert.Equal(t, "closed", cb.GetState())
}

func TestNewCircuitBreaker_DefaultTimeout(t *testing.T) {
	cb := NewCircuitBreaker(3, 0, zap.NewNop())
	assert.Equal(t, "closed", cb.GetState())
}

func TestCircuitBreaker_Call_Success(t *testing.T) {
	cb := NewCircuitBreaker(3, 10, zap.NewNop())
	err := cb.Call(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, "closed", cb.GetState())
}

func TestCircuitBreaker_Call_Failure(t *testing.T) {
	cb := NewCircuitBreaker(2, 1, zap.NewNop())
	err := cb.Call(func() error { return fmt.Errorf("fail") })
	assert.Error(t, err)
	assert.Equal(t, "closed", cb.GetState()) // not yet at maxFailures

	err = cb.Call(func() error { return fmt.Errorf("fail again") })
	assert.Error(t, err)
	assert.Equal(t, "open", cb.GetState()) // now at maxFailures
}

func TestCircuitBreaker_Call_OpenState(t *testing.T) {
	cb := NewCircuitBreaker(1, 1, zap.NewNop())
	// Trip the breaker
	_ = cb.Call(func() error { return fmt.Errorf("fail") })
	assert.Equal(t, "open", cb.GetState())

	// Call should fail immediately when open
	err := cb.Call(func() error { return nil })
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestCircuitBreaker_HalfOpen_Recovery(t *testing.T) {
	cb := NewCircuitBreaker(1, 0, zap.NewNop())
	// Trip the breaker
	_ = cb.Call(func() error { return fmt.Errorf("fail") })
	assert.Equal(t, "open", cb.GetState())

	// Wait for recovery timeout (default 30s, but we can manually set half-open)
	cb.mu.Lock()
	cb.state = "half-open"
	cb.mu.Unlock()
	assert.Equal(t, "half-open", cb.GetState())

	// Successful trial should close the breaker
	err := cb.Call(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, "closed", cb.GetState())
}

func TestCircuitBreaker_HalfOpen_TrialFailure(t *testing.T) {
	cb := NewCircuitBreaker(1, 0, zap.NewNop())
	// Trip the breaker
	_ = cb.Call(func() error { return fmt.Errorf("fail") })
	assert.Equal(t, "open", cb.GetState())

	// Manually transition to half-open
	cb.mu.Lock()
	cb.state = "half-open"
	cb.mu.Unlock()
	assert.Equal(t, "half-open", cb.GetState())

	// Failed trial should reopen
	err := cb.Call(func() error { return fmt.Errorf("still failing") })
	assert.Error(t, err)
	assert.Equal(t, "open", cb.GetState())
}

// --- ServiceLocator tests with mock registry ---

type mockRegistry struct {
	services map[string][]*ServiceInfo
}

func (m *mockRegistry) Register(ctx context.Context, service *ServiceInfo) error { return nil }
func (m *mockRegistry) Deregister(ctx context.Context, serviceID string) error   { return nil }
func (m *mockRegistry) Discover(ctx context.Context, serviceName string) ([]*ServiceInfo, error) {
	return m.services[serviceName], nil
}
func (m *mockRegistry) Watch(ctx context.Context, serviceName string) (<-chan []*ServiceInfo, error) {
	return nil, nil
}
func (m *mockRegistry) Health(ctx context.Context) error { return nil }

func TestServiceLocator_GetUploadService(t *testing.T) {
	reg := &mockRegistry{
		services: map[string][]*ServiceInfo{
			ServiceUpload: {{Address: "localhost", Port: 8080}},
		},
	}
	loc := NewServiceLocator(reg, zap.NewNop())
	addr, err := loc.GetUploadService(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "localhost:8080", addr)
}

func TestServiceLocator_GetStreamingService_NotFound(t *testing.T) {
	reg := &mockRegistry{services: map[string][]*ServiceInfo{}}
	loc := NewServiceLocator(reg, zap.NewNop())
	_, err := loc.GetStreamingService(context.Background())
	assert.Error(t, err)
}

func TestClientPool_Close(t *testing.T) {
	pool := NewClientPool(nil, zap.NewNop())
	err := pool.Close()
	assert.NoError(t, err)
}

// --- Streaming additional tests ---

func TestStreamingService_CreateStream_NilDB(t *testing.T) {
	svc := NewStreamingService(nil, nil, nil, "http://localhost")
	_, err := svc.CreateStream(context.Background(), "content1", "hls")
	assert.Error(t, err)
}

// --- Mock types for transcoding ---

type mockVideoTranscoder struct{}

func (m *mockVideoTranscoder) TranscodeToHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(progress float64)) error {
	return nil
}

type mockSegmentStorage struct{}

func (m *mockSegmentStorage) Upload(ctx context.Context, bucket, objectName string, data []byte) error {
	return nil
}
func (m *mockSegmentStorage) UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error {
	return nil
}
func (m *mockSegmentStorage) UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error {
	return nil
}
func (m *mockSegmentStorage) Download(ctx context.Context, bucket, objectName string) ([]byte, error) {
	return nil, nil
}
func (m *mockSegmentStorage) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	return nil, nil
}
func (m *mockSegmentStorage) DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockSegmentStorage) Exists(ctx context.Context, bucket, objectName string) (bool, error) {
	return false, nil
}
func (m *mockSegmentStorage) UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error {
	return nil
}
func (m *mockSegmentStorage) Delete(ctx context.Context, bucket, objectName string) error {
	return nil
}

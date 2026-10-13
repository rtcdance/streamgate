package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/rtcdance/streamgate/pkg/service"
)

// fakeSegmentStore serves a fixed set of objects so the handler's stored-playlist
// path can be exercised without MinIO.
type fakeSegmentStore struct {
	objects map[string][]byte
}

func (f *fakeSegmentStore) Upload(ctx context.Context, bucket, objectName string, data []byte) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeSegmentStore) UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeSegmentStore) UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeSegmentStore) UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeSegmentStore) Download(ctx context.Context, bucket, objectName string) ([]byte, error) {
	if data, ok := f.objects[objectName]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("object %s not found", objectName)
}
func (f *fakeSegmentStore) DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error) {
	data, err := f.Download(ctx, bucket, objectName)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}
func (f *fakeSegmentStore) Delete(ctx context.Context, bucket, objectName string) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeSegmentStore) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}
func (f *fakeSegmentStore) Exists(ctx context.Context, bucket, objectName string) (bool, error) {
	_, ok := f.objects[objectName]
	return ok, nil
}

const storedMediaPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:8
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:8.333333,
640x360_000.ts
#EXTINF:1.666667,
640x360_001.ts
#EXT-X-ENDLIST
`

const storedMasterPlaylist = `#EXTM3U
#EXT-X-VERSION:3

#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=640x360
640x360.m3u8
`

func newStoredPlaylistRouter(t *testing.T, store *fakeSegmentStore) (*gin.Engine, *service.AuthService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})
	authSvc := newGatewayTestAuthService()
	streamingSvc := service.NewStreamingService(nil, nil, nil, "http://localhost")
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)
	RegisterStreamingRoutes(r, zap.NewNop(), authSvc, streamingSvc, store, limiter, cache, nil)
	return r, authSvc
}

// The variant path is where the master rewrite sends the player. It must serve
// the playlist FFmpeg wrote, not a synthesised one claiming fixed 6.0s segments.
func TestStreamingExt_ManifestQuality_ServesTheStoredPlaylist(t *testing.T) {
	store := &fakeSegmentStore{objects: map[string][]byte{
		"streams/c1/640x360/640x360.m3u8":    []byte(storedMediaPlaylist),
		"streams/c1/640x360/640x360_000.ts":  []byte("seg0"),
		"streams/c1/640x360/640x360_001.ts":  []byte("seg1"),
		"streams/c1/master.m3u8/master.m3u8": []byte(storedMasterPlaylist),
	}}
	r, authSvc := newStoredPlaylistRouter(t, store)

	token, err := authSvc.GeneratePlaybackToken(context.Background(),
		"0x1234567890abcdef1234567890abcdef12345678", "c1", "", "", 1, 5*time.Minute, "")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		APIPrefix+"/streaming/c1/manifest.m3u8?quality=640x360&playback_token="+token, http.NoBody)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "#EXTINF:8.333333,", "the variant path must carry FFmpeg's real durations")
	assert.Contains(t, body, "#EXTINF:1.666667,", "the short final segment must survive")
	assert.Contains(t, body, "#EXT-X-TARGETDURATION:8")
	assert.NotContains(t, body, "#EXTINF:6.0,", "a synthesised playlist leaked onto the variant path")
	assert.Contains(t, body, "/api/v1/streaming/c1/segment/640x360_000.ts?quality=640x360&playback_token="+token)
}

// The master path serves FFmpeg's multivariant playlist with its BANDWIDTH and
// RESOLUTION intact, pointing variants at the protected manifest route.
func TestStreamingExt_ManifestMaster_ServesTheStoredMaster(t *testing.T) {
	store := &fakeSegmentStore{objects: map[string][]byte{
		"streams/c1/640x360/640x360.m3u8":    []byte(storedMediaPlaylist),
		"streams/c1/640x360/640x360_000.ts":  []byte("seg0"),
		"streams/c1/640x360/640x360_001.ts":  []byte("seg1"),
		"streams/c1/master.m3u8/master.m3u8": []byte(storedMasterPlaylist),
	}}
	r, _ := newStoredPlaylistRouter(t, store)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/c1/manifest.m3u8", http.NoBody)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=640x360")
	assert.Contains(t, body, "/api/v1/streaming/c1/manifest.m3u8?quality=640x360&playback_token=")
	assert.NotContains(t, body, "\n640x360.m3u8", "the raw object name must not survive as a URI")
}

// Without a stored playlist (legacy content transcoded before playlists were
// kept) the synthesised fallback still serves, so playback is not broken.
func TestStreamingExt_ManifestQuality_FallsBackWithoutStoredPlaylist(t *testing.T) {
	store := &fakeSegmentStore{objects: map[string][]byte{
		"streams/c1/720p/seg0.ts": []byte("seg0"),
		"streams/c1/720p/seg1.ts": []byte("seg1"),
	}}
	r, authSvc := newStoredPlaylistRouter(t, store)

	token, err := authSvc.GeneratePlaybackToken(context.Background(),
		"0x1234567890abcdef1234567890abcdef12345678", "c1", "", "", 1, 5*time.Minute, "")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		APIPrefix+"/streaming/c1/manifest.m3u8?quality=720p&playback_token="+token, http.NoBody)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "#EXTM3U", "legacy content without stored playlists must still play")
}

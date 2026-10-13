package streamingsvc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are the exact bytes FFmpeg produced for the acceptance fixture, read out
// of the object store. The 10s fixture is split 8.333333 + 1.666667, and
// TARGETDURATION is 8 — none of which the synthesised playlist could know.
const realMediaPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:8
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:8.333333,
640x360_000.ts
#EXTINF:1.666667,
640x360_001.ts
#EXT-X-ENDLIST
`

const realMasterPlaylist = `#EXTM3U
#EXT-X-VERSION:3

#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=640x360
640x360.m3u8
`

// Every tag is preserved exactly. The durations are FFmpeg's, including the
// short final segment that a fixed 6.0s rewrite erased.
func TestRewriteMediaPlaylistURIs_PreservesEveryTag(t *testing.T) {
	out, err := RewriteMediaPlaylistURIs(realMediaPlaylist, "c1", "640x360", "TOK")
	require.NoError(t, err)

	assert.Contains(t, out, "#EXT-X-TARGETDURATION:8", "TARGETDURATION must be FFmpeg's, not invented")
	assert.Contains(t, out, "#EXTINF:8.333333,")
	assert.Contains(t, out, "#EXTINF:1.666667,", "the short final segment must survive")
	assert.Contains(t, out, "#EXT-X-MEDIA-SEQUENCE:0")
	assert.Contains(t, out, "#EXT-X-ENDLIST")

	// The total the playlist describes is the real duration, not a fixed 6s each.
	assert.NotContains(t, out, "#EXTINF:6.0,")
}

func TestRewriteMediaPlaylistURIs_RewritesOnlyTheURIs(t *testing.T) {
	out, err := RewriteMediaPlaylistURIs(realMediaPlaylist, "c1", "640x360", "TOK")
	require.NoError(t, err)

	assert.Contains(t, out, "/api/v1/streaming/c1/segment/640x360_000.ts?quality=640x360&playback_token=TOK")
	assert.Contains(t, out, "/api/v1/streaming/c1/segment/640x360_001.ts?quality=640x360&playback_token=TOK")
	assert.NotContains(t, out, "\n640x360_000.ts", "the raw object name must not survive as a URI")
}

// A relative URI with a subdirectory still resolves to the same protected route,
// and any query/fragment on the original is dropped rather than forwarded.
func TestRewriteMediaPlaylistURIs_NormalisesTheURIPath(t *testing.T) {
	raw := "#EXTM3U\n#EXTINF:4.0,\n../v/seg.ts?x=1#frag\n#EXT-X-ENDLIST\n"
	out, err := RewriteMediaPlaylistURIs(raw, "c1", "q", "TOK")
	require.NoError(t, err)
	assert.Contains(t, out, "/api/v1/streaming/c1/segment/seg.ts?quality=q&playback_token=TOK")
}

// The initialisation segment is delivered through the same protected route, so
// EXT-X-MAP is rewritten rather than dropped or left pointing at an object path.
func TestRewriteMediaPlaylistURIs_RewritesTheInitSegment(t *testing.T) {
	raw := "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4.0,\nseg0.m4s\n#EXT-X-ENDLIST\n"
	out, err := RewriteMediaPlaylistURIs(raw, "c1", "q", "TOK")
	require.NoError(t, err)
	assert.Contains(t, out, `#EXT-X-MAP:URI="/api/v1/streaming/c1/segment/init.mp4?quality=q&playback_token=TOK"`)
	assert.Contains(t, out, "/api/v1/streaming/c1/segment/seg0.m4s?quality=q&playback_token=TOK")
}

// An encrypted playlist is refused rather than rewritten: a key is not a media
// segment, there is no route that serves one, and leaving the tag alone would
// hand the player a raw object path.
func TestRewriteMediaPlaylistURIs_RefusesEncryptedPlaylists(t *testing.T) {
	raw := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:4.0,\nseg0.ts\n#EXT-X-ENDLIST\n"
	_, err := RewriteMediaPlaylistURIs(raw, "c1", "q", "TOK")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypted")
}

// An unknown tag carrying a URI is refused rather than silently passed through,
// because passing it through would point the player at an unprotected object.
func TestRewriteMediaPlaylistURIs_RefusesAnUnknownTagURI(t *testing.T) {
	raw := "#EXTM3U\n#EXT-X-SOMETHING:URI=\"x.bin\"\n#EXTINF:4.0,\nseg0.ts\n#EXT-X-ENDLIST\n"
	_, err := RewriteMediaPlaylistURIs(raw, "c1", "q", "TOK")
	assert.Error(t, err)
}

func TestRewriteMediaPlaylistURIs_RefusesAnEmptyPlaylist(t *testing.T) {
	_, err := RewriteMediaPlaylistURIs("\n\n", "c1", "q", "TOK")
	assert.Error(t, err)
}

// The master keeps FFmpeg's BANDWIDTH and RESOLUTION, and each variant is
// addressed by the name that was actually produced.
func TestRewriteMasterPlaylistURIs_PreservesStreamInf(t *testing.T) {
	out, err := RewriteMasterPlaylistURIs(realMasterPlaylist, "c1", "TOK")
	require.NoError(t, err)

	assert.Contains(t, out, "#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=640x360")
	assert.Contains(t, out, "/api/v1/streaming/c1/manifest.m3u8?quality=640x360&playback_token=TOK")
	assert.NotContains(t, out, "\n640x360.m3u8")
}

// Alternate renditions point at their own playlist, so their URI is rewritten
// like a variant rather than dropped.
func TestRewriteMasterPlaylistURIs_RewritesAlternateRenditions(t *testing.T) {
	raw := "#EXTM3U\n" +
		`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="a",NAME="English",URI="audio/en.m3u8"` + "\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=1,AUDIO=\"a\"\n" +
		"640x360.m3u8\n"
	out, err := RewriteMasterPlaylistURIs(raw, "c1", "TOK")
	require.NoError(t, err)
	assert.Contains(t, out, "/api/v1/streaming/c1/manifest.m3u8?quality=en&playback_token=TOK")
	assert.Contains(t, out, "/api/v1/streaming/c1/manifest.m3u8?quality=640x360&playback_token=TOK")
}

// The kind is decided by the tags HLS defines for it, not by the object name.
func TestClassifyPlaylist(t *testing.T) {
	assert.Equal(t, PlaylistMaster, ClassifyPlaylist(realMasterPlaylist))
	assert.Equal(t, PlaylistMedia, ClassifyPlaylist(realMediaPlaylist))
	// A media playlist carries MEDIA-SEQUENCE, which contains "EXT-X-MEDIA" as a
	// substring but is not the alternate-rendition tag.
	assert.Equal(t, PlaylistMedia, ClassifyPlaylist("#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1,\na.ts\n"))
}

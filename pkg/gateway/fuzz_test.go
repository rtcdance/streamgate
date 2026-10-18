package gateway

import (
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzProtectedMediaPath pins the path-isolation property of the segment
// candidate builders: whatever a request supplies, every object key they build
// stays INSIDE the requested content's own namespace. The content ID and the
// segment name are validated by the route before a builder runs, so only those
// domains are asserted; `quality` is deliberately left unfiltered because the
// builder must be safe for any value it is handed — a client-controlled quality
// is exactly what could otherwise walk out of the prefix.
func FuzzProtectedMediaPath(f *testing.F) {
	f.Add("c1", "segment0.ts", "720p", int64(0))
	f.Add("content-1", "hls/720p/seg3.ts", "640x360", int64(0))
	f.Add("c1", "seg0.ts", "", int64(0))
	f.Add("c1", "seg0.ts", "720p", int64(7))
	f.Add("c1", "seg0.ts", "..", int64(0))
	f.Add("c1", "seg0.ts", "../../etc", int64(0))
	f.Add("c1", "seg0.ts", "g2", int64(0))
	f.Add("c1", "seg0.ts", "720p/../../x", int64(3))

	f.Fuzz(func(t *testing.T, contentID, segName, quality string, generation int64) {
		if !isValidContentID(contentID) || !validateSegmentName(segName) {
			return
		}

		// Two surfaces: no index at all, and an index whose keys a bug could
		// still have carried in from a listing.
		for _, withIndex := range []bool{false, true} {
			cache := NewStreamingCache()
			if withIndex {
				cache.SetSegmentIndex(contentID, generation, map[string][]string{
					quality: {segName},
					"720p":  {segName},
				})
			}
			candidates := buildSegmentCandidates(contentID, segName, quality, cache, generation)
			require.NotEmpty(t, candidates, "a segment request must always try at least one key")
			for _, cand := range candidates {
				assertKeyIsolated(t, cand.key, contentID, segName, generation)
			}
		}
	})
}

// assertKeyIsolated checks one candidate key: it must be a clean relative path
// with no traversal, no backslash and no empty segment.
func assertKeyIsolated(t *testing.T, key, contentID, segName string, generation int64) {
	t.Helper()

	require.NotEmpty(t, key, "candidate key must not be empty")
	require.False(t, strings.Contains(key, "\\"), "candidate key %q must not contain a backslash", key)
	require.False(t, path.IsAbs(key), "candidate key %q must be relative", key)
	require.Equal(t, path.Clean(key), key,
		"candidate key %q must already be clean: a %q / %q / empty segment escapes the namespace", key, "..", ".")
	for _, seg := range strings.Split(key, "/") {
		require.NotEqual(t, "", seg, "candidate key %q must not contain an empty segment", key)
	}

	if generation >= 1 {
		prefix := "streams/" + contentID + "/g" + strconv.FormatInt(generation, 10) + "/"
		require.True(t, strings.HasPrefix(key, prefix),
			"generation %d key %q must live under %q", generation, key, prefix)
		assertRenditionSegment(t, key, strings.TrimPrefix(key, prefix), segName)
		return
	}

	flat := contentID + "/" + segName
	streamPrefix := "streams/" + contentID + "/"
	switch {
	case key == flat:
		return
	case strings.HasPrefix(key, streamPrefix):
		assertRenditionSegment(t, key, strings.TrimPrefix(key, streamPrefix), segName)
	default:
		t.Fatalf("generation 0 key %q is outside content %q (expected %q or prefix %q)",
			key, contentID, flat, streamPrefix)
	}
}

// assertRenditionSegment checks that what follows the namespace prefix is
// exactly <rendition>/<segName>, with a rendition label that can never name a
// generation-private directory or rewrite the requested segment.
func assertRenditionSegment(t *testing.T, key, rest, segName string) {
	t.Helper()

	slash := strings.Index(rest, "/")
	require.Greater(t, slash, 0, "key %q must carry a rendition directory before the segment", key)
	rendition := rest[:slash]
	require.True(t, isValidRenditionName(rendition), "key %q has an unusable rendition label %q", key, rendition)
	require.False(t, isGenerationDirName(rendition), "key %q reaches into a generation-private directory", key)
	require.Equal(t, segName, rest[slash+1:], "key %q must not extend or rewrite the requested segment", key)
}

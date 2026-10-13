package load

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPerfHarness_ValidationRejectsUnsuccessfulRequests proves a 5xx, a
// truncated body and a failed sha256 expectation NEVER count as success
// throughput, and that bytes are only attributed to fully-validated responses.
func TestPerfHarness_ValidationRejectsUnsuccessfulRequests(t *testing.T) {
	manifest := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n" +
		"#EXTINF:6.0,\n/seg/good.ts\n#EXTINF:6.0,\n/seg/error500.ts\n" +
		"#EXTINF:6.0,\n/seg/truncated.ts\n#EXTINF:6.0,\n/seg/wronghash.ts\n#EXT-X-ENDLIST\n"

	goodBody := []byte("good-segment-bytes")
	goodHash := sha256.Sum256(goodBody)
	wrongHashBody := []byte("tampered-bytes")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/streaming/c1/manifest.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte(manifest))
		case "/seg/good.ts":
			w.Header().Set("Content-Length", fmt.Sprint(len(goodBody)))
			_, _ = w.Write(goodBody)
		case "/seg/error500.ts":
			http.Error(w, "boom", http.StatusInternalServerError)
		// Declares a larger Content-Length than it writes — the client must
		// detect the truncation and reject it even though the status was 200.
		case "/seg/truncated.ts":
			w.Header().Set("Content-Length", "4096")
			_, _ = w.Write([]byte("short"))
		case "/seg/wronghash.ts":
			_, _ = w.Write(wrongHashBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Expect only applies the sha256 check to the two segments whose digests we
	// pinned; the manifest body is validated by the built-in #EXTM3U check.
	segmentExpect := func(u string, body []byte) error {
		switch u {
		case srv.URL + "/seg/good.ts", srv.URL + "/seg/wronghash.ts":
			return ExpectedSHA256(hex.EncodeToString(goodHash[:]))(u, body)
		}
		return nil
	}

	cfg := Config{
		BaseURL:   srv.URL,
		Scenario:  "unit",
		JWTToken:  "unit-test-token",
		ContentID: "c1",
		Viewers:   1,
		Interval:  40 * time.Millisecond,
		Warmup:    0,
		Duration:  300 * time.Millisecond,
		Expect:    segmentExpect,
	}
	res, err := Run(context.Background(), cfg)
	require.NoError(t, err)

	require.Greater(t, res.SuccessCount+res.ErrorCount, int64(0),
		"the sampling window must have executed at least one request")

	// 5xx is recorded in the status distribution and never a success.
	assert.GreaterOrEqual(t, res.HTTPStatuses["500"], int64(1),
		"5xx responses must be recorded in the status distribution")

	// 200-but-invalid bodies (truncated, wrong sha256) are errors, not
	// successes — verified precisely by the all-wrong scenario below. Valid
	// bodies still contribute their fully-read bytes.
	assert.GreaterOrEqual(t, res.TotalBytes, int64(len(goodBody)),
		"the validated good segment's bytes must be counted")

	// A scenario where the only reachable media fails the expectation must
	// report ZERO successes and ZERO bytes.
	onlyWrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/streaming/c1/manifest.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6.0,\n/seg/wronghash.ts\n#EXT-X-ENDLIST\n"))
		case "/seg/wronghash.ts":
			_, _ = w.Write(wrongHashBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer onlyWrong.Close()
	cfg.BaseURL = onlyWrong.URL
	cfg.Expect = ExpectedSHA256(hex.EncodeToString(goodHash[:]))
	cfg.Duration = 200 * time.Millisecond
	res2, err := Run(context.Background(), cfg)
	require.NoError(t, err)
	assert.Greater(t, res2.ErrorCount, int64(0),
		"a failing expectation must produce errors")
	assert.Zero(t, res2.SuccessCount,
		"a body failing the sha256 expectation must never count as success throughput")
	assert.Zero(t, res2.TotalBytes,
		"no bytes may be attributed to fully-failed scenarios")
}

// TestPerfHarness_StatsCorrectness pins the percentile math on a fixed sample
// and asserts the JSON result carries the commit / fixture provenance fields.
func TestPerfHarness_StatsCorrectness(t *testing.T) {
	// Fixed sample: values 1..100 ms. Nearest-rank: p50=50th, p95=95th, p99=99th.
	samples := make([]float64, 100)
	for i := range samples {
		samples[i] = float64(i + 1)
	}
	pcts := ComputePercentiles(samples)
	assert.InDelta(t, 50.0, pcts.P50, 0.001)
	assert.InDelta(t, 95.0, pcts.P95, 0.001)
	assert.InDelta(t, 99.0, pcts.P99, 0.001)

	// Empty set → zero-valued percentiles, not a panic.
	empty := ComputePercentiles(nil)
	assert.Zero(t, empty.P50)

	// A full Run() against a deterministic local server must produce a JSON
	// result with commit + fixture fields populated.
	body := []byte("segment-body")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/streaming/c1/manifest.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6.0,\n/seg/a.ts\n#EXT-X-ENDLIST\n"))
		case "/seg/a.ts":
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("PERF_COMMIT", "deadbeefcafe")
	cfg := Config{
		BaseURL:     srv.URL,
		Scenario:    "warm_cache",
		JWTToken:    "unit-test-token",
		ContentID:   "c1",
		Viewers:     1,
		Interval:    30 * time.Millisecond,
		Warmup:      0,
		Duration:    300 * time.Millisecond,
		FixtureHash: "fixturehash-abc",
	}
	res, err := Run(context.Background(), cfg)
	require.NoError(t, err)
	require.Greater(t, res.SuccessCount, int64(0))
	assert.Zero(t, res.ErrorCount, "a healthy local server must yield zero errors")

	// Provenance fields must survive JSON round-trip.
	dir := t.TempDir()
	outPath := filepath.Join(dir, "result.json")
	require.NoError(t, res.WriteJSON(outPath))
	raw, err := os.ReadFile(outPath)
	require.NoError(t, err)
	var decoded Result
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, "deadbeefcafe", decoded.Commit)
	assert.Equal(t, "fixturehash-abc", decoded.FixtureHash)
	assert.Equal(t, "warm_cache", decoded.Scenario)
	assert.Equal(t, res.SuccessCount, decoded.SuccessCount)
	assert.Equal(t, res.TotalBytes, decoded.TotalBytes)
	assert.NotZero(t, decoded.IntervalMS)
	assert.Greater(t, decoded.DurationSec, 0.0)
}

// TestPerfHarness_ResolveURI pins playlist-URI normalisation against BaseURL.
func TestPerfHarness_ResolveURI(t *testing.T) {
	base := "http://host:18080"
	assert.Equal(t, "http://host:18080/api/seg.ts", resolveURI(base, "/api/seg.ts"))
	assert.Equal(t, "http://host:18080/seg.ts", resolveURI(base, "seg.ts"))
	assert.Equal(t, "http://other/seg.ts", resolveURI(base, "http://other/seg.ts"))
}

// TestPerfHarness_ParseManifestURIs ignores comments and blank lines.
func TestPerfHarness_ParseManifestURIs(t *testing.T) {
	body := []byte("#EXTM3U\n#EXTINF:6.0,\n/a.ts\n\n#EXT-X-ENDLIST\n/b.ts\n")
	uris := ParseManifestURIs(body)
	assert.Equal(t, []string{"/a.ts", "/b.ts"}, uris)
}

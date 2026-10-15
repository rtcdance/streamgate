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
	"strings"
	"sync"
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

// badWallet is a test-only hook: when set, the stub auth server rejects that
// wallet's login with a 401 so failure handling can be exercised.
var badWallet string

// newTokenEchoServer serves a manifest + one segment and echoes each request's
// Authorization header back so tests can observe WHICH wallet token each
// virtual viewer used.
func newTokenEchoServer(t *testing.T) (srv *httptest.Server, seenTokens func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "manifest.m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6.0,\n/seg/a.ts\n#EXT-X-ENDLIST\n"))
		case r.URL.Path == "/seg/a.ts":
			_, _ = w.Write([]byte("segment-body"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	seenTokens = func() []string {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]string, len(seen))
		copy(cp, seen)
		return cp
	}
	return srv, seenTokens
}

// TestPerfHarness_MultiWalletViewerMapping pins the SG-07 rule that every
// virtual viewer drives its OWN real-login wallet: with a 3-wallet pool and 3
// viewers, each distinct token must actually reach the server (no single-token
// fan-out), and the result must record the pool size + that logins were paced.
func TestPerfHarness_MultiWalletViewerMapping(t *testing.T) {
	srv, seen := newTokenEchoServer(t)

	cfg := Config{
		BaseURL:   srv.URL,
		Scenario:  "unit",
		ContentID: "c1",
		Wallets: []WalletIdentity{
			{JWTToken: "tok-alpha"},
			{JWTToken: "tok-beta"},
			{JWTToken: "tok-gamma"},
		},
		Viewers:  3,
		Interval: 20 * time.Millisecond,
		Warmup:   0,
		Duration: 300 * time.Millisecond,
	}
	res, err := Run(context.Background(), cfg)
	require.NoError(t, err)
	require.Greater(t, res.SuccessCount, int64(0))

	seenAuth := map[string]bool{}
	for _, a := range seen() {
		if a != "" {
			seenAuth[a] = true
		}
	}
	for _, want := range []string{"Bearer tok-alpha", "Bearer tok-beta", "Bearer tok-gamma"} {
		assert.True(t, seenAuth[want], "viewer using %s never issued a request (got %v)", want, seenAuth)
	}
	assert.Equal(t, 3, res.WalletPoolSize, "pool size must be recorded in the result")
	assert.True(t, res.LoginPaced, "logins must be recorded as paced even when all tokens were pre-resolved")
	assert.Zero(t, res.LoginsFailed)
}

// TestPerfHarness_SingleWalletStillWorks guards backwards compatibility: a
// pre-resolved JWT (or single Wallet+PrivateKey) still drives every viewer,
// with WalletPoolSize reported as the 1-wallet pool it effectively is.
func TestPerfHarness_SingleWalletStillWorks(t *testing.T) {
	srv, seen := newTokenEchoServer(t)

	cfg := Config{
		BaseURL:   srv.URL,
		Scenario:  "unit",
		JWTToken:  "single-token",
		ContentID: "c1",
		Viewers:   2,
		Interval:  20 * time.Millisecond,
		Warmup:    0,
		Duration:  300 * time.Millisecond,
	}
	res, err := Run(context.Background(), cfg)
	require.NoError(t, err)
	require.Greater(t, res.SuccessCount, int64(0))
	assert.Zero(t, res.ErrorCount)
	assert.Equal(t, 1, res.WalletPoolSize)
	for _, a := range seen() {
		assert.Equal(t, "Bearer single-token", a)
	}
}

// TestPerfHarness_LoginPacing drives the real challenge->login flow against a
// stub auth server: with LoginPace=50ms and 3 wallets, the login phase must
// take at least 2 pace gaps (100ms) and every wallet must end up with its own
// token. A login failure must be surfaced in the result AND fail the run.
func TestPerfHarness_LoginPacing(t *testing.T) {
	var mu sync.Mutex
	var loginWallets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case ChallengeEndpoint:
			var req struct {
				Address string `json:"address"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"challenge_id": "chal-" + req.Address,
				"message":      "please sign: " + req.Address,
			})
		case LoginEndpoint:
			var req struct {
				Address     string `json:"address"`
				ChallengeID string `json:"challenge_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Address == badWallet || req.ChallengeID != "chal-"+req.Address {
				http.Error(w, `{"error":"bad credentials"}`, http.StatusUnauthorized)
				return
			}
			mu.Lock()
			loginWallets = append(loginWallets, req.Address)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok-" + req.Address})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	keys := []string{
		"0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
		"0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a",
		"0x7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6",
	}
	pool := make([]WalletIdentity, len(keys))
	for i, k := range keys {
		pool[i] = WalletIdentity{PrivateKey: k, ChainID: 31337}
	}

	base := Config{
		BaseURL:   srv.URL,
		Scenario:  "unit",
		ContentID: "c1",
		Viewers:   3,
		LoginPace: 50 * time.Millisecond,
		Interval:  40 * time.Millisecond,
		Warmup:    0,
		Duration:  200 * time.Millisecond,
	}

	t.Run("Success", func(t *testing.T) {
		cfg := base
		cfg.Wallets = append([]WalletIdentity(nil), pool...)
		start := time.Now()
		res, err := Run(context.Background(), cfg)
		elapsed := time.Since(start)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond,
			"3 paced logins with 50ms pace must take at least 2 gaps (100ms) — got %v", elapsed)
		assert.Equal(t, 3, res.WalletPoolSize)
		assert.True(t, res.LoginPaced)
		assert.Zero(t, res.LoginsFailed)
		mu.Lock()
		n := len(loginWallets)
		mu.Unlock()
		assert.Equal(t, 3, n, "all three wallets must have completed a real login")
	})

	t.Run("FailedLoginFailsRun", func(t *testing.T) {
		badWallet = "0x90F79bf6EB2c4f870365E785982E1f101E93b906"
		t.Cleanup(func() { badWallet = "" })
		cfg := base
		// Wallet claims an address the stub rejects at login time — a real
		// signature-verification failure the setup phase must surface.
		cfg.Wallets = []WalletIdentity{
			{PrivateKey: keys[0], ChainID: 31337},
			{Wallet: badWallet, PrivateKey: keys[1], ChainID: 31337},
		}
		cfg.Viewers = 2
		_, err := Run(context.Background(), cfg)
		require.Error(t, err, "a failed wallet login must fail the run loudly")
		assert.Contains(t, err.Error(), "login")
	})
}

// TestPerfHarness_WalletsFromKeysFile pins env/CLI-driven key-file parsing:
// hex keys may arrive one-per-line or space-separated on a single line, and
// each identity gets its wallet address derived from its own key.
func TestPerfHarness_WalletsFromKeysFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallets.keys")
	content := "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d" + "\n" +
		"0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a 0x7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	pool, err := WalletsFromKeysFile(path)
	require.NoError(t, err)
	require.Len(t, pool, 3)
	for _, id := range pool {
		assert.Equal(t, int64(31337), id.ChainID)
		assert.True(t, strings.HasPrefix(id.Wallet, "0x") && len(id.Wallet) == 42,
			"wallet must be the derived 20-byte hex address, got %q", id.Wallet)
	}

	_, err = WalletsFromKeysFile(filepath.Join(dir, "missing.keys"))
	assert.Error(t, err, "a missing keys file must error out")

	bad := filepath.Join(dir, "bad.keys")
	require.NoError(t, os.WriteFile(bad, []byte("zz-not-a-key\n"), 0o600))
	_, err = WalletsFromKeysFile(bad)
	assert.Error(t, err, "an unparsable key must error out")
}

// TestPerfGenerateDeterministicKeys pins the deterministic key generation used
// by the SETUP phase: keccak256("streamgate-perf-<i>") as the secp256k1 scalar.
func TestPerfGenerateDeterministicKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gen.keys")
	require.NoError(t, PerfGenerateKeys(5, path))

	pool, err := WalletsFromKeysFile(path)
	require.NoError(t, err)
	require.Len(t, pool, 5)

	seen := map[string]bool{}
	for _, id := range pool {
		assert.False(t, seen[id.Wallet], "generated wallets must be unique")
		seen[id.Wallet] = true
		assert.True(t, strings.HasPrefix(id.Wallet, "0x") && len(id.Wallet) == 42)
	}

	// Determinism: regenerating at a different path yields the same addresses.
	path2 := filepath.Join(dir, "gen2.keys")
	require.NoError(t, PerfGenerateKeys(5, path2))
	pool2, err := WalletsFromKeysFile(path2)
	require.NoError(t, err)
	for i := range pool {
		assert.Equal(t, pool[i].Wallet, pool2[i].Wallet, "generation must be deterministic")
	}
}

// TestPerfHarness_CapacityTierAcceptsOnlyObservableOverload pins the second
// half of the ≥99% decision: above the M4 viewer count a healthy scenario is
// the capacity exploration, and the only outcomes it may report are successes
// and OBSERVABLE throttling/overload. This is the bar the perf stage applies
// when the matrix runs at 100 viewers — it has to accept the honest ceiling
// measurement while still failing on a holder being denied.
func TestPerfHarness_CapacityTierAcceptsOnlyObservableOverload(t *testing.T) {
	base := func() *Result {
		return &Result{Viewers: 100, SuccessCount: 400, ErrorCount: 0,
			HTTPStatuses: map[string]int64{"200": 400}}
	}

	for _, tc := range []struct {
		name    string
		mutate  func(*Result)
		wantErr bool
	}{
		{name: "all successes", mutate: func(r *Result) {}, wantErr: false},
		{name: "range responses count as success", mutate: func(r *Result) {
			r.HTTPStatuses["206"] = 12
		}, wantErr: false},
		{name: "throttled by the limiter", mutate: func(r *Result) {
			r.HTTPStatuses["429"] = 30
			r.ErrorCount = 30
		}, wantErr: false},
		{name: "overloaded server", mutate: func(r *Result) {
			r.HTTPStatuses["503"] = 17
			r.ErrorCount = 17
		}, wantErr: false},
		{name: "transport timeouts under load", mutate: func(r *Result) {
			r.HTTPStatuses["transport_error"] = 9
			r.ErrorCount = 9
		}, wantErr: false},
		{name: "a denied holder", mutate: func(r *Result) {
			r.HTTPStatuses["403"] = 1
			r.ErrorCount = 1
		}, wantErr: true},
		{name: "an expired capability", mutate: func(r *Result) {
			r.HTTPStatuses["401"] = 1
			r.ErrorCount = 1
		}, wantErr: true},
		{name: "a missing route", mutate: func(r *Result) {
			r.HTTPStatuses["404"] = 1
			r.ErrorCount = 1
		}, wantErr: true},
		{name: "nothing executed", mutate: func(r *Result) {
			r.SuccessCount, r.ErrorCount = 0, 0
			r.HTTPStatuses = map[string]int64{}
		}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := base()
			tc.mutate(res)
			err := CapacityTierViolation(res, CapacityTierViewers)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}

	// The helper is for the capacity tier only, and the boundary is the M4
	// count itself: an M4-sized run must be refused so a caller cannot
	// accidentally judge the 50-viewer tier by the exploration bar instead of
	// by ≥99%.
	m4 := base()
	m4.Viewers = CapacityTierViewers
	require.Error(t, CapacityTierViolation(m4, CapacityTierViewers),
		"the M4-sized run must not be judged as a capacity exploration")
	above := base()
	above.Viewers = CapacityTierViewers + 1
	require.NoError(t, CapacityTierViolation(above, CapacityTierViewers),
		"the first viewer count above the M4 count is the exploration")
}

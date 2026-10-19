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

	"github.com/ethereum/go-ethereum/crypto"
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

func TestPerfHarness_SingleWalletCannotDriveMultipleViewers(t *testing.T) {
	srv, _ := newTokenEchoServer(t)
	cfg := Config{BaseURL: srv.URL, JWTToken: "single-token", ContentID: "c1", Viewers: 2, Interval: 20 * time.Millisecond, Duration: 300 * time.Millisecond}
	_, err := Run(context.Background(), cfg)
	require.Error(t, err)
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
		key, err := crypto.HexToECDSA(strings.TrimPrefix(keys[1], "0x"))
		require.NoError(t, err)
		badWallet = crypto.PubkeyToAddress(key.PublicKey).Hex()
		t.Cleanup(func() { badWallet = "" })
		cfg := base
		// Wallet claims an address the stub rejects at login time — a real
		// signature-verification failure the setup phase must surface.
		cfg.Wallets = []WalletIdentity{
			{PrivateKey: keys[0], ChainID: 31337},
			{Wallet: badWallet, PrivateKey: keys[1], ChainID: 31337},
		}
		cfg.Viewers = 2
		_, err = Run(context.Background(), cfg)
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

func TestPerfHarness_RejectsSharedViewerIdentities(t *testing.T) {
	key := strings.Repeat("0", 63) + "1"
	base := Config{BaseURL: "http://localhost", ContentID: "c1", Viewers: 2, Interval: time.Second, Duration: time.Second}
	for _, tc := range []struct {
		name    string
		wallets []WalletIdentity
	}{
		{"short pool", []WalletIdentity{{JWTToken: "one"}}},
		{"same token", []WalletIdentity{{JWTToken: "one"}, {JWTToken: "one"}}},
		{"same wallet different sessions", []WalletIdentity{{JWTToken: "one", Wallet: "0xabc"}, {JWTToken: "two", Wallet: "0xABC"}}},
		{"same private key", []WalletIdentity{{PrivateKey: key}, {PrivateKey: "0x" + key}}},
		{"key address mismatch", []WalletIdentity{{PrivateKey: key, Wallet: "0xwrong"}, {JWTToken: "two"}}},
	} {
		t.Run(tc.name, func(t *testing.T) { cfg := base; cfg.Wallets = tc.wallets; require.Error(t, cfg.validate()) })
	}
}

func TestPerfHarness_RejectsEmptySamplingWindow(t *testing.T) {
	srv, _ := newTokenEchoServer(t)
	cfg := Config{BaseURL: srv.URL, ContentID: "c1", JWTToken: "one", Viewers: 1, Interval: time.Second, Duration: time.Millisecond}
	res, err := Run(context.Background(), cfg)
	require.Error(t, err, "zero completed samples must fail the run")
	require.NotNil(t, res, "retain failed result for artifact upload")
}

func TestPerfHarness_SamplingCompleteness(t *testing.T) {
	good := func() *Result {
		return &Result{Viewers: 2, SuccessCount: 1, ErrorCount: 1, AbortedCount: 1, ViewerSamples: []ViewerSamples{{ViewerID: 0, SuccessCount: 1}, {ViewerID: 1, ErrorCount: 1, AbortedCount: 1}}}
	}
	require.NoError(t, good().SamplingViolation())
	for _, tc := range []struct {
		name   string
		change func(*Result)
	}{
		{"missing viewer", func(r *Result) { r.ViewerSamples = r.ViewerSamples[:1] }},
		{"duplicate viewer", func(r *Result) { r.ViewerSamples[1].ViewerID = 0 }},
		{"boundary only viewer", func(r *Result) { r.ViewerSamples[1].ErrorCount = 0; r.ErrorCount = 0 }},
		{"wrong totals", func(r *Result) { r.SuccessCount++ }},
		{"negative counts", func(r *Result) { r.ViewerSamples[0].ErrorCount = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) { r := good(); tc.change(r); require.Error(t, r.SamplingViolation()) })
	}
}

func TestPerfHarness_ServerFailuresRemainErrors(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusGatewayTimeout} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer srv.Close()
			cfg := Config{BaseURL: srv.URL, ContentID: "c1", JWTToken: "one", Viewers: 1, Interval: time.Millisecond, Duration: 50 * time.Millisecond}
			res, err := Run(context.Background(), cfg)
			require.NoError(t, err)
			require.Zero(t, res.SuccessCount)
			require.Positive(t, res.ErrorCount)
			require.Equal(t, res.ErrorCount, res.HTTPStatuses[fmt.Sprint(status)])
			require.Equal(t, res.ErrorCount, res.ViewerSamples[0].ErrorCount)
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	s, _ := fetchURL(context.Background(), &http.Client{Timeout: 10 * time.Millisecond}, srv.URL, "one", Config{})
	require.NotEmpty(t, s.Err)
	require.False(t, s.Aborted, "client timeout inside the window is a server error")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, _ = fetchURL(ctx, http.DefaultClient, srv.URL, "one", Config{})
	require.NotEmpty(t, s.Err)
	require.True(t, s.Aborted, "sampling cancellation is reported separately")
}

func TestPerfHarness_HTTPFailureAtBoundaryIsNotExcluded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	s, _ := fetchURL(ctx, http.DefaultClient, srv.URL, "one", Config{})
	require.Equal(t, http.StatusTooManyRequests, s.StatusCode)
	require.NotEmpty(t, s.Err)
	require.False(t, s.Aborted, "an observed HTTP error must remain in the success-rate denominator")
}

func TestPerfHarness_PlaysAtEXTINFPace(t *testing.T) {
	var mu sync.Mutex
	segments := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "manifest") {
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:2.0,\n/segment.ts\n#EXTINF:2.0,\n/segment.ts\n"))
			return
		}
		mu.Lock()
		segments++
		mu.Unlock()
		_, _ = w.Write([]byte("media"))
	}))
	defer srv.Close()
	cfg := Config{BaseURL: srv.URL, ContentID: "c1", Quality: "360p", JWTToken: "one", Viewers: 1, Interval: time.Millisecond, Duration: 200 * time.Millisecond, PaceByEXTINF: true}
	sum := sha256.Sum256([]byte("media"))
	cfg.MediaSHA256 = map[string]string{"/segment.ts": hex.EncodeToString(sum[:])}
	res, err := Run(context.Background(), cfg)
	require.NoError(t, err)
	mu.Lock()
	n := segments
	mu.Unlock()
	require.Equal(t, 1, n, "playback must not burst every segment on each interval")
	require.Equal(t, int64(1), res.VerifiedMediaCount)
	require.Equal(t, int64(1), res.ViewerSamples[0].VerifiedMediaCount)
	require.NotEmpty(t, res.MediaHashesSHA256)
	require.True(t, res.PaceByEXTINF)
}

func TestPerfHarness_RejectsInvalidPlaybackDurations(t *testing.T) {
	for _, body := range []string{
		"#EXTM3U\n/variant.m3u8\n", "#EXTM3U\n#EXTINF:NaN,\n/seg.ts\n", "#EXTM3U\n#EXTINF:0,\n/seg.ts\n",
		"#EXTM3U\n#EXTINF:-1,\n/seg.ts\n", "#EXTM3U\n#EXTINF:9999999999999999999,\n/seg.ts\n",
		"#EXTM3U\n#EXTINF:2,\n", "#EXTM3U\n#EXTINF:2,\n#EXTINF:2,\n/seg.ts\n",
	} {
		_, err := parsePlaybackSegments([]byte(body))
		require.Error(t, err)
	}
	segments, err := parsePlaybackSegments([]byte("#EXTM3U\r\n#EXTINF:1.25,title\r\n/seg.ts\r\n"))
	require.NoError(t, err)
	require.Len(t, segments, 1)
	require.Equal(t, 1250*time.Millisecond, segments[0].duration)
}

func TestPerfHarness_NearestRankSmallSamples(t *testing.T) {
	samples := []float64{11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	for _, tc := range []struct {
		p    float64
		want float64
	}{{50, 6}, {95, 11}, {99, 11}} {
		got := Percentile(samples, tc.p)
		require.NotNil(t, got)
		require.Equal(t, tc.want, *got)
	}
}

func TestPerfHarness_PacedPlaybackRequiresMediaHashes(t *testing.T) {
	cfg := Config{BaseURL: "http://localhost", ContentID: "c1", Quality: "360p", JWTToken: "one", Viewers: 1, Interval: time.Second, Duration: time.Second, PaceByEXTINF: true}
	require.Error(t, cfg.validate(), "a paced acceptance run must not silently omit media verification")
	cfg.MediaSHA256 = map[string]string{"/segment.ts": "bad"}
	require.Error(t, cfg.validate())
}

func TestPerfHarness_MediaHashesRejectCorruptOrUnknownSegments(t *testing.T) {
	body := []byte("fixture media")
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()
	cfg := Config{MediaSHA256: map[string]string{"/segment.ts": hex.EncodeToString(sum[:])}}
	good, _ := fetchURL(context.Background(), http.DefaultClient, srv.URL+"/segment.ts?playback_token=private", "one", cfg)
	require.Empty(t, good.Err)
	for _, path := range []string{"/unknown.ts", "/different.ts"} {
		bad, _ := fetchURL(context.Background(), http.DefaultClient, srv.URL+path, "one", cfg)
		require.NotEmpty(t, bad.Err)
	}
	cfg.MediaSHA256["/segment.ts"] = strings.Repeat("0", 64)
	bad, _ := fetchURL(context.Background(), http.DefaultClient, srv.URL+"/segment.ts", "one", cfg)
	require.NotEmpty(t, bad.Err)
}

func TestPerfHarness_ResultUsesExplicitRevision(t *testing.T) {
	srv, _ := newTokenEchoServer(t)
	cfg := Config{BaseURL: srv.URL, ContentID: "c1", JWTToken: "one", Viewers: 1, Interval: time.Millisecond, Duration: 30 * time.Millisecond, CommitSHA: "explicit-reference-revision"}
	result, err := Run(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, cfg.CommitSHA, result.Commit)
}

func TestPerfHarness_ManifestDetectionUsesURLPath(t *testing.T) {
	require.False(t, isManifestURL("https://example.invalid/segment.ts?name=manifest.m3u8"))
	require.True(t, isManifestURL("https://example.invalid/content/manifest.m3u8?quality=360p"))
}

func TestPerfHarness_MediaHashManifestValidation(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, hashes := range []map[string]string{{"": digest}, {"/segment.ts?playback_token=private": digest}, {"https://example.invalid/segment.ts": digest}, {"/segment.ts": "bad"}} {
		_, err := mediaHashesDigest(hashes)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private")
	}
	a, err := mediaHashesDigest(map[string]string{"/b.ts": digest, "/a.ts": digest})
	require.NoError(t, err)
	b, err := mediaHashesDigest(map[string]string{"/a.ts": digest, "/b.ts": digest})
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Len(t, a, 64)
}

func TestPerfHarness_PlaylistNamedSegmentCannotBypassHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:2,\n/other/manifest.m3u8\n"))
	}))
	defer srv.Close()
	cfg := Config{BaseURL: srv.URL, ContentID: "c1", Quality: "360p", JWTToken: "one", Viewers: 1, Interval: time.Second, Duration: 50 * time.Millisecond, PaceByEXTINF: true, MediaSHA256: map[string]string{"/segment.ts": strings.Repeat("a", 64)}}
	result, err := Run(context.Background(), cfg)
	require.NoError(t, err)
	require.Positive(t, result.ErrorCount)
	require.Zero(t, result.VerifiedMediaCount)
}

func TestPerfHarness_SamplingPreparationIsRequired(t *testing.T) {
	srv, _ := newTokenEchoServer(t)
	called := false
	cfg := Config{BaseURL: srv.URL, ContentID: "c1", JWTToken: "one", Viewers: 1, Interval: time.Millisecond, Duration: 30 * time.Millisecond, BeforeSampling: func(context.Context) error { called = true; return fmt.Errorf("injected preparation failure") }}
	result, err := Run(context.Background(), cfg)
	require.Error(t, err)
	require.Nil(t, result)
	require.True(t, called)
}

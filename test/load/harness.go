// Package load holds the SG-07 real-media HTTP load harness.
//
// harness.go is NOT build-tagged so its core (request execution, validation,
// statistics, JSON reporting) compiles and unit-tests in the default test run.
// The live end-to-end entry point that needs a running fullchain stack lives in
// loadtest_test.go behind the `load` build tag:
//
//	PERF_JWT=... PERF_CONTENT_ID=... go test -tags load -run TestLoad ./test/load/ -v
//
// Design rules (TRANSITION_PLAN.md SG-07):
//   - Every counted request is a real HTTP GET that reads the ENTIRE response
//     body. A non-2xx status, a truncated body, or a failed expectation is an
//     error and NEVER counts as success throughput.
//   - No fabricated tokens: the JWT comes from the environment or from the
//     service's real challenge -> sign -> login flow with a real private key.
//   - Results are structured JSON (commit SHA, fixture hash, per-scenario
//     stats: success count, actual bytes, HTTP error distribution, TTFB,
//     p50/p95/p99, duration).
package load

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// LoginEndpoint is the public wallet-login route (no JWT needed).
const LoginEndpoint = "/api/v1/auth/login"

// ChallengeEndpoint is the public wallet-challenge route.
const ChallengeEndpoint = "/api/v1/auth/challenge"

// Config drives one load scenario. Zero values are rejected by Run; everything
// is deliberately explicit so a JSON result is reproducible from its inputs.
type Config struct {
	// BaseURL is the running stack root, e.g. http://localhost:18080.
	BaseURL string
	// Scenario labels the result file (cold_cache / warm_cache /
	// rpc_primary_down / rpc_all_down). The harness only records the label —
	// fault injection is orchestrated outside (scripts/perf-acceptance.sh).
	Scenario string
	// CommitSHA is stamped into the result; CommitSHA() resolves it when empty.
	CommitSHA string
	// FixtureHash is the sha256 of the media fixture used to prepare this
	// content; recorded so runs are comparable.
	FixtureHash string

	// Either JWTToken (pre-minted) or Wallet+PrivateKey (real login) must be set.
	JWTToken   string
	Wallet     string
	PrivateKey string // hex-encoded secp256k1 key; enables challenge->login
	ChainID    int64

	// ContentID is the gated content to play; Quality picks the variant
	// (empty = master manifest).
	ContentID string
	Quality   string

	// Viewers is the number of virtual viewers; Interval is each viewer's
	// request pace (one manifest + all its segments per tick).
	Viewers  int
	Interval time.Duration
	// Warmup runs before sampling; Duration is the timed sampling window.
	Warmup   time.Duration
	Duration time.Duration

	// Expect validates a fully-read body, e.g. sha256 equality per segment.
	// When nil, the default validation applies (2xx + non-truncated body +
	// "#EXTM3U" for manifests). An Expect failure is an error, not success.
	Expect func(url string, body []byte) error
}

// Sample is one executed request.
type Sample struct {
	URL        string
	StatusCode int
	TTFB       time.Duration
	Duration   time.Duration
	Bytes      int64
	Err        string // empty on success
	// Aborted marks a request cut short by the sampling window closing (context
	// cancellation/deadline). Aborted samples are neither successes nor errors:
	// they are a harness boundary artifact, not a server failure.
	Aborted bool
}

func (s Sample) ok() bool { return s.Err == "" }

// Result is the structured JSON output (one file per scenario).
type Result struct {
	Commit      string  `json:"commit"`
	Scenario    string  `json:"scenario"`
	FixtureHash string  `json:"fixture_hash,omitempty"`
	BaseURL     string  `json:"base_url"`
	ContentID   string  `json:"content_id"`
	Quality     string  `json:"quality,omitempty"`
	Viewers     int     `json:"viewers"`
	IntervalMS  int64   `json:"interval_ms"`
	WarmupSec   float64 `json:"warmup_sec"`
	DurationSec float64 `json:"duration_sec"`

	SuccessCount  int64            `json:"success_count"`
	ErrorCount    int64            `json:"error_count"`
	AbortedCount  int64            `json:"aborted_count"`
	TotalBytes    int64            `json:"total_bytes"`
	HTTPStatuses  map[string]int64 `json:"http_statuses"`
	TTFBMillis    Percentiles      `json:"ttfb_ms"`
	LatencyMillis Percentiles      `json:"latency_ms"`
	StartedAt     time.Time        `json:"started_at"`
	FinishedAt    time.Time        `json:"finished_at"`
}

// Percentiles holds p50/p95/p99 over a sample set.
type Percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

// Percentile computes the nearest-rank percentile of samples (sorted copy);
// nil for an empty set. samples are in milliseconds.
func Percentile(samples []float64, p float64) *float64 {
	if len(samples) == 0 {
		return nil
	}
	cp := make([]float64, len(samples))
	copy(cp, samples)
	sort.Float64s(cp)
	rank := int(p/100*float64(len(cp))+0.5) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(cp) {
		rank = len(cp) - 1
	}
	v := cp[rank]
	return &v
}

// ComputePercentiles derives p50/p95/p99 from raw samples.
func ComputePercentiles(samples []float64) Percentiles {
	var out Percentiles
	if v := Percentile(samples, 50); v != nil {
		out.P50 = *v
	}
	if v := Percentile(samples, 95); v != nil {
		out.P95 = *v
	}
	if v := Percentile(samples, 99); v != nil {
		out.P99 = *v
	}
	return out
}

func (c Config) validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("Config.BaseURL is required")
	}
	if _, err := url.Parse(c.BaseURL); err != nil {
		return fmt.Errorf("invalid BaseURL: %w", err)
	}
	if c.JWTToken == "" && (c.Wallet == "" || c.PrivateKey == "") {
		return fmt.Errorf("either JWTToken or (Wallet + PrivateKey) must be provided")
	}
	if c.PrivateKey != "" {
		if _, err := crypto.HexToECDSA(strings.TrimPrefix(c.PrivateKey, "0x")); err != nil {
			return fmt.Errorf("invalid PrivateKey: %w", err)
		}
	}
	if c.ContentID == "" {
		return fmt.Errorf("Config.ContentID is required")
	}
	if c.Viewers <= 0 {
		return fmt.Errorf("Config.Viewers must be > 0, got %d", c.Viewers)
	}
	if c.Interval <= 0 {
		return fmt.Errorf("Config.Interval must be > 0, got %v", c.Interval)
	}
	if c.Duration <= 0 {
		return fmt.Errorf("Config.Duration must be > 0, got %v", c.Duration)
	}
	return nil
}

// CommitSHA returns cfg-level override, then PERF_COMMIT env, then `git rev-parse HEAD`.
func CommitSHA() string {
	if v := os.Getenv("PERF_COMMIT"); v != "" {
		return v
	}
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// WalletLogin performs the real challenge -> sign -> login flow and returns the
// JWT. No self-signed tokens are ever fabricated (SG-05/SG-07 rule).
func WalletLogin(ctx context.Context, baseURL, walletHex, privKeyHex string, chainID int64) (string, error) {
	key, err := crypto.HexToECDSA(strings.TrimPrefix(privKeyHex, "0x"))
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	if walletHex == "" {
		walletHex = crypto.PubkeyToAddress(key.PublicKey).Hex()
	}

	var chalResp struct {
		ChallengeID string `json:"challenge_id"`
		Message     string `json:"message"`
	}
	chalBody, _ := json.Marshal(map[string]any{"address": walletHex, "chain_id": chainID})
	if err := postJSON(ctx, baseURL+ChallengeEndpoint, chalBody, &chalResp); err != nil {
		return "", fmt.Errorf("challenge: %w", err)
	}
	if chalResp.ChallengeID == "" || chalResp.Message == "" {
		return "", fmt.Errorf("challenge response missing challenge_id/message")
	}

	// EIP-191 personal_sign prefix — must match what the server verifies.
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(chalResp.Message), chalResp.Message)
	hash := crypto.Keccak256([]byte(prefixed))
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		return "", fmt.Errorf("sign challenge: %w", err)
	}
	sig[64] += 27
	signature := "0x" + hex.EncodeToString(sig)

	var loginResp struct {
		Token string `json:"token"`
	}
	loginBody, _ := json.Marshal(map[string]any{
		"address": walletHex, "challenge_id": chalResp.ChallengeID,
		"signature": signature, "chain_id": chainID,
	})
	if err := postJSON(ctx, baseURL+LoginEndpoint, loginBody, &loginResp); err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	if loginResp.Token == "" {
		return "", fmt.Errorf("login response missing token")
	}
	return loginResp.Token, nil
}

func postJSON(ctx context.Context, urlStr string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s -> %d: %s", urlStr, resp.StatusCode, truncate(raw, 200))
	}
	return json.Unmarshal(raw, out)
}

// fetchURL performs a real GET, records TTFB, reads the FULL body and
// validates it. Anything but a fully-read, expectation-satisfying 2xx is an
// error — never counted as success throughput. On success the (validated,
// fully-read) body is returned so the caller can parse a manifest once and
// reuse it for the segment fan-out.
func fetchURL(ctx context.Context, client *http.Client, urlStr, token string, cfg Config) (Sample, []byte) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, http.NoBody)
	if err != nil {
		return Sample{URL: urlStr, Err: "build request: " + err.Error()}, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		s := Sample{URL: urlStr, Duration: time.Since(start), Err: "do request: " + err.Error()}
		if ctx.Err() != nil {
			s.Aborted = true
		}
		return s, nil
	}
	defer resp.Body.Close()

	ttfb := time.Since(start)
	body, err := io.ReadAll(resp.Body)
	end := time.Since(start)
	s := Sample{URL: urlStr, StatusCode: resp.StatusCode, TTFB: ttfb, Duration: end, Bytes: int64(len(body))}
	if err != nil {
		s.Err = "read body: " + err.Error()
		if ctx.Err() != nil {
			s.Aborted = true
		}
		return s, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.Err = fmt.Sprintf("http %d: %s", resp.StatusCode, truncate(body, 200))
		return s, nil
	}
	// Truncated body detection: a short read against Content-Length is a
	// failure even when the status line said 200.
	if resp.ContentLength >= 0 && int64(len(body)) != resp.ContentLength {
		s.Err = fmt.Sprintf("truncated body: got %d bytes, Content-Length %d", len(body), resp.ContentLength)
		return s, nil
	}
	if cfg.Expect != nil {
		if err := cfg.Expect(urlStr, body); err != nil {
			s.Err = "expectation failed: " + err.Error()
			return s, nil
		}
	}
	if isManifestURL(urlStr) && !bytes.Contains(body, []byte("#EXTM3U")) {
		s.Err = "manifest body missing #EXTM3U header"
		return s, nil
	}
	return s, body
}

func isManifestURL(u string) bool {
	return strings.Contains(u, "manifest.m3u8")
}

// ParseManifestURIs extracts the media/variant URIs of an HLS playlist,
// skipping comment/tag lines.
func ParseManifestURIs(body []byte) []string {
	var uris []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		uris = append(uris, line)
	}
	return uris
}

func (c Config) manifestURL() string {
	u := c.BaseURL + "/api/v1/streaming/" + url.PathEscape(c.ContentID) + "/manifest.m3u8"
	if c.Quality != "" {
		u += "?quality=" + url.QueryEscape(c.Quality)
	}
	return u
}

// resolveURI normalises an HLS playlist URI against BaseURL. Playlist URIs may
// be absolute paths ("/api/..."), relative segments, or full URLs carrying the
// server-issued playback_token query.
func resolveURI(baseURL, uri string) string {
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		return uri
	}
	base := strings.TrimSuffix(baseURL, "/")
	if strings.HasPrefix(uri, "/") {
		return base + uri
	}
	return base + "/" + uri
}

// viewerLoop runs one virtual viewer: paced ticks of manifest fetch + every
// referenced segment, for as long as ctx allows. Recording is gated by the
// shared recorder so warmup samples can be discarded.
func viewerLoop(ctx context.Context, client *http.Client, cfg Config, jwt string, rec *recorder) {
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m, body := fetchURL(ctx, client, cfg.manifestURL(), jwt, cfg)
			rec.record(m)
			if !m.ok() {
				continue
			}
			for _, uri := range ParseManifestURIs(body) {
				s, _ := fetchURL(ctx, client, resolveURI(cfg.BaseURL, uri), jwt, cfg)
				rec.record(s)
			}
		}
	}
}

type recorder struct {
	mu        sync.Mutex
	samples   []Sample
	recording bool
}

func (r *recorder) record(s Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.recording {
		return
	}
	r.samples = append(r.samples, s)
}

func (r *recorder) setRecording(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording = on
}

func (r *recorder) snapshot() []Sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]Sample, len(r.samples))
	copy(cp, r.samples)
	return cp
}

// Run executes the scenario: resolve JWT (env-provided or real login), warm
// up, then sample for Config.Duration, and aggregate into a Result.
func Run(ctx context.Context, cfg Config) (*Result, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	jwt := cfg.JWTToken
	if jwt == "" {
		var err error
		jwt, err = WalletLogin(ctx, cfg.BaseURL, cfg.Wallet, cfg.PrivateKey, cfg.ChainID)
		if err != nil {
			return nil, fmt.Errorf("real wallet login failed: %w", err)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}

	rec := &recorder{}
	rec.setRecording(false)

	// Warmup: discard samples but exercise the same paths as the sampling phase.
	warmupCtx, cancelWarmup := context.WithTimeout(ctx, cfg.Warmup)
	runViewers(warmupCtx, client, cfg, jwt, rec)
	cancelWarmup()

	// Sampling: record everything.
	sampleCtx, cancelSample := context.WithTimeout(ctx, cfg.Duration)
	rec.setRecording(true)
	started := time.Now()
	runViewers(sampleCtx, client, cfg, jwt, rec)
	cancelSample()
	finished := time.Now()

	samples := rec.snapshot()
	res := &Result{
		Commit:       CommitSHA(),
		Scenario:     cfg.Scenario,
		FixtureHash:  cfg.FixtureHash,
		BaseURL:      cfg.BaseURL,
		ContentID:    cfg.ContentID,
		Quality:      cfg.Quality,
		Viewers:      cfg.Viewers,
		IntervalMS:   cfg.Interval.Milliseconds(),
		WarmupSec:    cfg.Warmup.Seconds(),
		DurationSec:  cfg.Duration.Seconds(),
		HTTPStatuses: map[string]int64{},
		StartedAt:    started,
		FinishedAt:   finished,
	}
	var ttfbs, latencies []float64
	for _, s := range samples {
		// Aborted samples straddle the sampling-window boundary and are
		// neither success nor error; they are still visible in the status
		// distribution for transparency.
		key := fmt.Sprintf("%d", s.StatusCode)
		if s.StatusCode == 0 {
			key = "transport_error"
		}
		res.HTTPStatuses[key]++
		if s.Aborted {
			res.AbortedCount++
			continue
		}
		if s.ok() {
			res.SuccessCount++
			res.TotalBytes += s.Bytes
			ttfbs = append(ttfbs, float64(s.TTFB.Microseconds())/1000.0)
			latencies = append(latencies, float64(s.Duration.Microseconds())/1000.0)
		} else {
			res.ErrorCount++
		}
	}
	res.TTFBMillis = ComputePercentiles(ttfbs)
	res.LatencyMillis = ComputePercentiles(latencies)
	return res, nil
}

func runViewers(ctx context.Context, client *http.Client, cfg Config, jwt string, rec *recorder) {
	var wg sync.WaitGroup
	for i := 0; i < cfg.Viewers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			viewerLoop(ctx, client, cfg, jwt, rec)
		}()
	}
	wg.Wait()
}

// WriteJSON writes the result to path (0600: results may reference a run's
// environment, never secrets, but stay private by default).
func (r *Result) WriteJSON(path string) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// ExpectedSHA256 builds an Expect function asserting the sha256 of each fully
// read body equals want (lowercase hex). Used by callers that precomputed
// segment digests from the fixture.
func ExpectedSHA256(want string) func(string, []byte) error {
	want = strings.ToLower(strings.TrimSpace(want))
	return func(u string, body []byte) error {
		sum := sha256.Sum256(body)
		got := hex.EncodeToString(sum[:])
		if got != want {
			return fmt.Errorf("sha256 mismatch for %s: got %s want %s", u, got, want)
		}
		return nil
	}
}

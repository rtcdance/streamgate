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
//   - Every virtual viewer uses its OWN real-logged-in wallet (a wallet pool,
//     one identity per viewer where the pool allows) so the per-wallet media
//     limiter (SG-14) measures playback, not limiter starvation from a single
//     shared wallet.
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

// DevMintEndpoint mints DemoNFTs to the AUTHENTICATED wallet (dev-only route;
// enabled on the local fullchain stack). The SETUP phase uses it to make each
// generated test wallet NFT-holding before it becomes a viewer.
const DevMintEndpoint = "/api/v1/nft/dev/mint"

// WalletIdentity is one pre-resolved viewer wallet: either a JWTToken that is
// already a live session token, or a PrivateKey the harness uses to perform a
// real challenge -> sign -> login flow. Wallet is optional (derived from
// PrivateKey when empty) and only used for logging / setup bookkeeping.
type WalletIdentity struct {
	JWTToken   string
	Wallet     string
	PrivateKey string
	ChainID    int64
}

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

	// Either JWTToken (pre-minted) or Wallet+PrivateKey (real login) must be
	// set. When Wallets is provided it takes precedence and this pair is
	// treated as a 1-wallet pool (backwards compatibility).
	JWTToken   string
	Wallet     string
	PrivateKey string // hex-encoded secp256k1 key; enables challenge->login
	ChainID    int64

	// Wallets is the pre-resolved viewer wallet pool: viewer i runs with
	// Wallets[i % len(Wallets)]. Pool size should equal Viewers — reuse
	// (round-robin) is allowed when it is smaller and is recorded in the
	// result as wallet_pool_size.
	Wallets []WalletIdentity
	// LoginPace is the minimum gap between consecutive real logins. The auth
	// limiter is 10 req/min/IP and each login is 2 requests (challenge +
	// login), so the safe default is ceil(60/9) ≈ 7s. Wallets carrying a
	// JWTToken skip the request entirely and consume no pace slot.
	LoginPace time.Duration
	// SetupMint makes the SETUP phase mint a DemoNFT to every wallet that
	// carried a PrivateKey (after its real login), so each generated test
	// wallet is NFT-holding before it becomes a viewer. Only wired by the
	// SETUP test (load tag); the scenario run itself never mints.
	SetupMint bool

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

	// LoginPaced records that token resolution ran through the paced login
	// phase (real logins are spaced by LoginPace so the auth limiter is never
	// hit); a run that failed a login never produces a Result at all.
	LoginPaced bool `json:"login_paced"`
	// WalletPoolSize is the number of distinct wallets the viewer pool
	// resolved to. Pool size should equal Viewers; a smaller pool means
	// viewers reused wallets round-robin.
	WalletPoolSize int `json:"wallet_pool_size"`
	// LoginsFailed is the number of wallet logins that failed during setup.
	// Any failure aborts the run, so a successful Result always reports 0 —
	// the field exists to make the invariant visible in the JSON.
	LoginsFailed int `json:"logins_failed"`
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
	if len(c.Wallets) == 0 && c.JWTToken == "" && (c.Wallet == "" || c.PrivateKey == "") {
		return fmt.Errorf("one of JWTToken, (Wallet + PrivateKey) or Wallets must be provided")
	}
	if c.PrivateKey != "" {
		if _, err := crypto.HexToECDSA(strings.TrimPrefix(c.PrivateKey, "0x")); err != nil {
			return fmt.Errorf("invalid PrivateKey: %w", err)
		}
	}
	for i, w := range c.Wallets {
		if w.JWTToken == "" && w.PrivateKey == "" {
			return fmt.Errorf("Config.Wallets[%d] needs either JWTToken or PrivateKey", i)
		}
		if w.PrivateKey != "" {
			if _, err := crypto.HexToECDSA(strings.TrimPrefix(w.PrivateKey, "0x")); err != nil {
				return fmt.Errorf("Config.Wallets[%d] invalid PrivateKey: %w", i, err)
			}
		}
	}
	if c.LoginPace < 0 {
		return fmt.Errorf("Config.LoginPace must be >= 0, got %v", c.LoginPace)
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

// defaultLoginPace is ceil(60/9) seconds: the auth limiter allows 10 req/min
// and each real login consumes 2 requests (challenge + login), so ~7s keeps
// any 10-login window below 9 logins even in the worst fixed-window
// alignment. Wallets with a pre-resolved JWTToken cost nothing.
const defaultLoginPace = 7 * time.Second

// minLoginPace is the floor for LoginPace: below 1s a burst of logins can
// still exhaust the 10 req/min auth limiter bucket.
const minLoginPace = 1 * time.Second

// resolveWalletPool collapses Config into the effective viewer wallet pool:
// Wallets when provided, otherwise the single-wallet (JWTToken or
// Wallet+PrivateKey) legacy path as a 1-element pool.
func (c Config) resolveWalletPool() []WalletIdentity {
	if len(c.Wallets) > 0 {
		return c.Wallets
	}
	return []WalletIdentity{{JWTToken: c.JWTToken, Wallet: c.Wallet, PrivateKey: c.PrivateKey, ChainID: c.ChainID}}
}

// WalletsFromKeysFile parses a file of hex secp256k1 private keys — one per
// line or space-separated — into a WalletIdentity pool with each wallet
// address derived from its own key. The SETUP phase (scripts/perf-acceptance.sh)
// feeds it the generated wallets.keys file via PERF_WALLET_KEYS_FILE.
func WalletsFromKeysFile(path string) ([]WalletIdentity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read wallet keys file: %w", err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return nil, fmt.Errorf("wallet keys file %s contains no keys", path)
	}
	pool := make([]WalletIdentity, 0, len(fields))
	for i, f := range fields {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(f, "0x"))
		if err != nil {
			return nil, fmt.Errorf("key %d in %s: %w", i+1, path, err)
		}
		pool = append(pool, WalletIdentity{
			Wallet:     crypto.PubkeyToAddress(key.PublicKey).Hex(),
			PrivateKey: strings.TrimPrefix(f, "0x"),
			ChainID:    31337,
		})
	}
	return pool, nil
}

// PerfGenerateKeys deterministically derives n secp256k1 keys — the keccak256
// of "streamgate-perf-<i>" used as the private scalar — and writes them to
// path (one hex key per line, 0600). Deterministic so re-running SETUP over a
// surviving keys file reproduces the same wallet addresses (and thus the same
// already-minted NFTs). Invoked by the SETUP test under the `load` tag.
func PerfGenerateKeys(n int, path string) error {
	if n <= 0 {
		return fmt.Errorf("key count must be > 0, got %d", n)
	}
	var sb strings.Builder
	for i := 0; i < n; i++ {
		// Guard against the (astronomically unlikely but fatal) case of a
		// scalar that is zero or >= the curve order: tweak the seed until the
		// key derives. Determinism is preserved because the tweak is a pure
		// function of (seed, attempt).
		for attempt := 0; ; attempt++ {
			digest := crypto.Keccak256([]byte(fmt.Sprintf("streamgate-perf-%d-%d", i, attempt)))
			key, err := crypto.ToECDSA(digest)
			if err != nil {
				if attempt > 8 {
					return fmt.Errorf("derive key %d: %w", i, err)
				}
				continue
			}
			sb.WriteString(hex.EncodeToString(crypto.FromECDSA(key)))
			sb.WriteByte('\n')
			break
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0o600)
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

	// The auth limiter keys on ClientIP and, when present, X-Wallet-Address.
	// Sending the wallet's own address scopes this wallet's challenge+login
	// pair to its own bucket, so N concurrent viewers' setups share the
	// per-wallet 10/min budget instead of fighting over one per-IP bucket.
	// Each login still consumes 2 of the wallet's own 10 requests/min.
	walletHeader := http.Header{"X-Wallet-Address": []string{walletHex}}

	var chalResp struct {
		ChallengeID string `json:"challenge_id"`
		Message     string `json:"message"`
	}
	chalBody, _ := json.Marshal(map[string]any{"address": walletHex, "chain_id": chainID})
	if err := postJSON(ctx, baseURL+ChallengeEndpoint, chalBody, &chalResp, walletHeader); err != nil {
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
	if err := postJSON(ctx, baseURL+LoginEndpoint, loginBody, &loginResp, walletHeader); err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	if loginResp.Token == "" {
		return "", fmt.Errorf("login response missing token")
	}
	return loginResp.Token, nil
}

func postJSON(ctx context.Context, urlStr string, body []byte, out any, headers ...http.Header) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for _, h := range headers {
		for k, vs := range h {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
	}
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

// Run executes the scenario: resolve the viewer wallet pool's tokens (env-
// provided or real paced logins), warm up, then sample for Config.Duration,
// and aggregate into a Result. A failed wallet login fails the whole run —
// per the plan, "setup 分批登录，失败使该轮失败".
func Run(ctx context.Context, cfg Config) (*Result, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// Only Viewers wallets can ever be driven, so resolve at most that many:
	// real logins are paced and burning pool entries the run never uses would
	// stall the setup for nothing.
	runCfg := cfg
	if pool := cfg.resolveWalletPool(); len(pool) > cfg.Viewers {
		runCfg.Wallets = pool[:cfg.Viewers]
	}

	tokens, loginPaced, loginsFailed, err := RunPhase(ctx, runCfg)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}

	rec := &recorder{}
	rec.setRecording(false)

	// Warmup: discard samples but exercise the same paths as the sampling phase.
	warmupCtx, cancelWarmup := context.WithTimeout(ctx, cfg.Warmup)
	runViewers(warmupCtx, client, cfg, tokens, rec)
	cancelWarmup()

	// Sampling: record everything.
	sampleCtx, cancelSample := context.WithTimeout(ctx, cfg.Duration)
	rec.setRecording(true)
	started := time.Now()
	runViewers(sampleCtx, client, cfg, tokens, rec)
	cancelSample()
	finished := time.Now()

	samples := rec.snapshot()
	res := &Result{
		Commit:         CommitSHA(),
		Scenario:       cfg.Scenario,
		FixtureHash:    cfg.FixtureHash,
		BaseURL:        cfg.BaseURL,
		ContentID:      cfg.ContentID,
		Quality:        cfg.Quality,
		Viewers:        cfg.Viewers,
		IntervalMS:     cfg.Interval.Milliseconds(),
		WarmupSec:      cfg.Warmup.Seconds(),
		DurationSec:    cfg.Duration.Seconds(),
		HTTPStatuses:   map[string]int64{},
		StartedAt:      started,
		FinishedAt:     finished,
		LoginPaced:     loginPaced,
		WalletPoolSize: len(tokens),
		LoginsFailed:   loginsFailed,
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

// RunPhase resolves the viewer wallet pool into per-viewer JWTs. Every wallet
// carrying a PrivateKey performs a REAL challenge -> sign -> login flow, PACED
// by LoginPace (default 7s, floor 1s) so the auth limiter — 10 req/min, 2
// requests per login — is never hit. When SetupMint is set, each logged-in
// wallet additionally mints a DemoNFT to itself. A failed login aborts the
// run loudly, exactly as the plan requires of the setup phase. Returns the
// tokens (pool order, one per identity), whether logins were paced, the
// number of failed logins, and an error if any login failed.
func RunPhase(ctx context.Context, cfg Config) ([]string, bool, int, error) {
	pool := cfg.resolveWalletPool()
	pace := cfg.LoginPace
	if pace == 0 {
		pace = defaultLoginPace
	}
	if pace > 0 && pace < minLoginPace {
		pace = minLoginPace
	}

	tokens := make([]string, len(pool))
	var failures []string
	var lastLoginEnd time.Time
	for i, w := range pool {
		if err := ctx.Err(); err != nil {
			return nil, pace > 0, len(failures), fmt.Errorf("login phase canceled: %w", err)
		}
		if w.JWTToken != "" {
			tokens[i] = w.JWTToken
			continue
		}
		// Pace real logins so the auth limiter (10/min) never sees a burst:
		// each login is 2 requests, so ~7s keeps any window under 9 logins.
		if pace > 0 && !lastLoginEnd.IsZero() {
			if wait := pace - time.Since(lastLoginEnd); wait > 0 {
				select {
				case <-ctx.Done():
					return nil, true, len(failures), fmt.Errorf("login phase canceled: %w", ctx.Err())
				case <-time.After(wait):
				}
			}
		}
		chainID := w.ChainID
		if chainID == 0 {
			chainID = cfg.ChainID
		}
		token, lerr := WalletLogin(ctx, cfg.BaseURL, w.Wallet, w.PrivateKey, chainID)
		lastLoginEnd = time.Now()
		if lerr != nil {
			failures = append(failures, fmt.Sprintf("wallet %d (%s): %v", i, w.Wallet, lerr))
			continue
		}
		tokens[i] = token
		if cfg.SetupMint {
			if merr := devMint(ctx, cfg.BaseURL, token, w.Wallet); merr != nil {
				failures = append(failures, fmt.Sprintf("wallet %d (%s): dev mint: %v", i, w.Wallet, merr))
				continue
			}
		}
	}
	if len(failures) > 0 {
		return nil, pace > 0, len(failures), fmt.Errorf("wallet setup failed (%d of %d): %s",
			len(failures), len(pool), strings.Join(failures, "; "))
	}
	return tokens, pace > 0, 0, nil
}

// devMint asks the dev-only server-side signer to mint DemoNFTs to the
// AUTHENTICATED wallet. Only reachable on the local fullchain stack (demo
// profile + chain 31337 + the explicit opt-in flag).
func devMint(ctx context.Context, baseURL, token, wallet string) error {
	body, _ := json.Marshal(map[string]any{"count": 1})
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	if wallet != "" {
		h.Set("X-Wallet-Address", wallet)
	}
	var out struct {
		TotalMinted int64 `json:"total_minted"`
	}
	if err := postJSON(ctx, baseURL+DevMintEndpoint, body, &out, h); err != nil {
		return err
	}
	return nil
}

func runViewers(ctx context.Context, client *http.Client, cfg Config, tokens []string, rec *recorder) {
	var wg sync.WaitGroup
	for i := 0; i < cfg.Viewers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Viewer i drives wallet i's token. The pool size should equal
			// Viewers; when it is smaller, wallets are reused round-robin
			// (recorded as wallet_pool_size in the result JSON).
			viewerLoop(ctx, client, cfg, tokens[i%len(tokens)], rec)
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

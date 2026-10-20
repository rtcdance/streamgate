//go:build load

// Live load entry point against a RUNNING fullchain stack (make
// fullchain-deploy). It is excluded from the default `go test ./...` run via
// the `load` build tag, exactly like the repo's `anvil` / `testnet` tags.
//
// Environment (see scripts/perf-acceptance.sh which sets all of these):
//
//	PERF_BASE_URL      running stack root            (default http://localhost:18080)
//	PERF_SCENARIO      scenario label                (default warm_cache; the script drives
//	                     cold_cache / rpc_primary_down / rpc_all_down via fault injection)
//	PERF_JWT           unsupported for live acceptance; use real wallet login
//	PERF_WALLET        wallet address for real login (optional; derived from key)
//	PERF_PRIVATE_KEY   hex secp256key of a REAL logged-in, NFT-holding wallet
//	PERF_WALLET_KEYS_FILE  file of hex private keys (one per line or space-separated);
//	                     when set, each viewer uses its OWN real-login wallet
//	                     (viewer i = wallet i) instead of sharing one wallet
//	PERF_CHAIN_ID      chain id for the login challenge (default 31337)
//	PERF_CONTENT_ID    gated content to play         (required)
//	PERF_QUALITY       variant quality               (fixed rendition; default 360p)
//	PERF_VIEWERS       virtual viewers               (default 1)
//	PERF_INTERVAL      per-viewer request pace       (default 2s)
//	PERF_WARMUP        warmup window                 (default 10s)
//	PERF_DURATION      timed sampling window         (default 30s)
//	PERF_LOGIN_PACE    gap between real logins       (default 7s; auth limiter is 10/min)
//	PERF_FIXTURE_HASH  sha256 of the media fixture   (optional; recorded)
//	PERF_RESULT_JSON   output path for the JSON result (default /tmp/perf-result.json)
//	PERF_MEDIA_HASHES_FILE JSON map of media URL paths to independent fixture SHA256 hashes.
//	PERF_SEGMENT_SHA256  single-segment alternative: hex digest — when set, every segment body is
//	                    sha256-checked against it (mismatch = error, not success)
//
// Exit: healthy and fallback scenarios enforce the declared success threshold;
// total RPC failure must deny every request. Missing viewer samples fail even
// when aggregate totals appear healthy. Failed sampling results are still saved.
package load_test

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rtcdance/streamgate/test/load"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(t *testing.T, key string, def int) int {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	require.NoError(t, err, "%s must be an integer", key)
	return n
}

// viewerPoolFromEnv resolves the viewer wallet pool from PERF_WALLET_KEYS_FILE
// (one hex key per line or space-separated) when set; empty when falling back
// to the single-wallet path.
func viewerPoolFromEnv(t *testing.T) []load.WalletIdentity {
	t.Helper()
	path := os.Getenv("PERF_WALLET_KEYS_FILE")
	if path == "" {
		return nil
	}
	pool, err := load.WalletsFromKeysFile(path)
	require.NoError(t, err, "PERF_WALLET_KEYS_FILE=%s must contain hex secp256k1 keys", path)
	return pool
}

func loginPaceFromEnv(t *testing.T) time.Duration {
	t.Helper()
	if v := os.Getenv("PERF_LOGIN_PACE"); v != "" {
		d, err := time.ParseDuration(v)
		require.NoError(t, err, "PERF_LOGIN_PACE must be a duration like 7s")
		return d
	}
	return 0 // harness default (7s)
}

func TestLoad_AuthorizedMedia(t *testing.T) {
	baseURL := envOr("PERF_BASE_URL", "http://localhost:18080")
	scenario := envOr("PERF_SCENARIO", "warm_cache")
	contentID := os.Getenv("PERF_CONTENT_ID")
	if contentID == "" {
		t.Skip("PERF_CONTENT_ID is not set — the live scenario is driven by scripts/perf-acceptance.sh")
	}

	walletPool := viewerPoolFromEnv(t)
	if len(walletPool) == 0 && os.Getenv("PERF_JWT") == "" && os.Getenv("PERF_PRIVATE_KEY") == "" {
		t.Skip("no wallet credentials: set PERF_WALLET_KEYS_FILE or PERF_PRIVATE_KEY for one viewer")
	}

	viewers := envInt(t, "PERF_VIEWERS", 1)
	require.Positive(t, viewers)
	if len(walletPool) == 0 && os.Getenv("PERF_PRIVATE_KEY") != "" {
		walletPool = []load.WalletIdentity{{PrivateKey: os.Getenv("PERF_PRIVATE_KEY"), Wallet: os.Getenv("PERF_WALLET")}}
	}
	require.GreaterOrEqual(t, len(walletPool), viewers, "live acceptance requires one private-key wallet per viewer")
	for i, w := range walletPool[:viewers] {
		require.NotEmpty(t, w.PrivateKey, "viewer %d must use real wallet login", i)
	}

	interval := 2 * time.Second
	if v := os.Getenv("PERF_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		require.NoError(t, err)
		interval = d
	}
	warmup := 10 * time.Second
	if v := os.Getenv("PERF_WARMUP"); v != "" {
		d, err := time.ParseDuration(v)
		require.NoError(t, err)
		warmup = d
	}
	duration := 30 * time.Second
	if v := os.Getenv("PERF_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		require.NoError(t, err)
		duration = d
	}
	chainID := int64(envInt(t, "PERF_CHAIN_ID", 31337))

	cfg := load.Config{
		HarnessRevision: os.Getenv("PERF_HARNESS_SHA"),
		BaseURL:         baseURL,
		Scenario:        scenario,
		JWTToken:        os.Getenv("PERF_JWT"),
		Wallet:          os.Getenv("PERF_WALLET"),
		PrivateKey:      os.Getenv("PERF_PRIVATE_KEY"),
		ChainID:         chainID,
		Wallets:         walletPool,
		LoginPace:       loginPaceFromEnv(t),
		ContentID:       contentID,
		Quality:         envOr("PERF_QUALITY", "360p"),
		PaceByEXTINF:    true,
		Viewers:         viewers,
		Interval:        interval,
		Warmup:          warmup,
		Duration:        duration,
		FixtureHash:     os.Getenv("PERF_FIXTURE_HASH"),
	}
	if path := os.Getenv("PERF_MEDIA_HASHES_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &cfg.MediaSHA256), "fixture media hashes must be a path-to-SHA256 JSON object")
	} else if digest := os.Getenv("PERF_SEGMENT_SHA256"); digest != "" {
		cfg.MediaSHA256 = map[string]string{"*": digest}
	}
	require.NotEmpty(t, cfg.MediaSHA256, "set PERF_MEDIA_HASHES_FILE from independent fixture artifacts, or PERF_SEGMENT_SHA256 for a single repeated segment")

	require.Len(t, cfg.HarnessRevision, 40, "harness source SHA is required separately from target revision")
	phaseDir := os.Getenv("PERF_PHASE_DIR")
	require.NotEmpty(t, phaseDir, "controller sampling barrier is required for live acceptance")
	cfg.BeforeSampling = func(ctx context.Context) error { return load.WaitForSamplingRelease(ctx, phaseDir, 180*time.Second) }
	res, runErr := load.Run(context.Background(), cfg)

	out := envOr("PERF_RESULT_JSON", "/tmp/perf-result.json")
	if res != nil {
		require.NoError(t, res.WriteJSON(out))
	}
	require.NoError(t, runErr)
	t.Logf("scenario=%s commit=%s viewers=%d pool=%d login_paced=%v success=%d errors=%d bytes=%d ttfb_p95=%.2fms latency_p95=%.2fms result=%s",
		res.Scenario, res.Commit, res.Viewers, res.WalletPoolSize, res.LoginPaced,
		res.SuccessCount, res.ErrorCount, res.TotalBytes,
		res.TTFBMillis.P95, res.LatencyMillis.P95, out)

	// Scenario bars, keyed by the label the caller injected the fault under.
	// The harness records every scenario but only enforces the ones whose
	// expected outcome it can name on its own; `load.ScenarioViolation` holds
	// those bars in one place so they can be exercised without a live stack.
	if load.CapacityTierScenario(scenario) && res.Viewers > load.CapacityTierViewers {
		// The two tiers make different claims. The ≥99% bar is defined at 50
		// viewers; above it the same scenario IS the capacity exploration, whose
		// acceptance is "throttles or overloads observably, and never turns a
		// holder away" (TRANSITION_PLAN.md §5 SG-07 验收). Judging that tier by
		// the 50-viewer bar would replace the specified acceptance with a
		// stricter one and make the ceiling unreportable.
		t.Logf("capacity tier at %d viewers: success=%d errors=%d bytes=%d statuses=%v — judged as an exploration, not by the M4 bar",
			res.Viewers, res.SuccessCount, res.ErrorCount, res.TotalBytes, res.HTTPStatuses)
	}
	require.NoError(t, load.ScenarioViolation(res))
}

// TestPerfSetup_MintViewerWallets is the one-time SETUP phase driven by
// scripts/perf-acceptance.sh (PERF_SETUP=1): generate N deterministic wallets,
// log in as each (paced so the auth limiter is never hit) and dev-mint a
// DemoNFT to each so every virtual viewer has its OWN NFT-holding wallet. The
// keys are written to PERF_WALLET_KEYS_FILE (0600) for the scenario runs to
// consume. Skips when PERF_SETUP != 1 so a bare `-tags load` run stays clean.
func TestPerfSetup_MintViewerWallets(t *testing.T) {
	if os.Getenv("PERF_SETUP") != "1" {
		t.Skip("PERF_SETUP != 1 — SETUP phase is only run on demand")
	}
	keysPath := os.Getenv("PERF_WALLET_KEYS_FILE")
	require.NotEmpty(t, keysPath, "PERF_WALLET_KEYS_FILE is required (it is where the generated keys are written)")
	baseURL := envOr("PERF_BASE_URL", "http://localhost:18080")
	count := envInt(t, "PERF_WALLETS", 5)
	chainID := int64(envInt(t, "PERF_CHAIN_ID", 31337))

	// Deterministic keys: re-running SETUP over an existing file reproduces the
	// same wallet addresses, so an already-minted NFT is reused, not reminted.
	require.NoError(t, load.PerfGenerateKeys(count, keysPath),
		"generate %d deterministic wallet keys at %s", count, keysPath)
	require.NoError(t, os.Chmod(keysPath, 0o600))
	t.Logf("generated %d wallet keys at %s", count, keysPath)

	pool, err := load.WalletsFromKeysFile(keysPath)
	require.NoError(t, err)
	for i := range pool {
		pool[i].ChainID = chainID
	}

	cfg := load.Config{
		BaseURL:   baseURL,
		Scenario:  "setup",
		ContentID: "setup",
		ChainID:   chainID,
		Wallets:   pool,
		SetupMint: true,
		LoginPace: loginPaceFromEnv(t),
		// Setup-only knobs: viewers/interval/duration never reach the load
		// phase because RunPhase errors out or minting fails loudly first, but
		// validate() demands nonzero placeholders.
		Viewers:  1,
		Interval: time.Second,
		Duration: time.Second,
	}
	tokens, paced, failed, err := load.RunPhase(context.Background(), cfg)
	require.NoError(t, err, "every wallet must log in and mint successfully — a failed setup fails the round")
	require.Equal(t, count, len(tokens))
	require.Zero(t, failed)
	require.True(t, paced, "logins must be paced")
	t.Logf("setup complete: %d wallets logged in (paced=%v) and hold a DemoNFT", count, paced)
}

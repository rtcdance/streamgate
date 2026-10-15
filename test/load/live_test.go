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
//	PERF_JWT           pre-minted JWT (optional; omit to use real login)
//	PERF_WALLET        wallet address for real login (optional; derived from key)
//	PERF_PRIVATE_KEY   hex secp256key of a REAL logged-in, NFT-holding wallet
//	PERF_WALLET_KEYS_FILE  file of hex private keys (one per line or space-separated);
//	                     when set, each viewer uses its OWN real-login wallet
//	                     (viewer i = wallet i) instead of sharing one wallet
//	PERF_CHAIN_ID      chain id for the login challenge (default 31337)
//	PERF_CONTENT_ID    gated content to play         (required)
//	PERF_QUALITY       variant quality               (optional; empty = master manifest)
//	PERF_VIEWERS       virtual viewers               (default 1)
//	PERF_INTERVAL      per-viewer request pace       (default 2s)
//	PERF_WARMUP        warmup window                 (default 10s)
//	PERF_DURATION      timed sampling window         (default 30s)
//	PERF_LOGIN_PACE    gap between real logins       (default 7s; auth limiter is 10/min)
//	PERF_FIXTURE_HASH  sha256 of the media fixture   (optional; recorded)
//	PERF_RESULT_JSON   output path for the JSON result (default /tmp/perf-result.json)
//	PERF_SEGMENT_SHA256  optional hex digest — when set, every segment body is
//	                    sha256-checked against it (mismatch = error, not success)
//
// Exit: TestLoad fails the run on a nonzero error count for scenarios that must
// stay healthy; fault scenarios (rpc_primary_down / rpc_all_down) record but
// do not fail on elevated errors — the safety assertion (no unauthorized 2xx
// when the gate must deny) belongs to the fault-injection scripts, not here.
package load_test

import (
	"context"
	"os"
	"strconv"
	"strings"
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
		t.Skip("no wallet credentials: set PERF_WALLET_KEYS_FILE (per-viewer pool), PERF_PRIVATE_KEY (single wallet) or PERF_JWT")
	}

	viewers := envInt(t, "PERF_VIEWERS", 1)
	if n := len(walletPool); n > 0 && n < viewers {
		// Reuse is allowed but must be visible: a pool smaller than the viewer
		// count means wallets are shared round-robin, which re-introduces
		// per-wallet media-limiter contention.
		t.Logf("WARNING: wallet pool (%d) < viewers (%d) — wallets reused round-robin", n, viewers)
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
		BaseURL:     baseURL,
		Scenario:    scenario,
		JWTToken:    os.Getenv("PERF_JWT"),
		Wallet:      os.Getenv("PERF_WALLET"),
		PrivateKey:  os.Getenv("PERF_PRIVATE_KEY"),
		ChainID:     chainID,
		Wallets:     walletPool,
		LoginPace:   loginPaceFromEnv(t),
		ContentID:   contentID,
		Quality:     os.Getenv("PERF_QUALITY"),
		Viewers:     viewers,
		Interval:    interval,
		Warmup:      warmup,
		Duration:    duration,
		FixtureHash: os.Getenv("PERF_FIXTURE_HASH"),
	}
	if sha := os.Getenv("PERF_SEGMENT_SHA256"); sha != "" {
		expect := load.ExpectedSHA256(sha)
		cfg.Expect = func(u string, body []byte) error {
			// The digest describes media segments, not the playlist text.
			if isManifest(u) {
				return nil
			}
			return expect(u, body)
		}
	}

	res, err := load.Run(context.Background(), cfg)
	require.NoError(t, err)

	out := envOr("PERF_RESULT_JSON", "/tmp/perf-result.json")
	require.NoError(t, res.WriteJSON(out))
	t.Logf("scenario=%s commit=%s viewers=%d pool=%d login_paced=%v success=%d errors=%d bytes=%d ttfb_p95=%.2fms latency_p95=%.2fms result=%s",
		res.Scenario, res.Commit, res.Viewers, res.WalletPoolSize, res.LoginPaced,
		res.SuccessCount, res.ErrorCount, res.TotalBytes,
		res.TTFBMillis.P95, res.LatencyMillis.P95, out)

	// Scenario bars, keyed by the label the caller injected the fault under.
	// The harness records every scenario but only enforces the ones whose
	// expected outcome it can name on its own.
	switch scenario {
	case "cold_cache", "warm_cache":
		// Healthy scenarios carry two different claims. M4's bar — ≥99% success
		// — is defined at 50 viewers; above that the same scenario IS the
		// capacity exploration, whose acceptance is not "stays at 99%" but
		// "throttles or overloads observably, and never turns a holder away"
		// (TRANSITION_PLAN.md §5 SG-07 验收). Judging this tier by the M4 bar
		// would replace the specified acceptance with a stricter one and make
		// the ceiling unreportable, so it is judged by its own rule.
		if res.Viewers > load.CapacityTierViewers {
			require.NoError(t, load.CapacityTierViolation(res, load.CapacityTierViewers))
			t.Logf("capacity tier at %d viewers: success=%d errors=%d bytes=%d statuses=%v — judged as an exploration, not by the M4 bar",
				res.Viewers, res.SuccessCount, res.ErrorCount, res.TotalBytes, res.HTTPStatuses)
			break
		}
		total := res.SuccessCount + res.ErrorCount
		require.Greater(t, total, int64(0), "no requests were executed")
		successRate := float64(res.SuccessCount) / float64(total)
		require.GreaterOrEqual(t, successRate, 0.99,
			"healthy scenario %s must keep ≥99%% success (got %.2f%%)", scenario, successRate*100)
	case "rpc_primary_down":
		// The primary endpoint was stopped behind the caller's back; the
		// same-chain fallback must carry the identical load. Same ≥99% bar as
		// the healthy scenarios — a fallback that degrades throughput is not a
		// working fallback.
		total := res.SuccessCount + res.ErrorCount
		require.Greater(t, total, int64(0), "no requests were executed")
		successRate := float64(res.SuccessCount) / float64(total)
		require.GreaterOrEqual(t, successRate, 0.99,
			"same-chain fallback must keep ≥99%% success (got %.2f%%)", successRate*100)
	case "rpc_all_down":
		// Every endpoint is unreachable, so the only acceptable outcome is a
		// total denial: not one request may be served media bytes and not one
		// byte of media may leave the origin. A single success here is an
		// authorization bypass, so it fails the run outright.
		require.Greater(t, res.SuccessCount+res.ErrorCount, int64(0),
			"no requests were executed — the scenario proves nothing")
		require.Zero(t, res.SuccessCount,
			"a request was SERVED while every RPC endpoint was down (authorization bypass)")
		require.Zero(t, res.TotalBytes,
			"media bytes were delivered while every RPC endpoint was down")
	}
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
		// Setup-only knobs: viewers/interval/duration never reach the load
		// phase because RunPhase errors out or minting fails loudly first, but
		// validate() demands nonzero placeholders.
		Viewers:  1,
		Interval: time.Second,
		Duration: time.Second,
	}
	tokens, paced, failed, err := load.RunPhase(context.Background(), cfg)
	require.NoError(t, err, "every wallet must log in and mint successfully — a failed setup fails the round")
	require.Len(t, tokens, count)
	require.Zero(t, failed)
	require.True(t, paced, "logins must be paced")
	t.Logf("setup complete: %d wallets logged in (paced=%v) and hold a DemoNFT", count, paced)
}

func isManifest(u string) bool {
	return strings.Contains(u, "manifest.m3u8")
}

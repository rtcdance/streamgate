package middleware

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// countingVerifier counts how often the chain was consulted, via the mock that
// already satisfies the whole interface.
func countingVerifier(calls *atomic.Int64, amount *big.Int) *mockNFTOwnershipCheckerOld {
	return &mockNFTOwnershipCheckerOld{
		quantityFn: func(context.Context, int64, string, string, string) (*big.Int, error) {
			calls.Add(1)
			return new(big.Int).Set(amount), nil
		},
	}
}

// playlistGateRouter wires [markPlaybackAuth, gate] onto both streaming routes,
// so FullPath() is populated the way it is in production.
func playlistGateRouter(t *testing.T, verifier NFTOwnershipChecker) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/")
	// Stand in for PlaybackTokenAuth: mark the request as capability-authenticated
	// and supply the wallet, exactly as a validated query token would.
	g.Use(func(c *gin.Context) {
		c.Set("playback_auth", true)
		c.Set("wallet_address", "0xholder")
		c.Next()
	})
	g.Use(NFTGateMiddleware(&NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: testContractAddr, ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		BlockProver:    &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xplaylist"}},
		DefaultChainID: 1,
	}, zap.NewNop()))
	g.GET("/api/v1/streaming/:id/manifest.m3u8", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	g.GET("/api/v1/streaming/:id/segment/:num", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

// A PLAYLIST is the renewal point: even with a valid capability it must re-check
// the CURRENT holding, or a capability alone would keep re-authorizing playback.
func TestNFTGateMiddleware_PlaylistWithCapabilityStillChecksTheChain(t *testing.T) {
	var calls atomic.Int64
	r := playlistGateRouter(t, countingVerifier(&calls, big.NewInt(5)))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/c1/manifest.m3u8?playback_token=cap", http.NoBody)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int64(1), calls.Load(),
		"a playlist request must re-check the holding even when it carries a capability")
}

// A SEGMENT is authorized by the capability alone. Re-reading the chain per
// segment would be both slow and unnecessary.
func TestNFTGateMiddleware_SegmentWithCapabilitySkipsTheChain(t *testing.T) {
	var calls atomic.Int64
	r := playlistGateRouter(t, countingVerifier(&calls, big.NewInt(5)))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/c1/segment/0.ts?playback_token=cap", http.NoBody)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int64(0), calls.Load(),
		"a segment must be served on the capability alone, with no chain query")
}

// The playlist re-check is a real decision, not a formality: a holder who no
// longer holds is refused even with a valid capability.
func TestNFTGateMiddleware_PlaylistWithCapabilityRefusesANonHolder(t *testing.T) {
	var calls atomic.Int64
	r := playlistGateRouter(t, countingVerifier(&calls, big.NewInt(0)))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/streaming/c1/manifest.m3u8?playback_token=cap", http.NoBody)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code,
		"a capability must not keep a non-holder playing")
}

// The route pattern decides, not the URL text: a segment path that merely
// mentions a manifest name must not be treated as a playlist.
func TestIsPlaylistRequest_MatchesTheRoutePattern(t *testing.T) {
	gin.SetMode(gin.TestMode)

	check := func(route, url string) bool {
		r := gin.New()
		var got bool
		r.GET(route, func(c *gin.Context) {
			got = isPlaylistRequest(c)
			c.String(http.StatusOK, "ok")
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, url, http.NoBody)
		r.ServeHTTP(w, req)
		return got
	}

	assert.True(t, check("/api/v1/streaming/:id/manifest.m3u8", "/api/v1/streaming/c1/manifest.m3u8"))
	assert.False(t, check("/api/v1/streaming/:id/segment/:num", "/api/v1/streaming/c1/segment/0.ts"))
	// A segment whose NAME contains "manifest.m3u8" is still a segment: the
	// pattern is what counts, so a crafted name cannot buy a chain-free read.
	assert.False(t, check("/api/v1/streaming/:id/segment/:num", "/api/v1/streaming/c1/segment/manifest.m3u8"))
}

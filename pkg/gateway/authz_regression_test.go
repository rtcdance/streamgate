package gateway

// authz_regression_test.go — TDD RED step for the streaming authorization bypass.
//
// WHAT THESE TESTS ARE
//   They encode the REQUIRED behavior of the streaming authorization path and are
//   expected to FAIL against the current code. Do NOT edit the assertions to make
//   them pass — fix the production code (TRANSITION_PLAN.md tasks A2..A9).
//
// VERIFIED THREAT (every step read from source):
//   1. middleware/auth.go:51-55   any non-empty `playback_token` sets playback_auth
//      and skips JWT entirely, WITHOUT validating the token.
//   2. middleware/nft_gate.go:195-200  seeing playback_auth, sets nft_verified=true
//      and skips every on-chain ownership check.
//   3. gateway/routes.go:52 vs :54  the segment route is registered BEFORE
//      router.Use(JWTAuthMiddleware), so it has neither JWT nor NFT gate.
//   4. streaming_handlers.go:190  `token != "" && quality != ""` — omitting
//      `quality` skips the only validation branch.
//   5. streaming_handlers.go:228,250  an empty wallet still gets a signed token.
//
// RED vs GUARD
//   RED   = must FAIL now (these are the ones that prove the bug exists)
//   GUARD = already pass; must KEEP passing (they pin behavior that must not regress)
//
// If a RED test passes on unmodified code, the test is wrong — not the code.

import (
	"context"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// NewAuthService panics if the secret is shorter than 32 chars (pkg/service/auth.go:208-210).
const authzTestSecret = "test-secret-that-is-at-least-32-chars"

const (
	authzContentID = "demo-content-1"
	authzWallet    = "0xabc"
	contractA      = "0x1111111111111111111111111111111111111111" // server-side rule
	contractB      = "0x2222222222222222222222222222222222222222" // caller-supplied
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeSegmentStorage satisfies service.SegmentStorage (aliased at
// pkg/service/transcoding_shim.go:13 -> transcoding.SegmentStorage).
// ListObjects returns empty so the real handlers report "content not ready" (404)
// instead of panicking on a nil dependency.
type fakeSegmentStorage struct{}

func (fakeSegmentStorage) Upload(context.Context, string, string, []byte) error { return nil }
func (fakeSegmentStorage) UploadStream(context.Context, string, string, io.Reader, int64) error {
	return nil
}
func (fakeSegmentStorage) UploadWithContentType(context.Context, string, string, []byte, string) error {
	return nil
}
func (fakeSegmentStorage) UploadStreamWithContentType(context.Context, string, string, io.Reader, int64, string) error {
	return nil
}
func (fakeSegmentStorage) Download(context.Context, string, string) ([]byte, error) {
	return nil, nil
}
func (fakeSegmentStorage) DownloadStream(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (fakeSegmentStorage) Delete(context.Context, string, string) error { return nil }
func (fakeSegmentStorage) ListObjects(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (fakeSegmentStorage) Exists(context.Context, string, string) (bool, error) { return false, nil }

// fakeRuleResolver implements middleware.GatingRuleResolver (nft_gate.go:89).
type fakeRuleResolver struct{ rules []middleware.GatingRule }

func (f fakeRuleResolver) GetActiveRulesForContent(context.Context, string) ([]middleware.GatingRule, error) {
	return f.rules, nil
}

// fakeOwnership implements middleware.NFTOwnershipChecker (nft_gate.go:64).
// Ownership is keyed by contract address so we can distinguish "the server's
// contract" from "a contract the caller substituted".
type fakeOwnership struct{ owned map[string]bool }

func (f fakeOwnership) isOwned(contract string) bool { return f.owned[strings.ToLower(contract)] }

func (f fakeOwnership) VerifyNFTOwnership(_ context.Context, _ int64, contract, _, _ string) (bool, error) {
	return f.isOwned(contract), nil
}

func (f fakeOwnership) VerifyNFTOwnershipAutoDetect(ctx context.Context, chainID int64, contract, tokenID, owner string) (bool, error) {
	return f.VerifyNFTOwnership(ctx, chainID, contract, tokenID, owner)
}

func (f fakeOwnership) VerifyNFTCollectionAutoDetect(_ context.Context, _ int64, contract, _ string) (bool, error) {
	return f.isOwned(contract), nil
}

func (f fakeOwnership) GetNFTBalance(_ context.Context, _ int64, contract, _ string) (*big.Int, error) {
	if f.isOwned(contract) {
		return big.NewInt(1), nil
	}
	return big.NewInt(0), nil
}

// GetNFTOwnershipQuantity mirrors the ownership map as a 0/1 quantity.
func (f fakeOwnership) GetNFTOwnershipQuantity(_ context.Context, _ int64, contract, _, _ string, block *big.Int) (*big.Int, error) {
	if f.isOwned(contract) {
		return big.NewInt(1), nil
	}
	return big.NewInt(0), nil
}

func (f fakeOwnership) GetNFTInfo(context.Context, int64, string, string) (*middleware.NFTMetadata, error) {
	return &middleware.NFTMetadata{}, nil
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// authzBlockProver supplies the block anchor the gate requires. Both lookups
// return the same header so the unchanged-block check passes.
type authzBlockProver struct{}

func (authzBlockProver) HeaderByNumber(_ context.Context, _ int64, _ *big.Int) (*middleware.BlockHeaderInfo, error) {
	return &middleware.BlockHeaderInfo{Number: 100, Hash: "0xauthz"}, nil
}

func (authzBlockProver) HeaderByTag(_ context.Context, _ int64, _ string) (*middleware.BlockHeaderInfo, error) {
	return &middleware.BlockHeaderInfo{Number: 100, Hash: "0xauthz"}, nil
}

func mustValidJWT(t *testing.T, wallet string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"wallet_address": wallet,
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Add(-time.Minute).Unix(),
	})
	signed, err := tok.SignedString([]byte(authzTestSecret))
	assert.NoError(t, err)
	return signed
}

// newAuthzTestRouter reproduces the PRODUCTION middleware order (routes.go):
// the streaming group is created BEFORE the global JWT Use and carries
// PlaybackTokenAuth first; JWT + the NFT gate are appended to the group
// afterwards, giving [PlaybackTokenAuth, JWT, NFTGate] for manifest AND
// segment routes. Getting this order wrong makes the tests meaningless.
func newAuthzTestRouter(t *testing.T, rules []middleware.GatingRule, owned map[string]bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := zap.NewNop()

	authSvc := service.NewAuthService(authzTestSecret, nil)
	cache := NewStreamingCache()
	lim := newStreamLimiter(100)
	store := fakeSegmentStorage{}
	r := gin.New()

	g := r.Group("/")
	g.Use(middleware.PlaybackTokenAuth(middleware.PlaybackTokenAuthConfig{
		Validate: func(ctx context.Context, token, contentID, fingerprint string) (string, error) {
			claims, err := authSvc.ParsePlaybackToken(ctx, token, contentID, fingerprint)
			if err != nil {
				return "", err
			}
			return claims.WalletAddress, nil
		},
	}, log))

	r.Use(middleware.JWTAuthMiddleware(middleware.JWTAuthConfig{Secret: authzTestSecret}, log))
	g.Use(middleware.JWTAuthMiddleware(middleware.JWTAuthConfig{Secret: authzTestSecret}, log))

	gateCfg := middleware.NFTGateConfig{
		Verifier:     fakeOwnership{owned: owned},
		RuleResolver: fakeRuleResolver{rules: rules},
		// The gate pins every authorization to one block, so it needs a prover.
		// Without one it fails closed (503) rather than reading at latest.
		BlockProver: authzBlockProver{},
		// Cache is NOT defaulted inside NFTGateMiddleware — must be set explicitly
		// or the gate panics.
		// NOTE: it must go through NFTAccessCacheAdapter. The concrete
		// *NFTAccessCache has NON-context signatures (Get(key), Delete(key)) and
		// therefore does NOT satisfy middleware.NFTAccessCache, which requires
		// Get(ctx, key) / Delete(ctx, key). See pkg/gateway/nft_handlers.go:310-348.
		Cache: &NFTAccessCacheAdapter{Cache: NewNFTAccessCache()},
	}
	g.Use(middleware.NFTGateMiddleware(&gateCfg, log))
	RegisterStreamingSegmentRoute(g, log, authSvc, store, lim, cache, "streamgate")
	RegisterStreamingRoutes(g, log, authSvc, nil, store, lim, cache, "streamgate")

	return r
}

func doGet(r *gin.Engine, target, bearer string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	r.ServeHTTP(w, req)
	return w
}

func manifestURL() string {
	return APIPrefix + "/streaming/" + authzContentID + "/manifest.m3u8"
}

func segmentURL() string {
	return APIPrefix + "/streaming/" + authzContentID + "/segment/0.ts"
}

// ===========================================================================
// Permanent regression (A10). These were RED tests gated behind
// requireRED()/AUTHZ_RED=1 while the fix was pending — tasks A2..A9 have
// landed, so they now run unconditionally in CI and permanently guard the
// streaming authorization path.
// ===========================================================================

// A forged playback_token must not be accepted as a credential.
// Pre-fix: JWT was skipped (auth.go:51), the gate bypassed (nft_gate.go:195),
// and the request reached the handler (404) — never 401.
func TestManifest_BogusPlaybackToken_Returns401(t *testing.T) {
	r := newAuthzTestRouter(t, nil, nil)
	w := doGet(r, manifestURL()+"?playback_token=x", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"forged playback_token must be rejected; got %d body=%s", w.Code, w.Body.String())
}

// The `quality` param must not gate whether validation happens
// (streaming_handlers.go:190). Omitting it must not open the door.
func TestManifest_BogusPlaybackToken_NoQuality_Returns401(t *testing.T) {
	r := newAuthzTestRouter(t, nil, nil)
	w := doGet(r, manifestURL()+"?playback_token=x", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"forged playback_token without quality must be rejected; got %d body=%s", w.Code, w.Body.String())
}

// With a valid JWT but NO gating rules, the gate must fail CLOSED.
// Pre-fix: nft_gate.go failed open ("no_gating_rules").
func TestManifest_NoGatingRules_DeniesByDefault(t *testing.T) {
	r := newAuthzTestRouter(t, nil /* no rules */, nil)
	w := doGet(r, manifestURL(), mustValidJWT(t, authzWallet))
	assert.Equal(t, http.StatusForbidden, w.Code,
		"no gating rules must DENY (403), not fail open; got %d body=%s", w.Code, w.Body.String())
}

// The contract to verify against must come from server-side rules, never from
// the caller (nft_gate.go:267 reads it from the query).
// Here the server's rule is contractA, the caller owns an NFT in contractB.
// Pre-fix: the caller's contractB won -> access granted.
func TestManifest_CallerSuppliedContract_Ignored(t *testing.T) {
	rules := []middleware.GatingRule{{
		ContractAddress: contractA,
		TokenID:         "1",
		ChainID:         1,
		Standard:        "ERC721",
		MinBalance:      1,
	}}
	// Caller owns contractB but NOT contractA.
	owned := map[string]bool{strings.ToLower(contractB): true}

	r := newAuthzTestRouter(t, rules, owned)
	w := doGet(r, manifestURL()+"?contract="+contractB+"&token_id=1&chain_id=1",
		mustValidJWT(t, authzWallet))

	assert.Equal(t, http.StatusForbidden, w.Code,
		"caller-supplied contract must be ignored; server rule is %s; got %d body=%s",
		contractA, w.Code, w.Body.String())
}

// ===========================================================================
// GUARD — already pass today; must keep passing.
// ===========================================================================

func TestManifest_NoAuth_Returns401(t *testing.T) {
	r := newAuthzTestRouter(t, nil, nil)
	w := doGet(r, manifestURL(), "")
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"unauthenticated manifest request must be 401; got %d", w.Code)
}

func TestSegment_NoToken_Returns401(t *testing.T) {
	r := newAuthzTestRouter(t, nil, nil)
	w := doGet(r, segmentURL(), "")
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"unauthenticated segment request must be 401; got %d", w.Code)
}

func TestSegment_BogusPlaybackToken_Returns401(t *testing.T) {
	r := newAuthzTestRouter(t, nil, nil)
	w := doGet(r, segmentURL()+"?playback_token=x", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"forged playback_token must not unlock segments; got %d body=%s", w.Code, w.Body.String())
}

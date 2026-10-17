package gateway

import (
	"context"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const paritySecret = "parity-secret-that-is-at-least-32-chars"

// parityQuantityVerifier answers the REAL quantity the authorizer compares, and
// refuses to answer without a resolved block.
type parityQuantityVerifier struct {
	grpcMockNFTChecker
	quantity *big.Int
}

func (v *parityQuantityVerifier) GetNFTOwnershipQuantity(_ context.Context, _ int64, _, _, _ string, block *big.Int) (*big.Int, error) {
	if block == nil {
		return nil, errors.New("block evidence is required to compare a quantity")
	}
	return new(big.Int).Set(v.quantity), nil
}

// paritySnapshotResolver serves ONE snapshot with a fixed rule version for any
// content and counts how many times the authorizer asked for it.
type paritySnapshotResolver struct {
	rules   []middleware.GatingRule
	version int64
	calls   int
}

func (r *paritySnapshotResolver) ResolvePolicySnapshot(_ context.Context, contentID string) (service.PolicySnapshot, error) {
	r.calls++
	return service.PolicySnapshot{
		ContentID:  contentID,
		Rules:      r.rules,
		Version:    r.version,
		VerifiedAt: time.Now(),
	}, nil
}

func parityRule(minBalance int) middleware.GatingRule {
	return middleware.GatingRule{
		ContractAddress: "0x1111111111111111111111111111111111111111",
		ChainID:         1,
		Standard:        "ERC721",
		MinBalance:      minBalance,
	}
}

// newManifestServerWithGate builds a manifest server whose SHARED authorizer
// evaluates one rule with the given verifier, so the decision reflects the real
// quantity/error the verifier reports.
func newManifestServerWithGate(t *testing.T, verifier middleware.NFTOwnershipChecker, minBalance int) *streamingGrpcServer {
	t.Helper()
	resolver := &paritySnapshotResolver{rules: []middleware.GatingRule{parityRule(minBalance)}, version: 1}
	gate := &middleware.NFTGateConfig{
		Verifier:       verifier,
		BlockProver:    authzBlockProver{},
		DefaultChainID: 1,
	}
	return &streamingGrpcServer{
		authorizer: service.NewPlaybackAuthorizer(gate, resolver, zap.NewNop()),
		log:        zap.NewNop(),
	}
}

func newParityServer(t *testing.T, quantity *big.Int, minBalance int, authSvc *service.AuthService) (*streamingGrpcServer, *paritySnapshotResolver) {
	t.Helper()
	resolver := &paritySnapshotResolver{rules: []middleware.GatingRule{parityRule(minBalance)}, version: 1}
	gate := &middleware.NFTGateConfig{
		Verifier:       &parityQuantityVerifier{quantity: quantity, grpcMockNFTChecker: grpcMockNFTChecker{requiresBlock: true}},
		BlockProver:    authzBlockProver{},
		DefaultChainID: 1,
	}
	store := newGrpcMockSegmentStorage()
	store.list = []string{"streams/c1/seg0.ts"}
	srv := &streamingGrpcServer{
		authSvc:      authSvc,
		streamingSvc: service.NewStreamingService(nil, nil, nil, "http://localhost"),
		segStore:     store,
		authorizer:   service.NewPlaybackAuthorizer(gate, resolver, zap.NewNop()),
		log:          zap.NewNop(),
	}
	return srv, resolver
}

func parityCtx(sessionID string, epoch int64, wallet string) context.Context {
	ctx := context.WithValue(context.Background(), grpcWalletKey, wallet)
	ctx = context.WithValue(ctx, grpcSessionKey, sessionID)
	ctx = context.WithValue(ctx, grpcEpochKey, epoch)
	return ctx
}

// startTestSession starts a live session a test capability can inherit.
func startTestSession(t *testing.T, svc *service.AuthService) string {
	t.Helper()
	id, err := svc.StartSession()
	require.NoError(t, err)
	return id
}

// testRuleContract is the server-side contract the permissive test authorizer
// grants access for.
const testRuleContract = "0x1111111111111111111111111111111111111111"

// staticSnapshotResolver serves one rule set at a fixed version for any content.
type staticSnapshotResolver struct {
	rules   []middleware.GatingRule
	version int64
}

func (r staticSnapshotResolver) ResolvePolicySnapshot(_ context.Context, contentID string) (service.PolicySnapshot, error) {
	return service.PolicySnapshot{
		ContentID:  contentID,
		Rules:      r.rules,
		Version:    r.version,
		VerifiedAt: time.Now(),
	}, nil
}

// testPlaybackAuthorizer is a permissive authorizer for HTTP test harnesses that
// exercise the manifest path without a real chain.
func testPlaybackAuthorizer(t *testing.T) *service.PlaybackAuthorizer {
	t.Helper()
	gate := &middleware.NFTGateConfig{
		Verifier:       fakeOwnership{owned: map[string]bool{testRuleContract: true}},
		BlockProver:    authzBlockProver{},
		DefaultChainID: 1,
	}
	resolver := staticSnapshotResolver{
		rules: []middleware.GatingRule{{
			ContractAddress: testRuleContract,
			ChainID:         1,
			Standard:        "ERC721",
			MinBalance:      1,
		}},
		version: 1,
	}
	return service.NewPlaybackAuthorizer(gate, resolver, zap.NewNop())
}

// The gRPC manifest path must compare the REAL quantity: a wallet holding 1 is
// denied under min_balance 2, while a wallet holding 2 is allowed.
func TestGrpcGetManifest_EnforcesRealQuantity(t *testing.T) {
	authSvc := service.NewAuthService(paritySecret, nil)
	sessionID, err := authSvc.StartSession()
	require.NoError(t, err)

	t.Run("holding one against a minimum of two is denied", func(t *testing.T) {
		srv, _ := newParityServer(t, big.NewInt(1), 2, authSvc)
		resp, err := srv.GetManifest(parityCtx(sessionID, 1, "0xWallet"), &streamingv1.GetManifestRequest{ContentId: "c1"})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("holding two against a minimum of two is allowed", func(t *testing.T) {
		srv, _ := newParityServer(t, big.NewInt(2), 2, authSvc)
		resp, err := srv.GetManifest(parityCtx(sessionID, 1, "0xWallet"), &streamingv1.GetManifestRequest{ContentId: "c1"})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotEmpty(t, resp.Segments)
		assert.Contains(t, resp.Segments[0].Url, "playback_token=")
	})
}

// The capability issued over gRPC must obey the shared 60s policy, not the 2
// minutes the boolean path hardcoded.
func TestGrpcGetManifest_CapabilityTTLIsTheSixtySecondPolicy(t *testing.T) {
	authSvc := service.NewAuthService(paritySecret, nil)
	sessionID, err := authSvc.StartSession()
	require.NoError(t, err)

	srv, _ := newParityServer(t, big.NewInt(5), 1, authSvc)
	resp, err := srv.GetManifest(parityCtx(sessionID, 1, "0xWallet"), &streamingv1.GetManifestRequest{ContentId: "c1"})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Segments)

	raw, err := url.Parse(resp.Segments[0].Url)
	require.NoError(t, err)
	token := raw.Query().Get("playback_token")
	require.NotEmpty(t, token)

	claims := jwt.MapClaims{}
	_, _, err = new(jwt.Parser).ParseUnverified(token, claims)
	require.NoError(t, err)
	expF, ok := claims["exp"].(float64)
	require.True(t, ok, "the capability must carry an expiry")

	remaining := time.Until(time.Unix(int64(expF), 0))
	assert.LessOrEqual(t, remaining, service.DefaultPlaybackTokenTTL+5*time.Second,
		"the capability must expire on the 60s policy, not the old 120s window")
	assert.Greater(t, remaining, service.DefaultPlaybackTokenTTL-5*time.Second)
}

// The gRPC entry point must reject an access token whose purpose is absent or
// is a playback capability.
func TestGrpcVerifyJWT_RefusesANonAccessTokenType(t *testing.T) {
	secret := "grpc-token-kind-secret-at-least-32-chars"

	for _, kind := range []string{"", "playback"} {
		claims := jwt.MapClaims{
			"wallet_address": "0xWallet",
			"session_id":     "s1",
			"auth_epoch":     float64(1),
			"iss":            service.AccessTokenIssuer,
			"aud":            service.AccessTokenAudience,
			"jti":            "jti-" + kind,
			"exp":            time.Now().Add(time.Hour).Unix(),
			"iat":            time.Now().Unix(),
		}
		if kind != "" {
			claims["token_type"] = kind
		}
		signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
		require.NoError(t, err)

		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"authorization": "Bearer " + signed,
		}))
		_, err = verifyGRPCJWT(ctx, "/streaming.StreamingService/GetManifest", secret, nil, nil)
		require.Error(t, err, "token_type=%q must be refused", kind)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	}
}

// When gRPC carries BOTH an access token and a capability, the wallet, session
// and epoch must agree; any disagreement is refused.
func TestGrpcGetManifest_RefusesConflictingDualCredentials(t *testing.T) {
	authSvc := service.NewAuthService(paritySecret, nil)
	authSvc.SetRuleVersionReader(func(context.Context, string) (int64, error) { return 1, nil })
	sessionID, err := authSvc.StartSession()
	require.NoError(t, err)

	// A capability for a DIFFERENT wallet, same live session and version.
	capability, err := authSvc.GeneratePlaybackToken(context.Background(), "0xOtherWallet", "c1", "", "", 1,
		time.Minute, "", service.WithSessionBinding(sessionID, 1), service.WithRuleVersion(1))
	require.NoError(t, err)

	srv, _ := newParityServer(t, big.NewInt(5), 1, authSvc)
	ctx := parityCtx(sessionID, 1, "0xWallet")
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("playback-token", capability))

	_, err = srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "c1"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

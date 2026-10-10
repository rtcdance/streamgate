package gateway

// gRPC streaming auth regression tests (TRANSITION_PLAN A7).
//
// GetSegment must bind the playback token to the caller's JWT-authenticated
// wallet (injected by grpcAuthInterceptor via grpcWalletKey). A token without
// a wallet binding must be rejected with Unauthenticated, not accepted on
// token possession alone.

import (
	"context"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/service"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const grpcAuthzSecret = "test-secret-that-is-at-least-32-chars"

// No wallet in the request context (i.e. no authenticated caller) + a valid
// playback token => Unauthenticated. Before A7 this returned Unavailable /
// NotFound because the token was accepted without any wallet binding.
func TestGRPC_GetSegment_TokenWithoutWallet_Unauthenticated(t *testing.T) {
	authSvc := service.NewAuthService(grpcAuthzSecret, nil)
	srv := &streamingGrpcServer{authSvc: authSvc, log: zap.NewNop()}

	token, err := authSvc.GeneratePlaybackToken(context.Background(), "0xabc", "content-1", "", "", 1, time.Minute, "")
	require.NoError(t, err)

	md := metadata.Pairs("playback-token", token)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	resp, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "content-1", SegmentId: "seg0.ts"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unauthenticated, status.Code(err),
		"a playback token without an authenticated wallet must not serve segments")
}

// With the wallet authenticated (as grpcAuthInterceptor would inject) and a
// token issued to the SAME wallet, the request must proceed past auth.
func TestGRPC_GetSegment_TokenWithMatchingWallet_PassesAuth(t *testing.T) {
	authSvc := service.NewAuthService(grpcAuthzSecret, nil)
	srv := &streamingGrpcServer{authSvc: authSvc, log: zap.NewNop()}

	token, err := authSvc.GeneratePlaybackToken(context.Background(), "0xabc", "content-1", "", "", 1, time.Minute, "")
	require.NoError(t, err)

	md := metadata.Pairs("playback-token", token)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = context.WithValue(ctx, grpcWalletKey, "0xabc")

	_, err = srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "content-1", SegmentId: "seg0.ts"})
	// segStore is nil in this harness, so the request must get PAST the auth
	// stage and fail later with Unavailable — anything else means the wallet
	// binding is wrong.
	assert.NotEqual(t, codes.Unauthenticated, status.Code(err),
		"a matching wallet must pass the auth stage (got %v)", err)
}

// A valid token issued to a DIFFERENT wallet must be rejected.
func TestGRPC_GetSegment_TokenWalletMismatch_Unauthenticated(t *testing.T) {
	authSvc := service.NewAuthService(grpcAuthzSecret, nil)
	srv := &streamingGrpcServer{authSvc: authSvc, log: zap.NewNop()}

	token, err := authSvc.GeneratePlaybackToken(context.Background(), "0xother", "content-1", "", "", 1, time.Minute, "")
	require.NoError(t, err)

	md := metadata.Pairs("playback-token", token)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = context.WithValue(ctx, grpcWalletKey, "0xabc")

	_, err = srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "content-1", SegmentId: "seg0.ts"})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err),
		"another wallet's playback token must not serve segments")
}

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

	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const grpcAuthzSecret = "test-secret-that-is-at-least-32-chars"

func bindTestGRPCCapabilitySession(t *testing.T, ctx context.Context, svc *service.AuthService, capability string) context.Context {
	t.Helper()
	claims, err := svc.ParseToken(capability)
	require.NoError(t, err)
	ctx = context.WithValue(ctx, grpcSessionKey, claims.SessionID)
	ctx = context.WithValue(ctx, grpcEpochKey, claims.AuthEpoch)
	return context.WithValue(ctx, grpcRestoreEpochKey, claims.RestoreEpoch)
}

func TestGRPC_GetSegment_RefusesAnotherSessionCapability(t *testing.T) {
	authSvc := service.NewAuthService(grpcAuthzSecret, nil)
	session := startTestSession(t, authSvc)
	other := startTestSession(t, authSvc)
	token, err := authSvc.GeneratePlaybackToken(context.Background(), "0xabc", "content-1", "", "", 1, time.Minute, "", service.WithSessionBinding(session, 1))
	require.NoError(t, err)
	currentRestore, ok := authSvc.RestoreEpoch()
	require.True(t, ok)
	for _, kind := range []string{"session", "version", "restore", "matching"} {
		t.Run(kind, func(t *testing.T) {
			accessSession, epoch, restore := session, int64(1), currentRestore
			if kind == "session" {
				accessSession = other
			}
			if kind == "version" {
				epoch = 2
			}
			if kind == "restore" {
				restore = "older-restore"
			}
			ctx := context.WithValue(parityCtx(accessSession, epoch, "0xabc"), grpcRestoreEpochKey, restore)
			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("playback-token", token))
			store := newGrpcMockSegmentStorage()
			require.NoError(t, store.Upload(context.Background(), "streamgate", "streams/content-1/seg0.ts", []byte("media")))
			srv := &streamingGrpcServer{authSvc: authSvc, segStore: store, log: zap.NewNop()}
			response, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "content-1", SegmentId: "seg0.ts"})
			if kind == "matching" {
				require.NoError(t, err)
				require.Equal(t, []byte("media"), response.Data)
			} else {
				require.Equal(t, codes.PermissionDenied, status.Code(err))
				require.Nil(t, response)
			}
		})
	}
}

// No wallet in the request context (i.e. no authenticated caller) + a valid
// playback token => Unauthenticated. Before A7 this returned Unavailable /
// NotFound because the token was accepted without any wallet binding.
func TestGRPC_GetSegment_TokenWithoutWallet_Unauthenticated(t *testing.T) {
	authSvc := service.NewAuthService(grpcAuthzSecret, nil)
	srv := &streamingGrpcServer{authSvc: authSvc, log: zap.NewNop()}

	token, err := authSvc.GeneratePlaybackToken(context.Background(), "0xabc", "content-1", "", "", 1, time.Minute, "",
		service.WithSessionBinding(startTestSession(t, authSvc), 1))
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

	token, err := authSvc.GeneratePlaybackToken(context.Background(), "0xabc", "content-1", "", "", 1, time.Minute, "",
		service.WithSessionBinding(startTestSession(t, authSvc), 1))
	require.NoError(t, err)

	md := metadata.Pairs("playback-token", token)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = context.WithValue(ctx, grpcWalletKey, "0xabc")
	ctx = bindTestGRPCCapabilitySession(t, ctx, authSvc, token)

	_, err = srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "content-1", SegmentId: "seg0.ts"})
	// segStore is nil in this harness, so the request must get PAST the auth
	// stage and fail later with Unavailable — anything else means the wallet
	// binding is wrong.
	assert.Equal(t, codes.Unavailable, status.Code(err),
		"a matching wallet must pass the auth stage (got %v)", err)
}

// A valid token issued to a DIFFERENT wallet must be rejected.
func TestGRPC_GetSegment_TokenWalletMismatch_Unauthenticated(t *testing.T) {
	authSvc := service.NewAuthService(grpcAuthzSecret, nil)
	srv := &streamingGrpcServer{authSvc: authSvc, log: zap.NewNop()}

	token, err := authSvc.GeneratePlaybackToken(context.Background(), "0xother", "content-1", "", "", 1, time.Minute, "",
		service.WithSessionBinding(startTestSession(t, authSvc), 1))
	require.NoError(t, err)

	md := metadata.Pairs("playback-token", token)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = context.WithValue(ctx, grpcWalletKey, "0xabc")

	_, err = srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "content-1", SegmentId: "seg0.ts"})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err),
		"another wallet's playback token must not serve segments")
}

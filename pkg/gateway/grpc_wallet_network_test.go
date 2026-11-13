package gateway

import (
	"context"
	"net"
	"testing"
	"time"

	authv1 "github.com/rtcdance/streamgate/pkg/api/v1/auth"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Generated clients must pass through the real interceptor. Calling the auth
// handler directly missed a stale unversioned allow-list and hid broken login.
// The proof fixture isolates dispatch; real ERC-1271 is covered by the stack.
func TestContractWalletTransport_NetworkAuthMethodsAndProtectedMedia(t *testing.T) {
	svc, _, _ := transportWalletFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(grpc.UnaryInterceptor(grpcAuthInterceptor(transportWalletSecret, svc, svc, zap.NewNop())))
	authv1.RegisterAuthServiceServer(server, &authGrpcServer{authSvc: svc, log: zap.NewNop()})
	streamingv1.RegisterStreamingServiceServer(server, &streamingGrpcServer{authSvc: svc, log: zap.NewNop()})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	auth, streaming := authv1.NewAuthServiceClient(connection), streamingv1.NewStreamingServiceClient(connection)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	nonce, err := auth.GetNonce(ctx, &authv1.GetNonceRequest{WalletAddress: transportContractWallet, ChainType: "ethereum"})
	require.NoError(t, err, "a new caller has no JWT before signing in")
	require.Contains(t, nonce.Message, "Chain ID: 31337")
	bad, err := auth.VerifySignature(ctx, &authv1.VerifySignatureRequest{WalletAddress: transportContractWallet, Nonce: nonce.Nonce, Signature: "0xnothex"})
	require.Error(t, err)
	require.Nil(t, bad, "a public signature endpoint still verifies the body")
	admitted, err := auth.VerifySignature(ctx, &authv1.VerifySignatureRequest{WalletAddress: transportContractWallet, Nonce: nonce.Nonce, Signature: "0x0102"})
	require.NoError(t, err)
	require.True(t, admitted.Valid)
	require.Equal(t, transportContractWallet, admitted.UserInfo.WalletAddress)
	refreshed, err := auth.RefreshToken(ctx, &authv1.RefreshTokenRequest{RefreshToken: admitted.AccessToken})
	require.NoError(t, err, "refresh validates its body credential and must not require a second live bearer")
	require.True(t, refreshed.Success)
	_, err = auth.RefreshToken(ctx, &authv1.RefreshTokenRequest{RefreshToken: "invalid-body-token"})
	require.Error(t, err)
	_, err = streaming.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "fixture"})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	_, err = streaming.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "fixture", SegmentId: "0.ts"})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	authv1 "github.com/rtcdance/streamgate/pkg/api/v1/auth"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// These controlled TCP services test the probe's rejection oracle only. The
// fixture JWTs are unsigned and cannot establish real application acceptance.
type credentialProbeServer struct {
	authv1.UnimplementedAuthServiceServer
	streamingv1.UnimplementedStreamingServiceServer
	validToken  string
	invalidate  bool
	mediaCode   codes.Code
	mediaCalls  atomic.Int32
	verifyCalls atomic.Int32
}

func (s *credentialProbeServer) VerifyToken(_ context.Context, request *authv1.VerifyTokenRequest) (*authv1.VerifyTokenResponse, error) {
	s.verifyCalls.Add(1)
	valid := request.GetToken() == s.validToken && (!s.invalidate || s.mediaCalls.Load() == 0)
	return &authv1.VerifyTokenResponse{Valid: valid}, nil
}

func (s *credentialProbeServer) GetSegment(context.Context, *streamingv1.GetSegmentRequest) (*streamingv1.GetSegmentResponse, error) {
	s.mediaCalls.Add(1)
	if s.mediaCode == codes.OK {
		return &streamingv1.GetSegmentResponse{}, nil
	}
	return nil, status.Error(s.mediaCode, "controlled probe response")
}

func probeClients(t *testing.T, service *credentialProbeServer) (streamingv1.StreamingServiceClient, authv1.AuthServiceClient) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	authv1.RegisterAuthServiceServer(server, service)
	streamingv1.RegisterStreamingServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	return streamingv1.NewStreamingServiceClient(connection), authv1.NewAuthServiceClient(connection)
}

func unsignedProbeJWT(t *testing.T, claims cwClaims) string {
	t.Helper()
	data, err := json.Marshal(claims)
	require.NoError(t, err)
	return "fixture." + base64.RawURLEncoding.EncodeToString(data) + ".unsigned"
}

func probeFixture(t *testing.T) (options, cwClaims) {
	t.Helper()
	now := time.Now().Unix()
	claims := cwClaims{Subject: "0x1111111111111111111111111111111111111111", Wallet: "0x1111111111111111111111111111111111111111",
		Chain: 31337, Kind: "erc1271", Session: "cw:test-session", Proof: "test-proof", Absolute: now + 7100, Issued: now - 35, Expires: now + 25}
	o := options{tokenFile: filepath.Join(t.TempDir(), "current.jwt"), walletSubject: claims.Wallet, walletChain: 31337, content: "content", segmentID: "segment"}
	require.NoError(t, os.WriteFile(o.tokenFile, []byte(unsignedProbeJWT(t, claims)), 0o600))
	media := claims
	media.Type = "playback"
	media.Subject = "content"
	o.capability = unsignedProbeJWT(t, media)
	return o, claims
}

func TestCWProbe_RequiresValidIdentityBeforeAndAfterMediaDenial(t *testing.T) {
	for _, tt := range []struct {
		name                        string
		invalidBefore, invalidAfter bool
		code                        codes.Code
		want                        bool
	}{
		{"valid-identity-and-denied-media", false, false, codes.PermissionDenied, true},
		{"valid-identity-and-invalid-capability", false, false, codes.Unauthenticated, true},
		{"already-rotated-jti", true, false, codes.Unauthenticated, false},
		{"jti-rotates-during-probe", false, true, codes.Unauthenticated, false},
		{"network-failure", false, false, codes.Unavailable, false},
		{"media-still-accepted", false, false, codes.OK, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o, claims := probeFixture(t)
			service := &credentialProbeServer{validToken: unsignedProbeJWT(t, claims), invalidate: tt.invalidAfter, mediaCode: tt.code}
			if tt.invalidBefore {
				service.validToken = "different-jti"
			}
			streaming, auth := probeClients(t, service)
			denied, err := guardedCWMediaProbe(streaming, auth, o, 3*time.Second)
			require.Equal(t, tt.want, denied)
			if tt.want {
				require.NoError(t, err)
				require.Equal(t, int32(2), service.verifyCalls.Load())
			}
			if tt.invalidBefore {
				require.Zero(t, service.mediaCalls.Load())
			}
		})
	}
}

func TestCWProbe_ReadsRenewedTokenButRejectsANewAdmission(t *testing.T) {
	o, claims := probeFixture(t)
	claims.Issued += 30
	claims.Expires += 30
	renewed := unsignedProbeJWT(t, claims)
	service := &credentialProbeServer{validToken: renewed, mediaCode: codes.PermissionDenied}
	streaming, auth := probeClients(t, service)
	denied, err := guardedCWMediaProbe(streaming, auth, o, time.Second)
	require.Error(t, err)
	require.False(t, denied)
	require.NoError(t, os.WriteFile(o.tokenFile, []byte(renewed), 0o600))
	denied, err = guardedCWMediaProbe(streaming, auth, o, time.Second)
	require.NoError(t, err)
	require.True(t, denied)
	claims.Session = "cw:replacement-admission"
	require.NoError(t, os.WriteFile(o.tokenFile, []byte(unsignedProbeJWT(t, claims)), 0o600))
	denied, err = guardedCWMediaProbe(streaming, auth, o, time.Second)
	require.Error(t, err)
	require.False(t, denied)
}

func TestCWProbe_RejectsExpiredForeignAndNonprivateCredentials(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*cwClaims)
	}{
		{"expired", func(c *cwClaims) { c.Issued -= 100; c.Expires -= 100 }},
		{"long-lease", func(c *cwClaims) { c.Expires += 1 }},
		{"foreign-chain", func(c *cwClaims) { c.Chain++ }},
		{"owner-instead-of-C", func(c *cwClaims) { c.Wallet = "0x2222222222222222222222222222222222222222" }},
		{"no-proof", func(c *cwClaims) { c.Proof = "" }},
		{"legacy-session", func(c *cwClaims) { c.Session = "legacy" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o, claims := probeFixture(t)
			tt.change(&claims)
			require.NoError(t, os.WriteFile(o.tokenFile, []byte(unsignedProbeJWT(t, claims)), 0o600))
			_, err := currentCWToken(o)
			require.Error(t, err)
		})
	}
	o, _ := probeFixture(t)
	require.NoError(t, os.Chmod(o.tokenFile, 0o644))
	_, err := currentCWToken(o)
	require.Error(t, err)
}

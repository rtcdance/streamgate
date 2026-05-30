package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/models"

	contentv1 "github.com/rtcdance/streamgate/pkg/api/v1/content"
	nftv1 "github.com/rtcdance/streamgate/pkg/api/v1/nft"
	servicev1 "github.com/rtcdance/streamgate/pkg/api/v1/service"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	uploadv1 "github.com/rtcdance/streamgate/pkg/api/v1/upload"
	"github.com/rtcdance/streamgate/pkg/service"

	jwt "github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(_ context.Context) error { return m.err }

type grpcMockTokenBlacklist struct {
	revoked map[string]bool
	mu      sync.RWMutex
}

func newGrpcMockTokenBlacklist() *grpcMockTokenBlacklist {
	return &grpcMockTokenBlacklist{revoked: make(map[string]bool)}
}

func (m *grpcMockTokenBlacklist) IsTokenRevoked(_ context.Context, jti string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.revoked[jti]
}

func (m *grpcMockTokenBlacklist) revoke(jti string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked[jti] = true
}

type grpcMockNFTChecker struct {
	owns    bool
	ownsErr error
	balance *big.Int
	balErr  error
}

func (m *grpcMockNFTChecker) VerifyNFTOwnership(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return m.owns, m.ownsErr
}
func (m *grpcMockNFTChecker) GetNFTBalance(_ context.Context, _ int64, _, _ string) (*big.Int, error) {
	return m.balance, m.balErr
}
func (m *grpcMockNFTChecker) VerifyNFTOwnershipAutoDetect(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return m.owns, m.ownsErr
}
func (m *grpcMockNFTChecker) VerifyNFTCollectionAutoDetect(_ context.Context, _ int64, _, _ string) (bool, error) {
	return m.owns, m.ownsErr
}
func (m *grpcMockNFTChecker) GetNFTInfo(_ context.Context, _ int64, _, _ string) (*middleware.NFTMetadata, error) {
	return nil, nil
}

type grpcMockSegmentStorage struct {
	data map[string][]byte
	mu   sync.RWMutex
	list []string
	lErr error
	dErr error
}

func newGrpcMockSegmentStorage() *grpcMockSegmentStorage {
	return &grpcMockSegmentStorage{data: make(map[string][]byte)}
}

func (s *grpcMockSegmentStorage) Upload(_ context.Context, bucket, key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[bucket+"/"+key] = data
	return nil
}
func (s *grpcMockSegmentStorage) UploadStream(_ context.Context, bucket, key string, reader io.Reader, size int64) error {
	return nil
}
func (s *grpcMockSegmentStorage) UploadWithContentType(_ context.Context, bucket, key string, data []byte, _ string) error {
	return nil
}
func (s *grpcMockSegmentStorage) UploadStreamWithContentType(_ context.Context, bucket, key string, reader io.Reader, size int64, _ string) error {
	return nil
}
func (s *grpcMockSegmentStorage) Download(_ context.Context, bucket, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dErr != nil {
		return nil, s.dErr
	}
	d, ok := s.data[bucket+"/"+key]
	if !ok {
		return nil, errors.New("not found")
	}
	return d, nil
}
func (s *grpcMockSegmentStorage) DownloadStream(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	return nil, nil
}
func (s *grpcMockSegmentStorage) Delete(_ context.Context, bucket, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, bucket+"/"+key)
	return nil
}
func (s *grpcMockSegmentStorage) ListObjects(_ context.Context, bucket, prefix string) ([]string, error) {
	if s.lErr != nil {
		return nil, s.lErr
	}
	return s.list, nil
}
func (s *grpcMockSegmentStorage) Exists(_ context.Context, bucket, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[bucket+"/"+key]
	return ok, nil
}

type grpcMockAuthStorage struct {
	users map[string]*models.User
	mu    sync.RWMutex
}

func newGrpcMockAuthStorage() *grpcMockAuthStorage {
	return &grpcMockAuthStorage{users: make(map[string]*models.User)}
}

func (m *grpcMockAuthStorage) GetUser(_ context.Context, username string) (*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[username]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}
func (m *grpcMockAuthStorage) CreateUser(_ context.Context, user *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.Username] = user
	return nil
}
func (m *grpcMockAuthStorage) UpdateUser(_ context.Context, user *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.Username] = user
	return nil
}

func generateTestJWT(t *testing.T, wallet, secret, jti string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"wallet_address": wallet,
		"jti":            jti,
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	return tokenStr
}

func generateTestJWTNoWallet(t *testing.T, secret, jti string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"jti": jti,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	return tokenStr
}

func TestValidatePagination(t *testing.T) {
	tests := []struct {
		name         string
		page         int
		pageSize     int
		wantPage     int
		wantPageSize int
	}{
		{"defaults for zero", 0, 0, 1, 20},
		{"negative page clamped", -1, 20, 1, 20},
		{"negative pageSize clamped", 1, -1, 1, 20},
		{"pageSize over 100 clamped", 1, 200, 1, 100},
		{"valid values pass through", 3, 50, 3, 50},
		{"page zero clamped", 0, 10, 1, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ps := validatePagination(tt.page, tt.pageSize)
			assert.Equal(t, tt.wantPage, p)
			assert.Equal(t, tt.wantPageSize, ps)
		})
	}
}

func TestValidateID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid id", "abc123", false},
		{"empty id", "", false},
		{"dot in id", "a.b", true},
		{"slash in id", "a/b", true},
		{"backslash in id", "a\\b", true},
		{"path traversal", "../etc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateID(tt.id)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGrpcWalletFromContext(t *testing.T) {
	t.Run("no wallet in context", func(t *testing.T) {
		wallet := grpcWalletFromContext(context.Background())
		assert.Equal(t, "", wallet)
	})

	t.Run("wallet in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, "0xABC")
		wallet := grpcWalletFromContext(ctx)
		assert.Equal(t, "0xABC", wallet)
	})

	t.Run("wrong type in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, 12345)
		wallet := grpcWalletFromContext(ctx)
		assert.Equal(t, "", wallet)
	})
}

func TestHealthGrpcServer_Check(t *testing.T) {
	log := zap.NewNop()

	t.Run("serving when all nil", func(t *testing.T) {
		srv := &healthGrpcServer{log: log}
		resp, err := srv.Check(context.Background(), &servicev1.HealthCheckRequest{})
		require.NoError(t, err)
		assert.Equal(t, servicev1.HealthCheckResponse_SERVING, resp.Status)
	})

	t.Run("not serving when db fails", func(t *testing.T) {
		srv := &healthGrpcServer{log: log, db: &mockPinger{err: errors.New("db down")}}
		resp, err := srv.Check(context.Background(), &servicev1.HealthCheckRequest{})
		require.NoError(t, err)
		assert.Equal(t, servicev1.HealthCheckResponse_NOT_SERVING, resp.Status)
	})

	t.Run("not serving when cache fails", func(t *testing.T) {
		srv := &healthGrpcServer{log: log, cache: &mockPinger{err: errors.New("cache down")}}
		resp, err := srv.Check(context.Background(), &servicev1.HealthCheckRequest{})
		require.NoError(t, err)
		assert.Equal(t, servicev1.HealthCheckResponse_NOT_SERVING, resp.Status)
	})

	t.Run("serving when both healthy", func(t *testing.T) {
		srv := &healthGrpcServer{log: log, db: &mockPinger{}, cache: &mockPinger{}}
		resp, err := srv.Check(context.Background(), &servicev1.HealthCheckRequest{})
		require.NoError(t, err)
		assert.Equal(t, servicev1.HealthCheckResponse_SERVING, resp.Status)
	})
}

func TestHealthGrpcServer_servingStatus(t *testing.T) {
	log := zap.NewNop()

	t.Run("serving with nil deps", func(t *testing.T) {
		srv := &healthGrpcServer{log: log}
		s := srv.servingStatus()
		assert.Equal(t, servicev1.HealthCheckResponse_SERVING, s)
	})

	t.Run("not serving when db fails", func(t *testing.T) {
		srv := &healthGrpcServer{log: log, db: &mockPinger{err: errors.New("down")}}
		s := srv.servingStatus()
		assert.Equal(t, servicev1.HealthCheckResponse_NOT_SERVING, s)
	})
}

func TestHealthGrpcServer_Watch(t *testing.T) {
	log := zap.NewNop()
	srv := &healthGrpcServer{log: log, db: &mockPinger{}, cache: &mockPinger{}}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	stream := &mockHealthWatchServer{ctx: ctx}
	err := srv.Watch(&servicev1.HealthCheckRequest{}, stream)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || err == nil)
}

type mockHealthWatchServer struct {
	servicev1.HealthService_WatchServer
	ctx  context.Context
	sent []*servicev1.HealthCheckResponse
}

func (m *mockHealthWatchServer) Context() context.Context { return m.ctx }
func (m *mockHealthWatchServer) Send(resp *servicev1.HealthCheckResponse) error {
	m.sent = append(m.sent, resp)
	return nil
}

func TestVerifyGRPCJWT_NoAuthMethods(t *testing.T) {
	tests := []struct {
		name   string
		method string
	}{
		{"GetNonce", "/auth.AuthService/GetNonce"},
		{"VerifySignature", "/auth.AuthService/VerifySignature"},
		{"HealthCheck", "/health.HealthService/Check"},
		{"HealthWatch", "/health.HealthService/Watch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, err := verifyGRPCJWT(context.Background(), tt.method, "secret", nil)
			assert.NoError(t, err)
			assert.NotNil(t, ctx)
		})
	}
}

func TestVerifyGRPCJWT_MissingMetadata(t *testing.T) {
	ctx, err := verifyGRPCJWT(context.Background(), "/some.Service/Method", "secret", nil)
	assert.Error(t, err)
	assert.Nil(t, ctx)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestVerifyGRPCJWT_MissingAuthorization(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{}))
	newCtx, err := verifyGRPCJWT(ctx, "/some.Service/Method", "secret", nil)
	assert.Error(t, err)
	assert.Nil(t, newCtx)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestVerifyGRPCJWT_EmptyToken(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer ",
	}))
	newCtx, err := verifyGRPCJWT(ctx, "/some.Service/Method", "secret", nil)
	assert.Error(t, err)
	assert.Nil(t, newCtx)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestVerifyGRPCJWT_InvalidToken(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer invalid.token.here",
	}))
	newCtx, err := verifyGRPCJWT(ctx, "/some.Service/Method", "secret", nil)
	assert.Error(t, err)
	assert.Nil(t, newCtx)
}

func TestVerifyGRPCJWT_ValidToken(t *testing.T) {
	secret := "test-secret-key-that-is-at-least-32-chars"
	token := generateTestJWT(t, "0xTestWallet", secret, "test-jti-1")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer " + token,
	}))
	newCtx, err := verifyGRPCJWT(ctx, "/some.Service/Method", secret, nil)
	require.NoError(t, err)
	wallet := grpcWalletFromContext(newCtx)
	assert.Equal(t, "0xTestWallet", wallet)
}

func TestVerifyGRPCJWT_RevokedToken(t *testing.T) {
	secret := "test-secret-key-that-is-at-least-32-chars"
	bl := newGrpcMockTokenBlacklist()
	bl.revoke("revoked-jti")

	token := generateTestJWT(t, "0xTestWallet", secret, "revoked-jti")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer " + token,
	}))
	newCtx, err := verifyGRPCJWT(ctx, "/some.Service/Method", secret, bl)
	assert.Error(t, err)
	assert.Nil(t, newCtx)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestVerifyGRPCJWT_NoWalletInClaims(t *testing.T) {
	secret := "test-secret-key-that-is-at-least-32-chars"
	token := generateTestJWTNoWallet(t, secret, "jti-no-wallet")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer " + token,
	}))
	newCtx, err := verifyGRPCJWT(ctx, "/some.Service/Method", secret, nil)
	assert.Error(t, err)
	assert.Nil(t, newCtx)
}

func TestGrpcRecoveryInterceptor(t *testing.T) {
	log := zap.NewNop()
	interceptor := grpcRecoveryInterceptor(log)

	t.Run("normal handler passes through", func(t *testing.T) {
		resp, err := interceptor(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: "/test"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "ok", resp)
	})

	t.Run("panic is recovered", func(t *testing.T) {
		resp, err := interceptor(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: "/test"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			panic("test panic")
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
	})
}

func TestGrpcLoggingInterceptor(t *testing.T) {
	log := zap.NewNop()
	interceptor := grpcLoggingInterceptor(log)

	t.Run("successful call", func(t *testing.T) {
		resp, err := interceptor(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: "/test"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "ok", resp)
	})

	t.Run("error call", func(t *testing.T) {
		resp, err := interceptor(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: "/test"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, status.Error(codes.NotFound, "not found")
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("with request ID in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcRequestIDKey{}, "req-123")
		resp, err := interceptor(ctx, "req", &grpc.UnaryServerInfo{FullMethod: "/test"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "ok", resp)
	})
}

func TestGrpcRequestIDUnaryInterceptor(t *testing.T) {
	interceptor := grpcRequestIDUnaryInterceptor()

	t.Run("with request ID in metadata", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"x-request-id": "req-test-123",
		}))
		var capturedReqID string
		_, err := interceptor(ctx, "req", &grpc.UnaryServerInfo{FullMethod: "/test"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			capturedReqID, _ = ctx.Value(grpcRequestIDKey{}).(string)
			return "ok", nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "req-test-123", capturedReqID)
	})

	t.Run("without request ID in metadata", func(t *testing.T) {
		ctx := context.Background()
		var capturedReqID string
		_, err := interceptor(ctx, "req", &grpc.UnaryServerInfo{FullMethod: "/test"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			capturedReqID, _ = ctx.Value(grpcRequestIDKey{}).(string)
			return "ok", nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "", capturedReqID)
	})

	t.Run("empty request ID in metadata", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"x-request-id": "",
		}))
		var capturedReqID string
		_, err := interceptor(ctx, "req", &grpc.UnaryServerInfo{FullMethod: "/test"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			capturedReqID, _ = ctx.Value(grpcRequestIDKey{}).(string)
			return "ok", nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "", capturedReqID)
	})
}

func TestGrpcStreamRecoveryInterceptor(t *testing.T) {
	log := zap.NewNop()
	interceptor := grpcStreamRecoveryInterceptor(log)

	t.Run("normal handler passes through", func(t *testing.T) {
		err := interceptor("srv", nil, &grpc.StreamServerInfo{FullMethod: "/test"}, func(srv interface{}, ss grpc.ServerStream) error {
			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("panic is recovered", func(t *testing.T) {
		err := interceptor("srv", nil, &grpc.StreamServerInfo{FullMethod: "/test"}, func(srv interface{}, ss grpc.ServerStream) error {
			panic("stream panic")
		})
		assert.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
	})
}

func TestGrpcStreamLoggingInterceptor(t *testing.T) {
	log := zap.NewNop()
	interceptor := grpcStreamLoggingInterceptor(log)

	t.Run("successful call", func(t *testing.T) {
		err := interceptor("srv", nil, &grpc.StreamServerInfo{FullMethod: "/test"}, func(srv interface{}, ss grpc.ServerStream) error {
			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("error call", func(t *testing.T) {
		err := interceptor("srv", nil, &grpc.StreamServerInfo{FullMethod: "/test"}, func(srv interface{}, ss grpc.ServerStream) error {
			return status.Error(codes.NotFound, "not found")
		})
		assert.Error(t, err)
	})
}

func TestGrpcStreamAuthInterceptor(t *testing.T) {
	log := zap.NewNop()
	interceptor := grpcStreamAuthInterceptor("secret", nil, log)

	t.Run("no auth method passes through", func(t *testing.T) {
		stream := &mockServerStream{ctx: context.Background()}
		err := interceptor("srv", stream, &grpc.StreamServerInfo{FullMethod: "/auth.AuthService/GetNonce"}, func(srv interface{}, ss grpc.ServerStream) error {
			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("missing metadata returns error", func(t *testing.T) {
		stream := &mockServerStream{ctx: context.Background()}
		err := interceptor("srv", stream, &grpc.StreamServerInfo{FullMethod: "/some.Service/Method"}, func(srv interface{}, ss grpc.ServerStream) error {
			return nil
		})
		assert.Error(t, err)
	})
}

func TestGrpcAuthInterceptor(t *testing.T) {
	secret := "test-secret-key-that-is-at-least-32-chars"
	log := zap.NewNop()
	interceptor := grpcAuthInterceptor(secret, nil, log)

	t.Run("no auth method passes through", func(t *testing.T) {
		resp, err := interceptor(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/GetNonce"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "ok", resp)
	})

	t.Run("missing metadata returns error", func(t *testing.T) {
		resp, err := interceptor(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: "/some.Service/Method"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("valid token passes through", func(t *testing.T) {
		token := generateTestJWT(t, "0xTestWallet", secret, "jti-ok")
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"authorization": "Bearer " + token,
		}))
		resp, err := interceptor(ctx, "req", &grpc.UnaryServerInfo{FullMethod: "/some.Service/Method"}, func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "ok", resp)
	})
}

type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context { return m.ctx }

func TestWrappedStream_Context(t *testing.T) {
	ctx := context.WithValue(context.Background(), grpcRequestIDKey{}, "test-req-id")
	ws := &wrappedStream{ctx: ctx}
	assert.Equal(t, ctx, ws.Context())
}

func TestNftGrpcServer_VerifyOwnership(t *testing.T) {
	log := zap.NewNop()

	t.Run("owns NFT", func(t *testing.T) {
		srv := &nftGrpcServer{
			nftVerifier: &grpcMockNFTChecker{owns: true},
			log:         log,
		}
		resp, err := srv.VerifyOwnership(context.Background(), &nftv1.VerifyOwnershipRequest{
			ChainId:         1,
			ContractAddress: "0xContract",
			TokenId:         "1",
			WalletAddress:   "0xWallet",
		})
		require.NoError(t, err)
		assert.True(t, resp.OwnsNft)
		assert.Equal(t, "0xWallet", resp.OwnerAddress)
	})

	t.Run("verification error", func(t *testing.T) {
		srv := &nftGrpcServer{
			nftVerifier: &grpcMockNFTChecker{owns: false, ownsErr: errors.New("rpc error")},
			log:         log,
		}
		resp, err := srv.VerifyOwnership(context.Background(), &nftv1.VerifyOwnershipRequest{
			ChainId:         1,
			ContractAddress: "0xContract",
			TokenId:         "1",
			WalletAddress:   "0xWallet",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Internal, st.Code())
	})
}

func TestNftGrpcServer_GetNFTBalance(t *testing.T) {
	log := zap.NewNop()

	t.Run("success", func(t *testing.T) {
		srv := &nftGrpcServer{
			nftVerifier: &grpcMockNFTChecker{balance: big.NewInt(5)},
			log:         log,
		}
		resp, err := srv.GetNFTBalance(context.Background(), &nftv1.GetNFTBalanceRequest{
			ChainId:         1,
			ContractAddress: "0xContract",
			WalletAddress:   "0xWallet",
		})
		require.NoError(t, err)
		assert.Equal(t, int64(5), resp.Balance)
	})

	t.Run("error", func(t *testing.T) {
		srv := &nftGrpcServer{
			nftVerifier: &grpcMockNFTChecker{balErr: errors.New("rpc error")},
			log:         log,
		}
		resp, err := srv.GetNFTBalance(context.Background(), &nftv1.GetNFTBalanceRequest{
			ChainId:         1,
			ContractAddress: "0xContract",
			WalletAddress:   "0xWallet",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestNftGrpcServer_GetNFTMetadata(t *testing.T) {
	log := zap.NewNop()

	t.Run("no web3 service", func(t *testing.T) {
		srv := &nftGrpcServer{web3Svc: nil, log: log}
		resp, err := srv.GetNFTMetadata(context.Background(), &nftv1.GetNFTMetadataRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestNftGrpcServer_ListUserNFTs(t *testing.T) {
	log := zap.NewNop()

	t.Run("no wallet returns error", func(t *testing.T) {
		srv := &nftGrpcServer{log: log}
		resp, err := srv.ListUserNFTs(context.Background(), &nftv1.ListUserNFTsRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("wallet from context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
		srv := &nftGrpcServer{log: log}
		resp, err := srv.ListUserNFTs(ctx, &nftv1.ListUserNFTsRequest{})
		require.NoError(t, err)
		assert.Empty(t, resp.Nfts)
		assert.Equal(t, int32(0), resp.Total)
	})

	t.Run("wallet from request fallback", func(t *testing.T) {
		srv := &nftGrpcServer{log: log}
		resp, err := srv.ListUserNFTs(context.Background(), &nftv1.ListUserNFTsRequest{
			WalletAddress: "0xWallet",
		})
		require.NoError(t, err)
		assert.Empty(t, resp.Nfts)
	})

	t.Run("pagination", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
		srv := &nftGrpcServer{log: log}
		resp, err := srv.ListUserNFTs(ctx, &nftv1.ListUserNFTsRequest{
			Page:     2,
			PageSize: 5,
		})
		require.NoError(t, err)
		assert.Equal(t, int32(2), resp.Page)
		assert.Equal(t, int32(5), resp.PageSize)
	})
}

func TestNftGrpcServer_GetContractInfo(t *testing.T) {
	log := zap.NewNop()

	t.Run("empty contract address", func(t *testing.T) {
		srv := &nftGrpcServer{log: log}
		resp, err := srv.GetContractInfo(context.Background(), &nftv1.GetContractInfoRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero chain id", func(t *testing.T) {
		srv := &nftGrpcServer{log: log}
		resp, err := srv.GetContractInfo(context.Background(), &nftv1.GetContractInfoRequest{
			ContractAddress: "0xContract",
			ChainId:         0,
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no web3 service", func(t *testing.T) {
		srv := &nftGrpcServer{log: log, web3Svc: nil}
		resp, err := srv.GetContractInfo(context.Background(), &nftv1.GetContractInfoRequest{
			ContractAddress: "0xContract",
			ChainId:         1,
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestContentGrpcServer_GetContent(t *testing.T) {
	log := zap.NewNop()

	t.Run("empty content id", func(t *testing.T) {
		srv := &contentGrpcServer{log: log}
		resp, err := srv.GetContent(context.Background(), &contentv1.GetContentRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid content id", func(t *testing.T) {
		srv := &contentGrpcServer{log: log}
		resp, err := srv.GetContent(context.Background(), &contentv1.GetContentRequest{
			ContentId: "../etc",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestContentGrpcServer_VerifyAccess(t *testing.T) {
	srv := &contentGrpcServer{}
	resp, err := srv.VerifyAccess(context.Background(), &contentv1.VerifyAccessRequest{})
	assert.Error(t, err)
	assert.Nil(t, resp)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unimplemented, st.Code())
}

func TestContentGrpcServer_GetTranscodeStatus(t *testing.T) {
	log := zap.NewNop()

	t.Run("no transcode service", func(t *testing.T) {
		srv := &contentGrpcServer{log: log}
		resp, err := srv.GetTranscodeStatus(context.Background(), &contentv1.GetTranscodeStatusRequest{
			ContentId: "content-1",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unavailable, st.Code())
	})

	t.Run("empty content id", func(t *testing.T) {
		svc := service.NewTranscodingService(nil, nil)
		srv := &contentGrpcServer{transcodeSvc: svc, log: log}
		resp, err := srv.GetTranscodeStatus(context.Background(), &contentv1.GetTranscodeStatusRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid content id", func(t *testing.T) {
		svc := service.NewTranscodingService(nil, nil)
		srv := &contentGrpcServer{transcodeSvc: svc, log: log}
		resp, err := srv.GetTranscodeStatus(context.Background(), &contentv1.GetTranscodeStatusRequest{
			ContentId: "../etc",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestContentGrpcServer_ListContent(t *testing.T) {
	log := zap.NewNop()

	t.Run("no wallet returns error", func(t *testing.T) {
		srv := &contentGrpcServer{log: log}
		resp, err := srv.ListContent(context.Background(), &contentv1.ListContentRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})
}

func TestContentGrpcServer_DeleteContent(t *testing.T) {
	log := zap.NewNop()

	t.Run("empty content id", func(t *testing.T) {
		srv := &contentGrpcServer{log: log}
		resp, err := srv.DeleteContent(context.Background(), &contentv1.DeleteContentRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid content id", func(t *testing.T) {
		srv := &contentGrpcServer{log: log}
		resp, err := srv.DeleteContent(context.Background(), &contentv1.DeleteContentRequest{
			ContentId: "a/b",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no wallet returns error", func(t *testing.T) {
		srv := &contentGrpcServer{log: log}
		resp, err := srv.DeleteContent(context.Background(), &contentv1.DeleteContentRequest{
			ContentId: "content-1",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})
}

func TestStreamingGrpcServer_Unimplemented(t *testing.T) {
	srv := &streamingGrpcServer{}

	t.Run("GetStreamURL", func(t *testing.T) {
		resp, err := srv.GetStreamURL(context.Background(), &streamingv1.GetStreamURLRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("StartStream", func(t *testing.T) {
		resp, err := srv.StartStream(context.Background(), &streamingv1.StartStreamRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("StopStream", func(t *testing.T) {
		resp, err := srv.StopStream(context.Background(), &streamingv1.StopStreamRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("GetStreamStats", func(t *testing.T) {
		resp, err := srv.GetStreamStats(context.Background(), &streamingv1.GetStreamStatsRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestStreamingGrpcServer_GetManifest_AuthChecks(t *testing.T) {
	log := zap.NewNop()

	t.Run("no wallet", func(t *testing.T) {
		srv := &streamingGrpcServer{log: log}
		resp, err := srv.GetManifest(context.Background(), &streamingv1.GetManifestRequest{ContentId: "c1"})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("empty content id", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
		srv := &streamingGrpcServer{log: log}
		resp, err := srv.GetManifest(ctx, &streamingv1.GetManifestRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid content id", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
		srv := &streamingGrpcServer{log: log}
		resp, err := srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "../etc"})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no contract metadata", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
		srv := &streamingGrpcServer{log: log}
		resp, err := srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "c1"})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})

	t.Run("no NFT verifier", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
		ctx = metadata.NewIncomingContext(ctx, metadata.New(map[string]string{
			"x-nft-contract": "0xContract",
		}))
		srv := &streamingGrpcServer{nftVerifier: nil, log: log}
		resp, err := srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "c1"})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Internal, st.Code())
	})

	t.Run("NFT verification fails", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
		ctx = metadata.NewIncomingContext(ctx, metadata.New(map[string]string{
			"x-nft-contract": "0xContract",
		}))
		srv := &streamingGrpcServer{
			nftVerifier: &grpcMockNFTChecker{owns: false, ownsErr: errors.New("rpc fail")},
			log:         log,
		}
		resp, err := srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "c1"})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})

	t.Run("NFT not owned", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
		ctx = metadata.NewIncomingContext(ctx, metadata.New(map[string]string{
			"x-nft-contract": "0xContract",
		}))
		srv := &streamingGrpcServer{
			nftVerifier: &grpcMockNFTChecker{owns: false},
			log:         log,
		}
		resp, err := srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "c1"})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})
}

func TestStreamingGrpcServer_GetSegment_AuthChecks(t *testing.T) {
	log := zap.NewNop()
	authSvc := service.NewAuthService("test-secret-key-that-is-at-least-32-chars", newGrpcMockAuthStorage())

	t.Run("missing metadata", func(t *testing.T) {
		srv := &streamingGrpcServer{log: log}
		resp, err := srv.GetSegment(context.Background(), &streamingv1.GetSegmentRequest{
			ContentId: "c1", SegmentId: "seg1.ts",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("empty playback token", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"playback-token": "",
		}))
		srv := &streamingGrpcServer{log: log}
		resp, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{
			ContentId: "c1", SegmentId: "seg1.ts",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("invalid playback token", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"playback-token": "invalid-token",
		}))
		srv := &streamingGrpcServer{authSvc: authSvc, log: log}
		resp, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{
			ContentId: "c1", SegmentId: "seg1.ts",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("invalid segment id", func(t *testing.T) {
		playbackToken, err := authSvc.GeneratePlaybackToken(context.Background(), "0xWallet", "c1", "", "", 1, 2*time.Minute, "")
		require.NoError(t, err)
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"playback-token": playbackToken,
		}))
		srv := &streamingGrpcServer{authSvc: authSvc, log: log}

		for _, segID := range []string{"../etc/passwd", "path/seg", "path\\seg"} {
			resp, segErr := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{
				ContentId: "c1", SegmentId: segID,
			})
			assert.Error(t, segErr)
			assert.Nil(t, resp)
			st, _ := status.FromError(segErr)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		}
	})

	t.Run("invalid content id", func(t *testing.T) {
		playbackToken, err := authSvc.GeneratePlaybackToken(context.Background(), "0xWallet", "c1", "", "", 1, 2*time.Minute, "")
		require.NoError(t, err)
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"playback-token": playbackToken,
		}))
		srv := &streamingGrpcServer{authSvc: authSvc, log: log}
		resp, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{
			ContentId: "../etc", SegmentId: "seg1.ts",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("no segment storage", func(t *testing.T) {
		playbackToken, err := authSvc.GeneratePlaybackToken(context.Background(), "0xWallet", "c1", "", "", 1, 2*time.Minute, "")
		require.NoError(t, err)
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"playback-token": playbackToken,
		}))
		srv := &streamingGrpcServer{authSvc: authSvc, segStore: nil, log: log}
		resp, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{
			ContentId: "c1", SegmentId: "seg1.ts",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unavailable, st.Code())
	})

	t.Run("segment not found", func(t *testing.T) {
		playbackToken, err := authSvc.GeneratePlaybackToken(context.Background(), "0xWallet", "c1", "", "", 1, 2*time.Minute, "")
		require.NoError(t, err)
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"playback-token": playbackToken,
		}))
		segStore := newGrpcMockSegmentStorage()
		srv := &streamingGrpcServer{authSvc: authSvc, segStore: segStore, log: log}
		resp, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{
			ContentId: "c1", SegmentId: "seg1.ts",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("segment found", func(t *testing.T) {
		playbackToken, err := authSvc.GeneratePlaybackToken(context.Background(), "0xWallet", "c1", "", "", 1, 2*time.Minute, "")
		require.NoError(t, err)
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"playback-token": playbackToken,
		}))
		segStore := newGrpcMockSegmentStorage()
		segData := []byte("fake-ts-data")
		_ = segStore.Upload(context.Background(), "streamgate", "streams/c1/seg1.ts", segData)
		srv := &streamingGrpcServer{authSvc: authSvc, segStore: segStore, log: log}
		resp, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{
			ContentId: "c1", SegmentId: "seg1.ts",
		})
		require.NoError(t, err)
		assert.Equal(t, segData, resp.Data)
		assert.Equal(t, "video/mp2t", resp.ContentType)
		assert.Equal(t, int64(len(segData)), resp.Size)
	})
}

func TestUploadGrpcServer_InitUpload_NoWallet(t *testing.T) {
	log := zap.NewNop()
	srv := &uploadGrpcServer{log: log}
	resp, err := srv.InitUpload(context.Background(), &uploadv1.InitUploadRequest{
		Filename: "test.mp4", FileSize: 1000, ChunkSize: 100,
	})
	assert.Error(t, err)
	assert.Nil(t, resp)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestUploadGrpcServer_InitUpload_Validation(t *testing.T) {
	log := zap.NewNop()
	ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
	svc := service.NewUploadService(nil, nil, "bucket")
	srv := &uploadGrpcServer{uploadSvc: svc, log: log}

	tests := []struct {
		name     string
		req      *uploadv1.InitUploadRequest
		wantCode codes.Code
	}{
		{"empty filename", &uploadv1.InitUploadRequest{Filename: "", FileSize: 1000, ChunkSize: 100}, codes.InvalidArgument},
		{"negative file size", &uploadv1.InitUploadRequest{Filename: "test.mp4", FileSize: -1, ChunkSize: 100}, codes.InvalidArgument},
		{"file too large", &uploadv1.InitUploadRequest{Filename: "test.mp4", FileSize: 600 * 1024 * 1024, ChunkSize: 100}, codes.InvalidArgument},
		{"zero chunk size", &uploadv1.InitUploadRequest{Filename: "test.mp4", FileSize: 1000, ChunkSize: 0}, codes.InvalidArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := srv.InitUpload(ctx, tt.req)
			assert.Error(t, err)
			assert.Nil(t, resp)
			st, _ := status.FromError(err)
			assert.Equal(t, tt.wantCode, st.Code())
		})
	}
}

func TestUploadGrpcServer_InitUpload_TooManyChunks(t *testing.T) {
	log := zap.NewNop()
	ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
	svc := service.NewUploadService(nil, nil, "bucket")
	srv := &uploadGrpcServer{uploadSvc: svc, log: log}

	resp, err := srv.InitUpload(ctx, &uploadv1.InitUploadRequest{
		Filename:  "test.mp4",
		FileSize:  500 * 1024 * 1024,
		ChunkSize: 1,
	})
	assert.Error(t, err)
	assert.Nil(t, resp)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUploadGrpcServer_NoWalletMethods(t *testing.T) {
	log := zap.NewNop()
	srv := &uploadGrpcServer{log: log}

	t.Run("CompleteUpload", func(t *testing.T) {
		resp, err := srv.CompleteUpload(context.Background(), &uploadv1.CompleteUploadRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("AbortUpload", func(t *testing.T) {
		resp, err := srv.AbortUpload(context.Background(), &uploadv1.AbortUploadRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("GetUploadStatus", func(t *testing.T) {
		resp, err := srv.GetUploadStatus(context.Background(), &uploadv1.GetUploadStatusRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestGrpcNoAuthMethods(t *testing.T) {
	expected := map[string]bool{
		"/auth.AuthService/GetNonce":        true,
		"/auth.AuthService/VerifySignature": true,
		"/health.HealthService/Check":       true,
		"/health.HealthService/Watch":       true,
	}
	assert.Equal(t, expected, grpcNoAuthMethods)
}

func TestGrpcRequestIDStreamInterceptor(t *testing.T) {
	interceptor := grpcRequestIDStreamInterceptor()

	t.Run("with request ID in metadata", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"x-request-id": "stream-req-123",
		}))
		stream := &mockServerStream{ctx: ctx}
		var capturedReqID string
		err := interceptor("srv", stream, nil, func(srv interface{}, ss grpc.ServerStream) error {
			ws, ok := ss.(*wrappedStream)
			if ok {
				capturedReqID, _ = ws.Context().Value(grpcRequestIDKey{}).(string)
			}
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "stream-req-123", capturedReqID)
	})

	t.Run("without request ID in metadata", func(t *testing.T) {
		stream := &mockServerStream{ctx: context.Background()}
		err := interceptor("srv", stream, nil, func(srv interface{}, ss grpc.ServerStream) error {
			return nil
		})
		assert.NoError(t, err)
	})
}

func TestStreamingGrpcServer_GetManifest_ChainIDFromMetadata(t *testing.T) {
	log := zap.NewNop()
	ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
	ctx = metadata.NewIncomingContext(ctx, metadata.New(map[string]string{
		"x-nft-contract": "0xContract",
		"x-nft-token-id": "1",
		"x-nft-chain-id": "137",
	}))
	srv := &streamingGrpcServer{
		nftVerifier: &grpcMockNFTChecker{owns: false},
		log:         log,
	}
	resp, err := srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "c1"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestStreamingGrpcServer_GetManifest_InvalidChainID(t *testing.T) {
	log := zap.NewNop()
	ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
	ctx = metadata.NewIncomingContext(ctx, metadata.New(map[string]string{
		"x-nft-contract": "0xContract",
		"x-nft-chain-id": "not-a-number",
	}))
	srv := &streamingGrpcServer{
		nftVerifier: &grpcMockNFTChecker{owns: false},
		log:         log,
	}
	resp, err := srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "c1"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestUploadGrpcServer_InitUpload_ChunkCountOverflow(t *testing.T) {
	log := zap.NewNop()
	ctx := context.WithValue(context.Background(), grpcWalletKey, "0xWallet")
	svc := service.NewUploadService(nil, nil, "bucket")
	srv := &uploadGrpcServer{uploadSvc: svc, log: log}

	resp, err := srv.InitUpload(ctx, &uploadv1.InitUploadRequest{
		Filename:  fmt.Sprintf("test%d.mp4", 1),
		FileSize:  int64(1 << 62),
		ChunkSize: 1,
	})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

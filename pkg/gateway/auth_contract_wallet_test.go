package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v4"
	authv1 "github.com/rtcdance/streamgate/pkg/api/v1/auth"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const transportWalletSecret = "contract-wallet-transport-secret-at-least-32"
const transportContractWallet = "0x1234567890123456789012345678901234567890"

type transportWalletProof struct {
	fail  error
	calls atomic.Int64
}

func (v *transportWalletProof) VerifySignature(context.Context, string, string, string) (bool, error) {
	return false, signature.ErrWalletSignature
}
func (v *transportWalletProof) VerifyWalletProof(_ context.Context, chain int64, _, address, message, encoded string) (*signature.AccountProof, error) {
	v.calls.Add(1)
	if v.fail != nil {
		return nil, v.fail
	}
	decoded, err := signature.DecodeWalletSignature(encoded)
	if err != nil {
		return nil, err
	}
	return &signature.AccountProof{Address: common.HexToAddress(address), ChainID: chain, Kind: signature.AccountContract, Signature: decoded,
		Digest:   crypto.Keccak256Hash([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message))),
		CodeHash: common.HexToHash("0x01"), BlockNumber: 17, BlockHash: common.HexToHash("0x02"), ObservedAt: time.Now().UTC()}, nil
}

func transportWalletFixture(t *testing.T) (*service.AuthService, *transportWalletProof, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	epochs, err := storage.NewRedisSessionStore(client)
	require.NoError(t, err)
	proofs, err := storage.NewRedisWalletSessionStore(client)
	require.NoError(t, err)
	v := &transportWalletProof{}
	s := service.NewAuthService(transportWalletSecret, nil, service.WithSessionStore(epochs), service.WithWalletSessionStore(proofs), service.WithSignatureVerifier(v), service.WithWalletDefaultChainID(31337))
	router := gin.New()
	router.POST("/challenge", handleAuthChallenge(&config.Config{}, s))
	router.POST("/login", handleAuthLogin(s))
	router.POST("/refresh", handleAuthRefresh(s))
	policy := middleware.JWTAuthConfig{Secret: transportWalletSecret, Issuer: service.AccessTokenIssuer, Audience: service.AccessTokenAudience, SessionValidator: s, RestoreEpochValidator: s}
	router.GET("/protected", middleware.JWTAuthMiddleware(policy, zap.NewNop()), func(c *gin.Context) { c.Status(http.StatusOK) })
	return s, v, router
}

func transportWalletToken(t *testing.T, s *service.AuthService) (string, *service.Claims) {
	t.Helper()
	challenge, err := s.GenerateWalletChallenge(t.Context(), transportContractWallet, 31337)
	require.NoError(t, err)
	token, err := s.AuthenticateWithWallet(t.Context(), transportContractWallet, challenge.ID, "0x0102", 31337)
	require.NoError(t, err)
	claims, err := s.ParseToken(token)
	require.NoError(t, err)
	return token, claims
}

func walletProtectedHTTP(router *gin.Engine, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/protected", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestContractWalletTransport_HTTPAndGRPCMetadataAndLeeway(t *testing.T) {
	s, v, router := transportWalletFixture(t)
	token, claims := transportWalletToken(t, s)
	require.Equal(t, http.StatusOK, walletProtectedHTTP(router, token).Code)
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+token))
	_, err := verifyGRPCJWT(ctx, "/streaming.v1.StreamingService/GetManifest", transportWalletSecret, s, s)
	require.NoError(t, err)
	before := v.calls.Load()
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-8 * time.Second))
	claims.IssuedAt = jwt.NewNumericDate(claims.ExpiresAt.Add(-time.Minute))
	claims.NotBefore = claims.IssuedAt
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(transportWalletSecret))
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, walletProtectedHTTP(router, expired).Code, "CW must not inherit the legacy 30s leeway")
	ctx = metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+expired))
	_, err = verifyGRPCJWT(ctx, "/streaming.v1.StreamingService/GetManifest", transportWalletSecret, s, s)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(30 * time.Second))
	claims.IssuedAt = jwt.NewNumericDate(time.Now())
	claims.NotBefore = claims.IssuedAt
	claims.AccountKind = ""
	claims.WalletProofID = ""
	claims.WalletSessionExpiresAt = 0
	claims.ChainID = 0
	missing, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(transportWalletSecret))
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, walletProtectedHTTP(router, missing).Code)
	ctx = metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+missing))
	_, err = verifyGRPCJWT(ctx, "/streaming.v1.StreamingService/GetManifest", transportWalletSecret, s, s)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Equal(t, before, v.calls.Load(), "protected requests must not introduce a per-request ERC-1271 RPC")
}

func TestContractWalletTransport_AuthBodyAndStableErrors(t *testing.T) {
	s, v, router := transportWalletFixture(t)
	challenge, err := s.GenerateWalletChallenge(t.Context(), transportContractWallet, 31337)
	require.NoError(t, err)
	payload := map[string]any{"address": transportContractWallet, "chain_id": 31337, "challenge_id": challenge.ID, "signature": "0x0102", "padding": strings.Repeat("x", 21*1024)}
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Zero(t, v.calls.Load())
	delete(payload, "padding")
	for _, tt := range []struct {
		err  error
		http int
		code string
	}{
		{signature.ErrWalletDependency, 503, "AUTH_UNAVAILABLE"}, {signature.ErrWalletUnsupported, 422, "WALLET_UNSUPPORTED"}, {signature.ErrWalletSignature, 401, ErrUnauthorized}, {signature.ErrWalletFormat, 400, ErrInvalidRequest},
	} {
		v.fail = tt.err
		encoded, err = json.Marshal(payload)
		require.NoError(t, err)
		req = httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, tt.http, w.Code)
		require.Contains(t, w.Body.String(), tt.code)
		require.NotContains(t, w.Body.String(), "0x0102")
	}
}

func TestContractWalletTransport_ExplicitSIWEAndNormalizedSubject(t *testing.T) {
	_, _, router := transportWalletFixture(t)
	body := `{"address":"` + transportContractWallet + `","chain_id":31337,"sign_type":"siwe"}`
	req := httptest.NewRequest(http.MethodPost, "/challenge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		ChallengeID string `json:"challenge_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotEmpty(t, response.ChallengeID)
	body = `{"address":"` + transportContractWallet + `","chain_id":31337,"challenge_id":"` + response.ChallengeID + `","signature":"0x0102"}`
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"account_kind":"erc1271"`)
	require.Contains(t, w.Body.String(), transportContractWallet)
}

func TestContractWalletTransport_GRPCUsesProfileAndStoredChallenge(t *testing.T) {
	s, v, _ := transportWalletFixture(t)
	server := &authGrpcServer{authSvc: s}
	nonce, err := server.GetNonce(t.Context(), &authv1.GetNonceRequest{WalletAddress: transportContractWallet, ChainType: "ethereum"})
	require.NoError(t, err)
	require.Contains(t, nonce.Message, "Chain ID: 31337")
	login, err := server.VerifySignature(t.Context(), &authv1.VerifySignatureRequest{WalletAddress: transportContractWallet, Nonce: nonce.Nonce, Signature: "0x0102"})
	require.NoError(t, err)
	require.True(t, login.Valid)
	claims, err := s.ParseToken(login.AccessToken)
	require.NoError(t, err)
	require.Equal(t, int64(31337), claims.ChainID)
	require.Equal(t, signature.AccountContract, claims.AccountKind)
	refreshed, err := server.RefreshToken(t.Context(), &authv1.RefreshTokenRequest{RefreshToken: login.AccessToken})
	require.NoError(t, err)
	require.True(t, refreshed.Success)
	renewed, err := s.ParseToken(refreshed.AccessToken)
	require.NoError(t, err)
	require.Equal(t, claims.WalletProofID, renewed.WalletProofID)
	v.fail = signature.ErrWalletDependency
	_, err = server.RefreshToken(t.Context(), &authv1.RefreshTokenRequest{RefreshToken: refreshed.AccessToken})
	require.Equal(t, codes.Unavailable, status.Code(err))
}

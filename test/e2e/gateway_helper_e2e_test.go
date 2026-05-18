package e2e_test

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
	"streamgate/pkg/core/config"
	"streamgate/pkg/gateway"
	"streamgate/pkg/middleware"
	"streamgate/pkg/service"
	"streamgate/pkg/storage"
	"streamgate/pkg/web3"
)

// mockNFTChecker implements middleware.NFTOwnershipChecker for E2E tests.
type mockNFTChecker struct {
	verifyResult bool
	verifyErr    error
	balance      *big.Int
	balanceErr   error
}

func (m *mockNFTChecker) VerifyNFTOwnership(ctx context.Context, chainID int64, contract, tokenID, owner string) (bool, error) {
	return m.verifyResult, m.verifyErr
}

func (m *mockNFTChecker) GetNFTBalance(ctx context.Context, chainID int64, contract, owner string) (*big.Int, error) {
	return m.balance, m.balanceErr
}

func (m *mockNFTChecker) VerifyNFTOwnershipAutoDetect(ctx context.Context, contract, tokenID, owner string) (bool, error) {
	return m.verifyResult, m.verifyErr
}

func (m *mockNFTChecker) VerifyNFTCollectionAutoDetect(ctx context.Context, contract, owner string) (bool, error) {
	if m.balanceErr != nil {
		return false, m.balanceErr
	}
	return m.balance != nil && m.balance.Sign() > 0, nil
}

// mockSegmentStorage implements service.SegmentStorage for E2E tests.
type mockSegmentStorage struct {
	objects map[string][]byte
}

func newMockSegmentStorage() *mockSegmentStorage {
	return &mockSegmentStorage{objects: make(map[string][]byte)}
}

func (s *mockSegmentStorage) Upload(ctx context.Context, bucket, objectName string, data []byte) error {
	s.objects[bucket+"/"+objectName] = data
	return nil
}

func (s *mockSegmentStorage) UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error {
	return s.Upload(ctx, bucket, objectName, data)
}

func (s *mockSegmentStorage) UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error {
	data, _ := io.ReadAll(reader)
	return s.Upload(ctx, bucket, objectName, data)
}

func (s *mockSegmentStorage) Download(ctx context.Context, bucket, objectName string) ([]byte, error) {
	if data, ok := s.objects[bucket+"/"+objectName]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("not found: %s/%s", bucket, objectName)
}

func (s *mockSegmentStorage) Delete(ctx context.Context, bucket, objectName string) error {
	delete(s.objects, bucket+"/"+objectName)
	return nil
}

func (s *mockSegmentStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	for _, key := range keys {
		delete(s.objects, bucket+"/"+key)
	}
	return nil
}

func (s *mockSegmentStorage) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	var result []string
	for key := range s.objects {
		if len(key) > len(bucket)+1 && key[:len(bucket)+1] == bucket+"/" {
			objName := key[len(bucket)+1:]
			if len(objName) >= len(prefix) && objName[:len(prefix)] == prefix {
				result = append(result, objName)
			}
		}
	}
	return result, nil
}

func (s *mockSegmentStorage) UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error {
	data, _ := io.ReadAll(reader)
	s.objects[bucket+"/"+objectName] = data
	return nil
}

func (s *mockSegmentStorage) DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *mockSegmentStorage) Exists(ctx context.Context, bucket, objectName string) (bool, error) {
	_, ok := s.objects[bucket+"/"+objectName]
	return ok, nil
}

func (s *mockSegmentStorage) CreateBucket(ctx context.Context, bucket string) error {
	return nil
}

func e2eTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = "test-secret-that-is-at-least-32-chars"
	cfg.Web3.ChainID = 11155111
	cfg.Server.Port = 0
	cfg.Database.Host = "localhost"
	cfg.Database.Port = 5432
	cfg.Redis.Host = "localhost"
	cfg.Redis.Port = 6379
	cfg.Storage.Endpoint = "localhost:9000"
	return cfg
}

func e2eNewAuthService() (*service.AuthService, *web3.SignatureVerifier) {
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	return service.NewAuthServiceWithDeps(
		"test-secret-that-is-at-least-32-chars",
		nil,
		verifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	), verifier
}

func e2eTestJWT(walletAddress string) string {
	claims := jwt.MapClaims{
		"wallet_address": walletAddress,
		"username":       walletAddress,
		"sub":            walletAddress,
		"jti":            fmt.Sprintf("test-jti-%d", time.Now().UnixNano()),
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString([]byte("test-secret-that-is-at-least-32-chars"))
	return s
}

func e2eSetupServer(t *testing.T, checker middleware.NFTOwnershipChecker, segStorage service.SegmentStorage) (*service.AuthService, *web3.SignatureVerifier, *httptest.Server) {
	t.Helper()
	authService, verifier := e2eNewAuthService()
	cfg := e2eTestConfig()

	opts := []gateway.RouterOption{
		gateway.WithAuthService(authService),
		gateway.WithChallengeStore(storage.NewMemoryChallengeStore()),
		gateway.WithSegmentStorage(segStorage),
		gateway.WithNFTVerifier(checker),
	}

	router, resources, err := gateway.SetupRouter(cfg, zap.NewNop(), opts...)
	if err != nil {
		t.Fatalf("SetupRouter failed: %v", err)
	}
	t.Cleanup(func() { _ = resources.Close() })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return authService, verifier, server
}

func e2eGenerateWallet(t *testing.T) (string, *web3.SignatureVerifier) {
	t.Helper()
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := verifier.GetAddressFromPrivateKey(privateKey)
	return wallet, verifier
}

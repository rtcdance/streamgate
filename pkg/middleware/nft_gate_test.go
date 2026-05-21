package middleware

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

const testContractAddr = "0x1234567890123456789012345678901234567890"

type mockNFTOwnershipChecker struct {
	verifyFn         func(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	balanceFn        func(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error)
	autoDetectFn     func(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	collectionAutoFn func(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (bool, error)
	getNFTInfoFn     func(ctx context.Context, chainID int64, contractAddress, tokenID string) (*NFTMetadata, error)
}

func (m *mockNFTOwnershipChecker) VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	return m.verifyFn(ctx, chainID, contractAddress, tokenID, ownerAddress)
}

func (m *mockNFTOwnershipChecker) GetNFTBalance(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error) {
	return m.balanceFn(ctx, chainID, contractAddress, ownerAddress)
}

func (m *mockNFTOwnershipChecker) VerifyNFTOwnershipAutoDetect(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	if m.autoDetectFn != nil {
		return m.autoDetectFn(ctx, chainID, contractAddress, tokenID, ownerAddress)
	}
	return m.verifyFn(ctx, chainID, contractAddress, tokenID, ownerAddress)
}

func (m *mockNFTOwnershipChecker) VerifyNFTCollectionAutoDetect(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (bool, error) {
	if m.collectionAutoFn != nil {
		return m.collectionAutoFn(ctx, chainID, contractAddress, ownerAddress)
	}
	bal, err := m.balanceFn(ctx, chainID, contractAddress, ownerAddress)
	if err != nil {
		return false, err
	}
	return bal != nil && bal.Sign() > 0, nil
}

func (m *mockNFTOwnershipChecker) GetNFTInfo(ctx context.Context, chainID int64, contractAddress, tokenID string) (*NFTMetadata, error) {
	if m.getNFTInfoFn != nil {
		return m.getNFTInfoFn(ctx, chainID, contractAddress, tokenID)
	}
	return nil, nil
}

type mockNFTAccessCache struct {
	entries map[string]NFTAccessEntry
}

func (m *mockNFTAccessCache) Get(_ context.Context, key string) (NFTAccessEntry, bool) {
	e, ok := m.entries[key]
	return e, ok
}

func (m *mockNFTAccessCache) Set(_ context.Context, key string, entry NFTAccessEntry) {
	m.entries[key] = entry
}

func (m *mockNFTAccessCache) Delete(_ context.Context, key string) {
	delete(m.entries, key)
}

func (m *mockNFTAccessCache) DeleteByPrefix(_ context.Context, prefix string) {
	for k := range m.entries {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(m.entries, k)
		}
	}
}

func setupNFTGateRouter(config *NFTGateConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	jwtConfig := JWTAuthConfig{Secret: "test-secret-that-is-at-least-32-chars"}
	router.Use(JWTAuthMiddleware(jwtConfig, zap.NewNop()))
	router.Use(NFTGateMiddleware(config, zap.NewNop()))

	router.GET("/stream/:id/manifest.m3u8", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"nft_verified": GetNFTVerified(c),
			"nft_contract": GetNFTContract(c),
		})
	})

	return router
}

func authRequestWithWallet(path, walletAddr string) *http.Request {
	token := generateTestJWT("test-secret-that-is-at-least-32-chars", walletAddr, time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", path, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestNFTGateMiddleware_Owner(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipChecker{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				return big.NewInt(3), nil
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "true")
}

func TestNFTGateMiddleware_NotOwner(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipChecker{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr, "0xNotOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestNFTGateMiddleware_MissingWalletAddress(t *testing.T) {
	config := NFTGateConfig{
		Verifier:       &mockNFTOwnershipChecker{},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := httptest.NewRequest("GET", "/stream/123/manifest.m3u8?contract="+testContractAddr, http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestNFTGateMiddleware_MissingContract(t *testing.T) {
	config := NFTGateConfig{
		Verifier:       &mockNFTOwnershipChecker{},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTGateMiddleware_InvalidContractAddress(t *testing.T) {
	config := NFTGateConfig{
		Verifier:       &mockNFTOwnershipChecker{},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract=0xABC", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "INVALID_CONTRACT", resp["code"])
}

func TestNFTGateMiddleware_VerifierError(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipChecker{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				return big.NewInt(0), context.DeadlineExceeded
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNFTGateMiddleware_CacheHit(t *testing.T) {
	verifierCalled := false
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipChecker{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				verifierCalled = true
				return big.NewInt(5), nil
			},
		},
		Cache: &mockNFTAccessCache{
			entries: map[string]NFTAccessEntry{
				"1:0xOwner:" + testContractAddr + ":__collection__": {
					HasNFT:  true,
					Balance: big.NewInt(5),
					Expires: time.Now().Add(time.Minute),
				},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, verifierCalled, "verifier should not be called when cache hits")
}

func TestNFTGateMiddleware_WithTokenID(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipChecker{
			verifyFn: func(ctx context.Context, chainID int64, contractAddress string, tokenID string, ownerAddress string) (bool, error) {
				assert.Equal(t, "42", tokenID)
				return true, nil
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr+"&token_id=42", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTGateMiddleware_ContractAddressAlias(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipChecker{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				return big.NewInt(1), nil
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract_address="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

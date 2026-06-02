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

type mockNFTOwnershipCheckerOld struct {
	verifyFn         func(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	balanceFn        func(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error)
	autoDetectFn     func(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	collectionAutoFn func(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (bool, error)
	getNFTInfoFn     func(ctx context.Context, chainID int64, contractAddress, tokenID string) (*NFTMetadata, error)
}

func (m *mockNFTOwnershipCheckerOld) VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	return m.verifyFn(ctx, chainID, contractAddress, tokenID, ownerAddress)
}

func (m *mockNFTOwnershipCheckerOld) GetNFTBalance(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error) {
	return m.balanceFn(ctx, chainID, contractAddress, ownerAddress)
}

func (m *mockNFTOwnershipCheckerOld) VerifyNFTOwnershipAutoDetect(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	if m.autoDetectFn != nil {
		return m.autoDetectFn(ctx, chainID, contractAddress, tokenID, ownerAddress)
	}
	return m.verifyFn(ctx, chainID, contractAddress, tokenID, ownerAddress)
}

func (m *mockNFTOwnershipCheckerOld) VerifyNFTCollectionAutoDetect(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (bool, error) {
	if m.collectionAutoFn != nil {
		return m.collectionAutoFn(ctx, chainID, contractAddress, ownerAddress)
	}
	bal, err := m.balanceFn(ctx, chainID, contractAddress, ownerAddress)
	if err != nil {
		return false, err
	}
	return bal != nil && bal.Sign() > 0, nil
}

func (m *mockNFTOwnershipCheckerOld) GetNFTInfo(ctx context.Context, chainID int64, contractAddress, tokenID string) (*NFTMetadata, error) {
	if m.getNFTInfoFn != nil {
		return m.getNFTInfoFn(ctx, chainID, contractAddress, tokenID)
	}
	return nil, nil
}

type mockNFTAccessCacheOld struct {
	entries map[string]NFTAccessEntry
}

func (m *mockNFTAccessCacheOld) Get(_ context.Context, key string) (NFTAccessEntry, bool) {
	e, ok := m.entries[key]
	return e, ok
}

func (m *mockNFTAccessCacheOld) Set(_ context.Context, key string, entry NFTAccessEntry) {
	m.entries[key] = entry
}

func (m *mockNFTAccessCacheOld) Delete(_ context.Context, key string) {
	delete(m.entries, key)
}

func (m *mockNFTAccessCacheOld) DeleteByPrefix(_ context.Context, prefix string) {
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
		Verifier: &mockNFTOwnershipCheckerOld{
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
		Verifier: &mockNFTOwnershipCheckerOld{
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
		Verifier:       &mockNFTOwnershipCheckerOld{},
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
		Verifier:       &mockNFTOwnershipCheckerOld{},
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
		Verifier:       &mockNFTOwnershipCheckerOld{},
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
		Verifier: &mockNFTOwnershipCheckerOld{
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
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				verifierCalled = true
				return big.NewInt(5), nil
			},
		},
		Cache: &mockNFTAccessCacheOld{
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
		Verifier: &mockNFTOwnershipCheckerOld{
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
		Verifier: &mockNFTOwnershipCheckerOld{
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

func TestNewBlockHashCache_DefaultTTL(t *testing.T) {
	bhc := NewBlockHashCache()
	assert.NotNil(t, bhc)
	assert.Equal(t, 5*time.Minute, bhc.ttl)
	assert.Equal(t, 2048, bhc.maxEntries)
}

func TestNewBlockHashCache_CustomTTL(t *testing.T) {
	bhc := NewBlockHashCache(10 * time.Minute)
	assert.Equal(t, 10*time.Minute, bhc.ttl)
}

func TestNewBlockHashCache_ZeroTTL(t *testing.T) {
	bhc := NewBlockHashCache(0)
	assert.Equal(t, 5*time.Minute, bhc.ttl)
}

func TestBlockHashCache_Get_Miss(t *testing.T) {
	bhc := NewBlockHashCache()
	_, ok := bhc.Get(12345)
	assert.False(t, ok)
}

func TestBlockHashCache_Set_Get(t *testing.T) {
	bhc := NewBlockHashCache()
	bhc.Set(100, "0xabc")
	hash, ok := bhc.Get(100)
	assert.True(t, ok)
	assert.Equal(t, "0xabc", hash)
}

func TestBlockHashCache_Get_Expired(t *testing.T) {
	bhc := NewBlockHashCache()
	bhc.entries[100] = blockHashEntry{hash: "0xexpired", expiresAt: time.Now().Add(-1 * time.Hour)}
	_, ok := bhc.Get(100)
	assert.False(t, ok)
}

func TestBlockHashCache_Set_Overwrite(t *testing.T) {
	bhc := NewBlockHashCache()
	bhc.Set(100, "0xfirst")
	bhc.Set(100, "0xsecond")
	hash, ok := bhc.Get(100)
	assert.True(t, ok)
	assert.Equal(t, "0xsecond", hash)
}

func TestBlockHashCache_Set_Eviction(t *testing.T) {
	bhc := NewBlockHashCache()
	bhc.maxEntries = 5
	for i := uint64(0); i < 10; i++ {
		bhc.Set(i, "0xhash")
	}
	assert.LessOrEqual(t, len(bhc.entries), bhc.maxEntries)
}

func TestGetNFTVerified_True(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("nft_verified", true)
	assert.True(t, GetNFTVerified(c))
}

func TestGetNFTVerified_False(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("nft_verified", false)
	assert.False(t, GetNFTVerified(c))
}

func TestGetNFTVerified_Missing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	assert.False(t, GetNFTVerified(c))
}

func TestGetNFTContract_Set(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("nft_contract", "0xContract123")
	assert.Equal(t, "0xContract123", GetNFTContract(c))
}

func TestGetNFTContract_Missing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	assert.Equal(t, "", GetNFTContract(c))
}

func TestNftCacheKey(t *testing.T) {
	key := nftCacheKey(1, "0xWallet", "0xContract", "42")
	assert.Equal(t, "1:0xWallet:0xContract:42", key)
}

func TestNftCacheKey_Collection(t *testing.T) {
	key := nftCacheKey(1, "0xWallet", "0xContract", "")
	assert.Equal(t, "1:0xWallet:0xContract:__collection__", key)
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		ok    bool
	}{
		{"1", 1, true},
		{"137", 137, true},
		{"0", 0, true},
		{"abc", 0, false},
		{"", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseInt64(tt.input)
			if tt.ok {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestBoolStr(t *testing.T) {
	assert.Equal(t, "true", boolStr(true))
	assert.Equal(t, "false", boolStr(false))
}

func TestNFTGateMiddleware_Disabled(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	config.Enabled.Store(false)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

type mockGatingRuleResolver struct {
	rules []GatingRule
	err   error
}

func (m *mockGatingRuleResolver) GetActiveRulesForContent(_ context.Context, _ string) ([]GatingRule, error) {
	return m.rules, m.err
}

type mockBlockProver struct {
	header *BlockHeaderInfo
	err    error
}

func (m *mockBlockProver) HeaderByNumber(_ context.Context, _ *big.Int) (*BlockHeaderInfo, error) {
	return m.header, m.err
}

type mockAuditLogger struct {
	logs []string
}

func (m *mockAuditLogger) Log(_ context.Context, event, _, _, _ string, _ bool, _, _ string) {
	m.logs = append(m.logs, event)
}

func (m *mockAuditLogger) Close() error { return nil }

func TestNFTGateMiddleware_WithRuleResolver(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(1), nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "", ChainID: 1, Standard: "erc721", MinBalance: 1},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/content-1/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTGateMiddleware_RuleResolverFails(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(1), nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			err: context.DeadlineExceeded,
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/content-1/manifest.m3u8?contract="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTGateMiddleware_NoGatingRules_SkipsVerification(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/content-1/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTGateMiddleware_NoGatingRules_WithContractParam_SkipsVerification(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/content-1/manifest.m3u8?contract="+testContractAddr+"&chain_id=1", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTGateMiddleware_RuleResolverWithAutoDetect(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			autoDetectFn: func(_ context.Context, _ int64, _ string, _ string, _ string) (bool, error) {
				return true, nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "42", ChainID: 1, Standard: "erc1155", MinBalance: 0},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/content-1/manifest.m3u8?contract="+testContractAddr+"&token_id=42", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTGateMiddleware_FallbackGatingRules(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			verifyFn: func(_ context.Context, _ int64, _ string, tokenID string, _ string) (bool, error) {
				if tokenID == "fallback-token" {
					return true, nil
				}
				return false, nil
			},
			balanceFn: func(_ context.Context, _ int64, contract string, _ string) (*big.Int, error) {
				if contract == "0xAlternate7890123456789012345678901234567890123" {
					return big.NewInt(1), nil
				}
				return big.NewInt(0), nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "42", ChainID: 1, MinBalance: 0},
				{ContractAddress: "0xAlternate7890123456789012345678901234567890123", TokenID: "", ChainID: 1, MinBalance: 0},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/content-1/manifest.m3u8?contract="+testContractAddr+"&token_id=42", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTGateMiddleware_NftGateDeniedWithMarketplaceURL(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			verifyFn: func(_ context.Context, _ int64, _ string, _ string, _ string) (bool, error) {
				return false, nil
			},
		},
		DefaultChainID: 1,
		MarketplaceURL: "https://market.example.com/{contract}/{token_id}",
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr+"&token_id=42", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "marketplace_url")
	assert.Contains(t, w.Body.String(), "https://market.example.com/")
}

func TestNFTGateMiddleware_AuditLoggerOnDenied(t *testing.T) {
	audit := &mockAuditLogger{}
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		AuditLogger:    audit,
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, audit.logs, "nft.gate_denied")
}

func TestNFTGateMiddleware_AuditLoggerOnPassed(t *testing.T) {
	audit := &mockAuditLogger{}
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(1), nil
			},
		},
		AuditLogger:    audit,
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, audit.logs, "nft.gate_passed")
}

func TestResolveOwnership_Wrapper(t *testing.T) {
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(2), nil
			},
		},
		DefaultChainID: 1,
	}
	hasNFT, err := resolveOwnership(context.Background(), &config, zap.NewNop(), "1:0xW:0xC:__collection__", 1, testContractAddr, "", "0xW", 0)
	assert.NoError(t, err)
	assert.True(t, hasNFT)
}

func TestSetCachedEntry_WithBlockProver(t *testing.T) {
	cache := &mockNFTAccessCacheOld{entries: map[string]NFTAccessEntry{}}
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{},
		BlockProver: &mockBlockProver{
			header: &BlockHeaderInfo{Number: 100, Hash: "0xblockhash"},
		},
		Cache:          cache,
		CacheTTL:       60 * time.Second,
		DefaultChainID: 1,
	}
	setCachedEntry(context.Background(), &config, zap.NewNop(), "test-key", true, big.NewInt(1))

	entry, ok := cache.entries["test-key"]
	assert.True(t, ok)
	assert.True(t, entry.HasNFT)
	assert.Equal(t, uint64(100), entry.BlockNumber)
	assert.Equal(t, "0xblockhash", entry.BlockHash)
}

func TestSetCachedEntry_WithBlockProverError(t *testing.T) {
	cache := &mockNFTAccessCacheOld{entries: map[string]NFTAccessEntry{}}
	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{},
		BlockProver: &mockBlockProver{
			err: context.DeadlineExceeded,
		},
		Cache:          cache,
		CacheTTL:       60 * time.Second,
		DefaultChainID: 1,
	}
	setCachedEntry(context.Background(), &config, zap.NewNop(), "test-key-err", true, big.NewInt(1))

	entry, ok := cache.entries["test-key-err"]
	assert.True(t, ok)
	assert.True(t, entry.HasNFT)
	assert.Equal(t, uint64(0), entry.BlockNumber)
}

func TestSetCachedEntry_NegativeResultShorterTTL(t *testing.T) {
	cache := &mockNFTAccessCacheOld{entries: map[string]NFTAccessEntry{}}
	config := NFTGateConfig{
		Verifier:       &mockNFTOwnershipCheckerOld{},
		Cache:          cache,
		CacheTTL:       60 * time.Second,
		DefaultChainID: 1,
	}
	setCachedEntry(context.Background(), &config, zap.NewNop(), "neg-key", false, big.NewInt(0))

	entry, ok := cache.entries["neg-key"]
	assert.True(t, ok)
	assert.False(t, entry.HasNFT)
	assert.True(t, entry.Expires.After(time.Now().Add(30*time.Second)))
}

func TestNFTGateMiddleware_CircuitBreakerOpen(t *testing.T) {
	cb := NewCircuitBreaker("test-nft-cb", CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          1 * time.Second,
	}, zap.NewNop())
	_ = cb.Execute(context.Background(), func() error {
		return context.DeadlineExceeded
	})

	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		CircuitBreaker: cb,
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNFTGateMiddleware_CacheWithBlockHashReorg(t *testing.T) {
	cache := &mockNFTAccessCacheOld{entries: map[string]NFTAccessEntry{}}
	blockVerifyCache := NewBlockHashCache(5 * time.Minute)
	blockVerifyCache.Set(100, "0xoriginalhash")

	cacheKey := "1:0xOwner:" + testContractAddr + ":__collection__"
	cache.entries[cacheKey] = NFTAccessEntry{
		HasNFT:      true,
		Balance:     big.NewInt(1),
		BlockNumber: 100,
		BlockHash:   "0xoriginalhash",
		Expires:     time.Now().Add(time.Minute),
	}

	config := NFTGateConfig{
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(1), nil
			},
		},
		BlockProver: &mockBlockProver{
			header: &BlockHeaderInfo{Number: 100, Hash: "0xdifferenthash"},
		},
		Cache:            cache,
		BlockVerifyCache: blockVerifyCache,
		CacheTTL:         60 * time.Second,
		ReorgTTL:         5 * time.Second,
		DefaultChainID:   1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr, "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestParseNFTParams_ChainIDOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/stream/abc/manifest.m3u8?contract=0xABC&chain_id=137&token_id=99", http.NoBody)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}

	config := NFTGateConfig{DefaultChainID: 1}
	contract, chainID, tokenID, contentID := parseNFTParams(c, &config)

	assert.Equal(t, "0xABC", contract)
	assert.Equal(t, int64(137), chainID)
	assert.Equal(t, "99", tokenID)
	assert.Equal(t, "abc", contentID)
}

func TestParseNFTParams_InvalidChainID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/stream/abc/manifest.m3u8?contract=0xABC&chain_id=invalid", http.NoBody)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}

	config := NFTGateConfig{DefaultChainID: 1}
	_, chainID, _, _ := parseNFTParams(c, &config)

	assert.Equal(t, int64(1), chainID)
}

func TestNFTGateMiddleware_EmptyBearerToken(t *testing.T) {
	config := NFTGateConfig{
		Verifier:       &mockNFTOwnershipCheckerOld{},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := httptest.NewRequest("GET", "/stream/123/manifest.m3u8?contract="+testContractAddr, http.NoBody)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// metricValue reads one counter/histogram series from the DEFAULT registry by
// metric name and exact label set. Used instead of prometheus testutil, which
// would pull in a new module (kylelemons/godebug) — this project adds no deps.
func metricValue(t *testing.T, name string, wantLabels map[string]string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		var total float64
		matched := false
		for _, m := range fam.GetMetric() {
			if !labelsMatch(m, wantLabels) {
				continue
			}
			matched = true
			switch fam.GetType() {
			case dto.MetricType_COUNTER:
				total += m.GetCounter().GetValue()
			case dto.MetricType_HISTOGRAM:
				total += float64(m.GetHistogram().GetSampleCount())
			case dto.MetricType_GAUGE:
				total += m.GetGauge().GetValue()
			}
		}
		if matched {
			return total
		}
	}
	return 0
}

func labelsMatch(m *dto.Metric, want map[string]string) bool {
	got := map[string]string{}
	for _, lp := range m.GetLabel() {
		got[lp.GetName()] = lp.GetValue()
	}
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

const testContractAddr = "0x1234567890123456789012345678901234567890"

type mockNFTOwnershipCheckerOld struct {
	verifyFn         func(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	balanceFn        func(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error)
	autoDetectFn     func(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error)
	collectionAutoFn func(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (bool, error)
	getNFTInfoFn     func(ctx context.Context, chainID int64, contractAddress, tokenID string) (*NFTMetadata, error)
	// quantityFn, when set, is the authoritative quantity source for the gate.
	quantityFn func(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (*big.Int, error)
}

// GetNFTOwnershipQuantity mirrors what a real verifier would report: a quantity
// for the subject. When quantityFn is not set it derives one from the legacy
// fields so existing cases keep their meaning (collection → balanceFn,
// specific token → 0/1 from verifyFn).
func (m *mockNFTOwnershipCheckerOld) GetNFTOwnershipQuantity(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string, block *big.Int) (*big.Int, error) {
	if m.quantityFn != nil {
		return m.quantityFn(ctx, chainID, contractAddress, tokenID, ownerAddress)
	}
	if tokenID == "" {
		if m.balanceFn == nil {
			return big.NewInt(0), nil
		}
		return m.balanceFn(ctx, chainID, contractAddress, ownerAddress)
	}
	fn := m.verifyFn
	if fn == nil {
		fn = m.autoDetectFn
	}
	if fn == nil {
		return big.NewInt(0), nil
	}
	ok, err := fn(ctx, chainID, contractAddress, tokenID, ownerAddress)
	if err != nil {
		return nil, err
	}
	if ok {
		return big.NewInt(1), nil
	}
	return big.NewInt(0), nil
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
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				return big.NewInt(3), nil
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

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "true")
}

func TestNFTGateMiddleware_NotOwner(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				return big.NewInt(0), nil
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

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xNotOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestNFTGateMiddleware_MissingWalletAddress(t *testing.T) {
	config := NFTGateConfig{
		BlockProver:    &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier:       &mockNFTOwnershipCheckerOld{},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := httptest.NewRequest("GET", "/stream/123/manifest.m3u8", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// No server-side gating rules + GatingMode "closed" (default) must DENY —
// an unlisted content is protected by default (TRANSITION_PLAN A8).
func TestNFTGateMiddleware_NoRules_FailsClosed(t *testing.T) {
	config := NFTGateConfig{
		BlockProver:    &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier:       &mockNFTOwnershipCheckerOld{},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "GATING_REQUIRED", resp["code"])
}

// An explicitly public deployment opts out via GatingMode "open".
func TestNFTGateMiddleware_NoRules_OpenModeAllows(t *testing.T) {
	config := NFTGateConfig{
		BlockProver:    &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier:       &mockNFTOwnershipCheckerOld{},
		GatingMode:     "open",
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// A server rule with a malformed contract address must still be rejected.
func TestNFTGateMiddleware_InvalidContractAddress(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier:    &mockNFTOwnershipCheckerOld{},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: "not-a-hex-address", TokenID: "", ChainID: 1, Standard: "erc721"},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "INVALID_CONTRACT", resp["code"])
}

func TestNFTGateMiddleware_VerifierError(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				return big.NewInt(0), context.DeadlineExceeded
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

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// An unverifiable rule set is a service failure, not a denial: answering
	// 403 here would disguise an RPC outage as "you hold no NFT".
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestNFTGateMiddleware_CacheHit(t *testing.T) {
	verifierCalled := false
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				verifierCalled = true
				return big.NewInt(5), nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "", ChainID: 1, Standard: "erc721", MinBalance: 1},
			},
		},
		Cache: &mockNFTAccessCacheOld{
			entries: map[string]NFTAccessEntry{
				"1:0xOwner:" + testContractAddr + ":__collection__:erc721": {
					HasNFT:     true,
					Balance:    big.NewInt(5),
					Expires:    time.Now().Add(time.Minute),
					ObservedAt: time.Now(),
				},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, verifierCalled, "verifier should not be called when cache hits")
}

func TestNFTGateMiddleware_WithTokenID(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			verifyFn: func(ctx context.Context, chainID int64, contractAddress string, tokenID string, ownerAddress string) (bool, error) {
				assert.Equal(t, "42", tokenID)
				return true, nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "42", ChainID: 1, Standard: "erc721", MinBalance: 1},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// Caller-supplied contract/chain_id/token_id query params must be IGNORED —
// the authorization subject comes only from server-side rules (A9). With no
// server rules and GatingMode closed, the request is denied even though the
// caller names a contract they own.
func TestNFTGateMiddleware_ContractQueryParams_Ignored(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (*big.Int, error) {
				return big.NewInt(1), nil
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8?contract="+testContractAddr+"&contract_address="+testContractAddr+"&token_id=42&chain_id=1", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
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
	_, ok := bhc.Get(1, 12345)
	assert.False(t, ok)
}

func TestBlockHashCache_Set_Get(t *testing.T) {
	bhc := NewBlockHashCache()
	bhc.Set(1, 100, "0xabc")
	hash, ok := bhc.Get(1, 100)
	assert.True(t, ok)
	assert.Equal(t, "0xabc", hash)
}

// Two chains routinely have a block at the same height with different hashes.
// The cache is keyed by (chainID, height), so an entry recorded on one chain
// must not answer for another.
func TestBlockHashCache_IsScopedToChain(t *testing.T) {
	bhc := NewBlockHashCache()
	bhc.Set(1, 100, "0xchain-one")

	hash, ok := bhc.Get(1, 100)
	assert.True(t, ok)
	assert.Equal(t, "0xchain-one", hash)

	_, ok = bhc.Get(2, 100)
	assert.False(t, ok, "chain 2 must not see chain 1's hash at the same height")

	// And the other chain's own hash is tracked independently.
	bhc.Set(2, 100, "0xchain-two")
	hash, ok = bhc.Get(2, 100)
	assert.True(t, ok)
	assert.Equal(t, "0xchain-two", hash)
	hash, ok = bhc.Get(1, 100)
	assert.True(t, ok)
	assert.Equal(t, "0xchain-one", hash, "chain 1's entry must be untouched")
}

func TestBlockHashCache_Get_Expired(t *testing.T) {
	bhc := NewBlockHashCache()
	bhc.entries[blockHashKey{chainID: 1, number: 100}] = blockHashEntry{hash: "0xexpired", expiresAt: time.Now().Add(-1 * time.Hour)}
	_, ok := bhc.Get(1, 100)
	assert.False(t, ok)
}

func TestBlockHashCache_Set_Overwrite(t *testing.T) {
	bhc := NewBlockHashCache()
	bhc.Set(1, 100, "0xfirst")
	bhc.Set(1, 100, "0xsecond")
	hash, ok := bhc.Get(1, 100)
	assert.True(t, ok)
	assert.Equal(t, "0xsecond", hash)
}

func TestBlockHashCache_Set_Eviction(t *testing.T) {
	bhc := NewBlockHashCache()
	bhc.maxEntries = 5
	for i := uint64(0); i < 10; i++ {
		bhc.Set(1, i, "0xhash")
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
	key := nftCacheKey(1, "0xWallet", "0xContract", "42", "erc721")
	assert.Equal(t, "1:0xWallet:0xContract:42:erc721", key)
}

func TestNftCacheKey_Collection(t *testing.T) {
	key := nftCacheKey(1, "0xWallet", "0xContract", "", "")
	assert.Equal(t, "1:0xWallet:0xContract:__collection__:__auto__", key)
}

// The standard is part of the key: the same subject read as ERC-721 vs
// ERC-1155 is different evidence and must not collide.
func TestNftCacheKey_StandardIsolates(t *testing.T) {
	erc721 := nftCacheKey(1, "0xWallet", "0xContract", "42", "erc721")
	erc1155 := nftCacheKey(1, "0xWallet", "0xContract", "42", "erc1155")
	assert.NotEqual(t, erc721, erc1155)
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
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	config.Enabled.Store(false)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Fail closed: a disabled gate must NOT become a bypass (A8).
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

type mockGatingRuleResolver struct {
	rules []GatingRule
	err   error
}

func (m *mockGatingRuleResolver) GetActiveRulesForContent(_ context.Context, _ string) ([]GatingRule, error) {
	return m.rules, m.err
}

// mockVersionedRuleResolver additionally reports a rule version, satisfying
// GatingRuleVersionedResolver so the gate can label its decisions with it.
type mockVersionedRuleResolver struct {
	mockGatingRuleResolver
	version int64
	verr    error
}

func (m *mockVersionedRuleResolver) RuleVersion(_ context.Context, _ string) (int64, error) {
	return m.version, m.verr
}

type mockBlockProver struct {
	header    *BlockHeaderInfo
	err       error
	byChain   map[int64]*BlockHeaderInfo
	gotChain  int64
	gotNumber *big.Int
	gotTag    string
}

func (m *mockBlockProver) HeaderByNumber(_ context.Context, chainID int64, number *big.Int) (*BlockHeaderInfo, error) {
	m.gotChain, m.gotNumber = chainID, number
	if m.byChain != nil {
		header, ok := m.byChain[chainID]
		if !ok {
			return nil, errors.New("no header for chain")
		}
		return header, nil
	}
	return m.header, m.err
}

// HeaderByTag resolves the anchor block. It returns the same header as
// HeaderByNumber so the unchanged-block check passes.
func (m *mockBlockProver) HeaderByTag(_ context.Context, chainID int64, tag string) (*BlockHeaderInfo, error) {
	m.gotChain, m.gotTag = chainID, tag
	if m.byChain != nil {
		header, ok := m.byChain[chainID]
		if !ok {
			return nil, errors.New("no header for chain")
		}
		return header, nil
	}
	return m.header, m.err
}

type mockAuditLogger struct {
	logs []string
}

func (m *mockAuditLogger) Log(_ context.Context, event, _, _, _ string, _ bool, _, _ string) {
	m.logs = append(m.logs, event)
}

func (m *mockAuditLogger) Close() error { return nil }

// TestAcceptance_GateDecisionMetricRecorded drives the real NFTGateMiddleware
// with fakes and asserts GateDecisionTotal{decision,reason,rule_version}
// increments on both an allow and a deny, each with the right labels.
func TestAcceptance_GateDecisionMetricRecorded(t *testing.T) {
	gateDecisionAllowBefore := metricValue(t, "streamgate_gate_decisions_total",
		map[string]string{"decision": "allow", "reason": "ok", "rule_version": "7"})
	denyBefore := metricValue(t, "streamgate_gate_decisions_total",
		map[string]string{"decision": "deny", "reason": "insufficient_balance", "rule_version": "7"})

	// Allow: the wallet holds a sufficient balance against the server-side rule.
	allowCfg := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(3), nil
			},
		},
		RuleResolver: &mockVersionedRuleResolver{
			mockGatingRuleResolver: mockGatingRuleResolver{rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "", ChainID: 1, Standard: "erc721", MinBalance: 1},
			}},
			version: 7,
		},
		DefaultChainID: 1,
	}
	allowRouter := setupNFTGateRouter(&allowCfg)
	w := httptest.NewRecorder()
	allowRouter.ServeHTTP(w, authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner"))
	require.Equal(t, http.StatusOK, w.Code)

	// Deny: same rules, zero balance.
	denyCfg := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		RuleResolver: &mockVersionedRuleResolver{
			mockGatingRuleResolver: mockGatingRuleResolver{rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "", ChainID: 1, Standard: "erc721", MinBalance: 1},
			}},
			version: 7,
		},
		DefaultChainID: 1,
	}
	denyRouter := setupNFTGateRouter(&denyCfg)
	w = httptest.NewRecorder()
	denyRouter.ServeHTTP(w, authRequestWithWallet("/stream/123/manifest.m3u8", "0xNotOwner"))
	require.Equal(t, http.StatusForbidden, w.Code)

	gateDecisionAllowAfter := metricValue(t, "streamgate_gate_decisions_total",
		map[string]string{"decision": "allow", "reason": "ok", "rule_version": "7"})
	denyAfter := metricValue(t, "streamgate_gate_decisions_total",
		map[string]string{"decision": "deny", "reason": "insufficient_balance", "rule_version": "7"})

	assert.Equal(t, gateDecisionAllowBefore+1, gateDecisionAllowAfter,
		"an allow must increment GateDecisionTotal{decision=allow,reason=ok,rule_version=7}")
	assert.Equal(t, denyBefore+1, denyAfter,
		"a deny must increment GateDecisionTotal{decision=deny,reason=insufficient_balance,rule_version=7}")
}

func TestNFTGateMiddleware_WithRuleResolver(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
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
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
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

	req := authRequestWithWallet("/stream/content-1/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Fail closed: an unresolvable rule set is a server error, not a bypass (A8).
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "GATING_RULES_ERROR", resp["code"])
}

func TestNFTGateMiddleware_NoGatingRules_DeniedByDefault(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
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

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Caller-supplied contract/chain_id params must not restore access when no
// server rules exist (A8+A9).
func TestNFTGateMiddleware_NoGatingRules_WithContractParam_Denied(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
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

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestNFTGateMiddleware_RuleResolverWithAutoDetect(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
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
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			verifyFn: func(_ context.Context, _ int64, _ string, tokenID string, _ string) (bool, error) {
				if tokenID == "fallback-token" {
					return true, nil
				}
				return false, nil
			},
			balanceFn: func(_ context.Context, _ int64, contract string, _ string) (*big.Int, error) {
				if contract == "0x9999999999999999999999999999999999999999" {
					return big.NewInt(1), nil
				}
				return big.NewInt(0), nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "42", ChainID: 1, MinBalance: 0},
				{ContractAddress: "0x9999999999999999999999999999999999999999", TokenID: "", ChainID: 1, MinBalance: 0},
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
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			verifyFn: func(_ context.Context, _ int64, _ string, _ string, _ string) (bool, error) {
				return false, nil
			},
		},
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "42", ChainID: 1, Standard: "erc721", MinBalance: 1},
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
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		AuditLogger: audit,
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "", ChainID: 1, Standard: "erc721", MinBalance: 1},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, audit.logs, "nft.gate_denied")
}

func TestNFTGateMiddleware_AuditLoggerOnPassed(t *testing.T) {
	audit := &mockAuditLogger{}
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(1), nil
			},
		},
		AuditLogger: audit,
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "", ChainID: 1, Standard: "erc721", MinBalance: 1},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, audit.logs, "nft.gate_passed")
}

func TestResolveQuantity_Wrapper(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(2), nil
			},
		},
		DefaultChainID: 1,
	}
	quantity, _, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "1:0xW:0xC:__collection__:__auto__", 1, testContractAddr, "", "0xW")
	assert.NoError(t, err)
	assert.Equal(t, 0, quantity.Cmp(big.NewInt(2)))
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
	setCachedEntry(context.Background(), &config, zap.NewNop(), "test-key", time.Now(), &BlockHeaderInfo{Number: 100, Hash: "0xblockhash"}, big.NewInt(1))

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
	setCachedEntry(context.Background(), &config, zap.NewNop(), "test-key-err", time.Now(), nil, big.NewInt(1))

	entry, ok := cache.entries["test-key-err"]
	assert.True(t, ok)
	assert.True(t, entry.HasNFT)
	assert.Equal(t, uint64(0), entry.BlockNumber)
}

func TestSetCachedEntry_NegativeResultShorterTTL(t *testing.T) {
	cache := &mockNFTAccessCacheOld{entries: map[string]NFTAccessEntry{}}
	config := NFTGateConfig{
		BlockProver:    &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier:       &mockNFTOwnershipCheckerOld{},
		Cache:          cache,
		CacheTTL:       60 * time.Second,
		DefaultChainID: 1,
	}
	setCachedEntry(context.Background(), &config, zap.NewNop(), "neg-key", time.Now(), &BlockHeaderInfo{Number: 7, Hash: "0xneg"}, big.NewInt(0))

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
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
				return big.NewInt(0), nil
			},
		},
		CircuitBreaker: cb,
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "", ChainID: 1, Standard: "erc721", MinBalance: 1},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// A tripped circuit breaker means the chain cannot be read: that is a
	// service failure (503), not a denial.
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestNFTGateMiddleware_CacheWithBlockHashReorg(t *testing.T) {
	cache := &mockNFTAccessCacheOld{entries: map[string]NFTAccessEntry{}}
	blockVerifyCache := NewBlockHashCache(5 * time.Minute)
	blockVerifyCache.Set(1, 100, "0xoriginalhash")

	cacheKey := "1:0xOwner:" + testContractAddr + ":__collection__:erc721"
	cache.entries[cacheKey] = NFTAccessEntry{
		HasNFT:      true,
		Balance:     big.NewInt(1),
		BlockNumber: 100,
		BlockHash:   "0xoriginalhash",
		Expires:     time.Now().Add(time.Minute),
		ObservedAt:  time.Now(),
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
		RuleResolver: &mockGatingRuleResolver{
			rules: []GatingRule{
				{ContractAddress: testContractAddr, TokenID: "", ChainID: 1, Standard: "erc721", MinBalance: 1},
			},
		},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	req := authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// Caller-supplied contract / chain_id / token_id query parameters must never
// influence the authorization subject: it comes only from server-side gating
// rules. Exercised end-to-end — the internal param parser was removed when the
// gate moved to a rule-only subject, so this asserts the observable behaviour
// instead of a helper.
func TestNFTGateMiddleware_QueryParamsDoNotChangeSubject(t *testing.T) {
	config := NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			quantityFn: func(_ context.Context, chainID int64, contract, tokenID, _ string) (*big.Int, error) {
				assert.Equal(t, int64(1), chainID, "gate must use the rule's chain, not ?chain_id")
				assert.Equal(t, testContractAddr, contract, "gate must use the rule's contract, not ?contract")
				assert.Empty(t, tokenID, "gate must use the rule's token id, not ?token_id")
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

	req := authRequestWithWallet("/stream/abc/manifest.m3u8?contract=0xABC&chain_id=137&token_id=99", "0xOwner")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTGateMiddleware_EmptyBearerToken(t *testing.T) {
	config := NFTGateConfig{
		BlockProver:    &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xq"}},
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

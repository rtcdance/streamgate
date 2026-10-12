package middleware

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	gateContractA = "0x1111111111111111111111111111111111111111"
	gateContractB = "0x2222222222222222222222222222222222222222"
	gateWallet    = "0x3333333333333333333333333333333333333333"
)

// switchableRuleResolver lets a test change the active rule set between
// requests, which is how the cache-ordering regression is reproduced.
type switchableRuleResolver struct {
	mu    sync.Mutex
	rules []GatingRule
	err   error
}

func (r *switchableRuleResolver) set(rules ...GatingRule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules = rules
}

func (r *switchableRuleResolver) GetActiveRulesForContent(_ context.Context, _ string) ([]GatingRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	return append([]GatingRule(nil), r.rules...), nil
}

// perContentRuleResolver returns a different rule set per content, so one test
// can drive two different thresholds over the SAME subject concurrently.
type perContentRuleResolver struct {
	mu        sync.Mutex
	byContent map[string][]GatingRule
}

func (r *perContentRuleResolver) GetActiveRulesForContent(_ context.Context, contentID string) ([]GatingRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]GatingRule(nil), r.byContent[contentID]...), nil
}

// countingQuantityVerifier counts how many chain reads the gate performed, so
// tests can prove the cache is actually used (and that it is not the cache that
// decided the verdict).
type countingQuantityVerifier struct {
	calls atomic.Int64
	fn    func(contract, tokenID string) (*big.Int, error)
}

func (v *countingQuantityVerifier) quantity(contract, tokenID string) (*big.Int, error) {
	v.calls.Add(1)
	return v.fn(contract, tokenID)
}

func (v *countingQuantityVerifier) GetNFTOwnershipQuantity(_ context.Context, _ int64, contract, tokenID, _ string) (*big.Int, error) {
	return v.quantity(contract, tokenID)
}

func (v *countingQuantityVerifier) VerifyNFTOwnership(_ context.Context, _ int64, contract, tokenID, _ string) (bool, error) {
	q, err := v.quantity(contract, tokenID)
	return q != nil && q.Sign() > 0, err
}

func (v *countingQuantityVerifier) VerifyNFTOwnershipAutoDetect(ctx context.Context, chainID int64, contract, tokenID, owner string) (bool, error) {
	return v.VerifyNFTOwnership(ctx, chainID, contract, tokenID, owner)
}

func (v *countingQuantityVerifier) VerifyNFTCollectionAutoDetect(ctx context.Context, chainID int64, contract, owner string) (bool, error) {
	return v.VerifyNFTOwnership(ctx, chainID, contract, "", owner)
}

func (v *countingQuantityVerifier) GetNFTBalance(ctx context.Context, chainID int64, contract, owner string) (*big.Int, error) {
	return v.GetNFTOwnershipQuantity(ctx, chainID, contract, "", owner)
}

func (v *countingQuantityVerifier) GetNFTInfo(context.Context, int64, string, string) (*NFTMetadata, error) {
	return nil, nil
}

// syncAccessCache is a race-free NFTAccessCache for concurrent tests. The
// shared mockNFTAccessCacheOld is map-backed and only safe for serial use.
type syncAccessCache struct {
	mu      sync.Mutex
	entries map[string]NFTAccessEntry
}

func newQuantityCache() *syncAccessCache {
	return &syncAccessCache{entries: map[string]NFTAccessEntry{}}
}

func (c *syncAccessCache) Get(_ context.Context, key string) (NFTAccessEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	return entry, ok
}

func (c *syncAccessCache) Set(_ context.Context, key string, entry NFTAccessEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry
}

func (c *syncAccessCache) Delete(_ context.Context, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func (c *syncAccessCache) DeleteByPrefix(_ context.Context, prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
		}
	}
}

// doGate runs one request through the middleware and returns the status.
func doGate(t *testing.T, router *gin.Engine, path string) int {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, authRequestWithWallet(path, gateWallet))
	return w.Code
}

// --- the P0 regression: cache ordering must not change the verdict ---

// Wallet holds exactly 1. A min=1 request must be allowed, and a later min=2
// request over the same subject must NOT inherit that allow.
func TestNFTGate_CacheOrderLowThenHigh_Denies(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(1), nil }}
	resolver := &switchableRuleResolver{}
	config := NFTGateConfig{
		Verifier:       verifier,
		RuleResolver:   resolver,
		Cache:          newQuantityCache(),
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	resolver.set(GatingRule{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 1})
	require.Equal(t, http.StatusOK, doGate(t, router, "/stream/c1/manifest.m3u8"))

	resolver.set(GatingRule{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 2})
	assert.Equal(t, http.StatusForbidden, doGate(t, router, "/stream/c1/manifest.m3u8"),
		"a min=1 allow must not satisfy a min=2 rule over the same subject")
}

// The reverse order: a min=2 denial must not deny a later min=1 rule.
func TestNFTGate_CacheOrderHighThenLow_Allows(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(1), nil }}
	resolver := &switchableRuleResolver{}
	config := NFTGateConfig{
		Verifier:       verifier,
		RuleResolver:   resolver,
		Cache:          newQuantityCache(),
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	resolver.set(GatingRule{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 2})
	require.Equal(t, http.StatusForbidden, doGate(t, router, "/stream/c1/manifest.m3u8"))

	resolver.set(GatingRule{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 1})
	assert.Equal(t, http.StatusOK, doGate(t, router, "/stream/c1/manifest.m3u8"),
		"a min=2 denial must not deny a min=1 rule over the same subject")
}

// The cached value is the BALANCE, so a warm cache and a cold read agree.
func TestNFTGate_WarmAndColdAgree(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(3), nil }}
	resolver := &switchableRuleResolver{}
	config := NFTGateConfig{
		Verifier:       verifier,
		RuleResolver:   resolver,
		Cache:          newQuantityCache(),
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	resolver.set(GatingRule{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 3})

	require.Equal(t, http.StatusOK, doGate(t, router, "/stream/c1/manifest.m3u8")) // cold
	assert.Equal(t, http.StatusOK, doGate(t, router, "/stream/c1/manifest.m3u8"))  // warm
	assert.Equal(t, int64(1), verifier.calls.Load(), "second request must be served from cache")

	// The cache must hold the real balance, not a normalised "1".
	key := nftCacheKey(1, gateWallet, gateContractA, "", "erc721")
	entry, ok := config.Cache.Get(context.Background(), key)
	require.True(t, ok)
	require.NotNil(t, entry.Balance)
	assert.Equal(t, 0, entry.Balance.Cmp(big.NewInt(3)), "cache must store the balance evidence")
}

// Concurrent requests with different thresholds over the same subject must each
// get their own verdict.
func TestNFTGate_ConcurrentDifferentThresholds(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) {
		time.Sleep(20 * time.Millisecond) // widen the singleflight window
		return big.NewInt(1), nil
	}}
	resolver := &perContentRuleResolver{byContent: map[string][]GatingRule{
		"c1": {{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 1}},
		"c2": {{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 2}},
	}}
	config := NFTGateConfig{
		Verifier:       verifier,
		RuleResolver:   resolver,
		Cache:          newQuantityCache(),
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	const rounds = 20
	var wg sync.WaitGroup
	lowResults := make([]int, rounds)
	highResults := make([]int, rounds)

	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			lowResults[i] = doGate(t, router, "/stream/c1/manifest.m3u8")
		}(i)
		go func(i int) {
			defer wg.Done()
			highResults[i] = doGate(t, router, "/stream/c2/manifest.m3u8")
		}(i)
	}
	wg.Wait()

	for i := 0; i < rounds; i++ {
		assert.Equal(t, http.StatusOK, lowResults[i], "min=1 must allow (round %d)", i)
		assert.Equal(t, http.StatusForbidden, highResults[i], "min=2 must deny (round %d)", i)
	}
}

// --- quantity comparison semantics ---

func TestNFTGate_MinEqualsBalance_Allows(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(2), nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 2},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusOK, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

func TestNFTGate_MinGreaterThanBalance_Denies(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(1), nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 2},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusForbidden, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

// A specific ERC-721 token is 0/1: owning a different token must not help.
func TestNFTGate_ERC721SpecificToken_ZeroAndOne(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, tokenID string) (*big.Int, error) {
		if tokenID == "42" {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	}}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, TokenID: "42", ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusOK, doGate(t, router, "/stream/c1/manifest.m3u8"))

	config2 := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, TokenID: "43", ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		DefaultChainID: 1,
	}
	router2 := setupNFTGateRouter(&config2)
	assert.Equal(t, http.StatusForbidden, doGate(t, router2, "/stream/c1/manifest.m3u8"))
}

// ERC-1155 quantity must be compared as a number, not collapsed to a boolean.
func TestNFTGate_ERC1155Quantity_OneRejected_TwoAllowed(t *testing.T) {
	for _, tc := range []struct {
		held int64
		want int
	}{
		{held: 1, want: http.StatusForbidden},
		{held: 2, want: http.StatusOK},
		{held: 3, want: http.StatusOK},
	} {
		held := tc.held
		verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(held), nil }}
		config := NFTGateConfig{
			Verifier: verifier,
			RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
				{ContractAddress: gateContractA, TokenID: "7", ChainID: 1, Standard: "erc1155", MinBalance: 2},
			}},
			DefaultChainID: 1,
		}
		router := setupNFTGateRouter(&config)
		assert.Equal(t, tc.want, doGate(t, router, "/stream/c1/manifest.m3u8"), "held=%d", held)
	}
}

// Auto-detect (empty standard) must run the same comparison.
func TestNFTGate_AutoDetectStandard_SameComparison(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(1), nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, TokenID: "7", ChainID: 1, Standard: "", MinBalance: 2},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusForbidden, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

// --- failure modes must never be permissive ---

func TestNFTGate_NilQuantityIsRejected(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return nil, nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusServiceUnavailable, doGate(t, router, "/stream/c1/manifest.m3u8"),
		"a nil quantity is an unusable answer, not ownership")
}

func TestNFTGate_NegativeQuantityIsRejected(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(-1), nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusServiceUnavailable, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

func TestNFTGate_RPCErrorIsServiceError(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) {
		return nil, context.DeadlineExceeded
	}}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusServiceUnavailable, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

// --- OR semantics over the rule set ---

func TestNFTGate_ORRules_OneTrueAllows(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(contract, _ string) (*big.Int, error) {
		if contract == gateContractB {
			return big.NewInt(5), nil
		}
		return big.NewInt(0), nil
	}}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 1},
			{ContractAddress: gateContractB, ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusOK, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

func TestNFTGate_ORRules_AllFalseDenies(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(0), nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 1},
			{ContractAddress: gateContractB, ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusForbidden, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

// No rule verified true and at least one could not be verified: a service
// error, not a denial.
func TestNFTGate_ORRules_UnverifiableWithNoTrueIsServiceError(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(contract, _ string) (*big.Int, error) {
		if contract == gateContractB {
			return nil, context.DeadlineExceeded
		}
		return big.NewInt(0), nil
	}}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 1},
			{ContractAddress: gateContractB, ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusServiceUnavailable, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

// --- malformed rules are 400, never silently laxer ---

func TestNFTGate_NegativeMinBalanceIsRejected(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(1), nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: -1},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusBadRequest, doGate(t, router, "/stream/c1/manifest.m3u8"))
	assert.Equal(t, int64(0), verifier.calls.Load(), "a malformed rule must not reach the chain")
}

func TestNFTGate_ERC1155CollectionRuleWithoutTokenIDIsRejected(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(1), nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc1155", MinBalance: 2},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusBadRequest, doGate(t, router, "/stream/c1/manifest.m3u8"),
		"an ERC-1155 collection rule cannot express min>1 and must be rejected")
}

func TestNFTGate_NonNumericTokenIDIsRejected(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(1), nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, TokenID: "not-a-number", ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusBadRequest, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

// The default min_balance is 1: a rule that omits it still requires ownership.
func TestNFTGate_DefaultMinBalanceIsOne(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(0), nil }}
	config := NFTGateConfig{
		Verifier: verifier,
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: gateContractA, ChainID: 1, Standard: "erc721", MinBalance: 0},
		}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)
	assert.Equal(t, http.StatusForbidden, doGate(t, router, "/stream/c1/manifest.m3u8"))
}

// Different standards over the same subject must not share cache evidence.
func TestNFTGate_StandardIsolatesCache(t *testing.T) {
	verifier := &countingQuantityVerifier{fn: func(_, _ string) (*big.Int, error) { return big.NewInt(1), nil }}
	resolver := &switchableRuleResolver{}
	config := NFTGateConfig{
		Verifier:       verifier,
		RuleResolver:   resolver,
		Cache:          newQuantityCache(),
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	resolver.set(GatingRule{ContractAddress: gateContractA, TokenID: "7", ChainID: 1, Standard: "erc721", MinBalance: 1})
	require.Equal(t, http.StatusOK, doGate(t, router, "/stream/c1/manifest.m3u8"))

	resolver.set(GatingRule{ContractAddress: gateContractA, TokenID: "7", ChainID: 1, Standard: "erc1155", MinBalance: 1})
	require.Equal(t, http.StatusOK, doGate(t, router, "/stream/c1/manifest.m3u8"))

	assert.Equal(t, int64(2), verifier.calls.Load(), "the two standards must not share a cache entry")
}

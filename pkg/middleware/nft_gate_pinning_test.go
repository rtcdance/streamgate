package middleware

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// recordingBlockVerifier captures the block the gate pins the read to.
type recordingBlockVerifier struct {
	quantity *big.Int
	err      error
	gotBlock *big.Int
}

func (v *recordingBlockVerifier) VerifyNFTOwnership(context.Context, int64, string, string, string) (bool, error) {
	return false, nil
}
func (v *recordingBlockVerifier) GetNFTBalance(context.Context, int64, string, string) (*big.Int, error) {
	return nil, nil
}
func (v *recordingBlockVerifier) VerifyNFTOwnershipAutoDetect(context.Context, int64, string, string, string) (bool, error) {
	return false, nil
}
func (v *recordingBlockVerifier) VerifyNFTCollectionAutoDetect(context.Context, int64, string, string) (bool, error) {
	return false, nil
}
func (v *recordingBlockVerifier) GetNFTInfo(context.Context, int64, string, string) (*NFTMetadata, error) {
	return nil, nil
}
func (v *recordingBlockVerifier) GetNFTOwnershipQuantity(_ context.Context, _ int64, _, _, _ string, block *big.Int) (*big.Int, error) {
	v.gotBlock = block
	return v.quantity, v.err
}

// The gate must hand the verifier the resolved anchor block, not a tag and not
// "latest": every decision query in an authorization reads one chain state.
func TestResolveQuantity_PinsTheAnchorBlock(t *testing.T) {
	verifier := &recordingBlockVerifier{quantity: big.NewInt(3)}
	prover := &mockBlockProver{header: &BlockHeaderInfo{Number: 4242, Hash: "0xanchor"}}
	config := NFTGateConfig{
		Verifier:       verifier,
		BlockProver:    prover,
		DefaultChainID: 1,
		BlockTag:       "safe",
	}

	quantity, anchor, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "k", 1, "0xC", "", "0xW")

	require.NoError(t, err)
	assert.Equal(t, 0, quantity.Cmp(big.NewInt(3)))
	require.NotNil(t, verifier.gotBlock, "the verifier must receive a block, never a bare tag")
	assert.Equal(t, int64(4242), verifier.gotBlock.Int64(), "the verifier must be pinned to the anchor block")
	assert.Equal(t, "safe", prover.gotTag, "the anchor must be resolved from the configured finality tag")
	require.NotNil(t, anchor, "the evidence must name the block it was read at")
	assert.Equal(t, uint64(4242), anchor.Number)
	assert.Equal(t, "0xanchor", anchor.Hash)
}

// Without a block prover there is no anchor, so there is no authorization:
// falling back to "latest" would silently reintroduce mixed-height evidence.
func TestResolveQuantity_WithoutProverFailsClosed(t *testing.T) {
	verifier := &recordingBlockVerifier{quantity: big.NewInt(1)}
	config := NFTGateConfig{Verifier: verifier, DefaultChainID: 1}

	_, _, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "k", 1, "0xC", "", "0xW")

	require.Error(t, err)
	assert.Nil(t, verifier.gotBlock, "no chain read may happen without a resolved anchor")
}

// If the anchor's hash moves while the read is in flight, the evidence was
// assembled from a chain state that no longer exists, so the request is refused.
func TestResolveQuantity_BlockChangedDuringReadIsRejected(t *testing.T) {
	prover := &shiftingBlockProver{
		tagHeader:    &BlockHeaderInfo{Number: 100, Hash: "0xbefore"},
		numberHeader: &BlockHeaderInfo{Number: 100, Hash: "0xafter"},
	}
	verifier := &recordingBlockVerifier{quantity: big.NewInt(5)}
	config := NFTGateConfig{Verifier: verifier, BlockProver: prover, DefaultChainID: 1}

	_, _, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "k", 1, "0xC", "", "0xW")

	require.Error(t, err, "a block that changed under the read must not authorize")
	assert.True(t, config.reorgActive.Load(), "the gate must record that it saw a reorg")
}

// A tag that the prover cannot resolve is a failure, not a silent latest read.
func TestResolveQuantity_UnresolvableAnchorFailsClosed(t *testing.T) {
	verifier := &recordingBlockVerifier{quantity: big.NewInt(1)}
	config := NFTGateConfig{
		Verifier:       verifier,
		BlockProver:    &mockBlockProver{err: assert.AnError},
		DefaultChainID: 1,
	}

	_, _, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "k", 1, "0xC", "", "0xW")

	require.Error(t, err)
	assert.Nil(t, verifier.gotBlock)
}

// shiftingBlockProver returns one header for the tag lookup and a different one
// for the by-number re-check, simulating a reorg during the read.
type shiftingBlockProver struct {
	tagHeader    *BlockHeaderInfo
	numberHeader *BlockHeaderInfo
}

func (p *shiftingBlockProver) HeaderByTag(_ context.Context, _ int64, _ string) (*BlockHeaderInfo, error) {
	return p.tagHeader, nil
}

func (p *shiftingBlockProver) HeaderByNumber(_ context.Context, _ int64, _ *big.Int) (*BlockHeaderInfo, error) {
	return p.numberHeader, nil
}

// The anchor is always resolved at a finality tag, never at "latest": reading
// the head would let a block that is not yet final authorize a request.
func TestResolveQuantity_NeverAnchorsAtLatest(t *testing.T) {
	cases := []struct {
		configured string
		want       string
	}{
		{configured: "safe", want: "safe"},
		{configured: "finalized", want: "finalized"},
		// An unset tag must default to safe, not to latest.
		{configured: "", want: "safe"},
		{configured: "   ", want: "safe"},
	}
	for _, tc := range cases {
		t.Run("tag="+tc.configured, func(t *testing.T) {
			prover := &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xh"}}
			config := NFTGateConfig{
				Verifier:       &recordingBlockVerifier{quantity: big.NewInt(1)},
				BlockProver:    prover,
				DefaultChainID: 1,
				BlockTag:       web3.BlockTag(tc.configured),
			}

			_, _, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "k", 1, "0xC", "", "0xW")

			require.NoError(t, err)
			assert.Equal(t, tc.want, prover.gotTag)
			assert.NotEqual(t, "latest", prover.gotTag, "the anchor must never be the chain head")
		})
	}
}

// A reorg event drops that chain's cached evidence and marks the gate, so the
// next request re-reads at a fresh anchor instead of trusting pre-reorg
// evidence. Another chain's evidence must survive.
func TestInvalidateNFTEvidence_DropsOnlyThatChainsEvidence(t *testing.T) {
	cache := &mockNFTAccessCacheOld{entries: map[string]NFTAccessEntry{
		"1:0xA:0xC1:__collection__:erc721": {Balance: big.NewInt(1), Expires: time.Now().Add(time.Minute), ObservedAt: time.Now()},
		"2:0xA:0xC2:__collection__:erc721": {Balance: big.NewInt(1), Expires: time.Now().Add(time.Minute), ObservedAt: time.Now()},
	}}
	config := &NFTGateConfig{Cache: cache}

	InvalidateNFTEvidence(config, 1)

	assert.True(t, config.reorgActive.Load(), "the gate must record the reorg")
	assert.NotZero(t, config.reorgDetectedAt.Load())
	assert.NotContains(t, cache.entries, "1:0xA:0xC1:__collection__:erc721",
		"the reorged chain's evidence must be dropped")
	assert.Contains(t, cache.entries, "2:0xA:0xC2:__collection__:erc721",
		"another chain's evidence must survive")
}

// Invalidation must not panic on a partially-wired config.
func TestInvalidateNFTEvidence_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() { InvalidateNFTEvidence(nil, 1) })

	config := &NFTGateConfig{}
	assert.NotPanics(t, func() { InvalidateNFTEvidence(config, 1) })
	assert.True(t, config.reorgActive.Load())
}

// An allow must be traceable to a specific chain and block: the response says
// which chain state authorized it, instead of leaving the caller to guess.
func TestNFTGateMiddleware_AllowReportsBlockEvidence(t *testing.T) {
	config := NFTGateConfig{
		Verifier:    &recordingBlockVerifier{quantity: big.NewInt(1)},
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 777, Hash: "0xevidence"}},
		RuleResolver: &mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: testContractAddr, TokenID: "", ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		Cache:          &mockNFTAccessCacheOld{entries: map[string]NFTAccessEntry{}},
		DefaultChainID: 1,
	}
	router := setupNFTGateRouter(&config)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, authRequestWithWallet("/stream/123/manifest.m3u8", "0xOwner"))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "777", w.Header().Get("X-NFT-Block-Number"))
	assert.Equal(t, "0xevidence", w.Header().Get("X-NFT-Block-Hash"))
	assert.Equal(t, "1", w.Header().Get("X-NFT-Chain-Id"))
}

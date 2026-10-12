package middleware

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// Block evidence is anchored to a chain. A block hash recorded on chain 1 at
// height N must never be used to validate an entry on chain 2 at the same
// height: the two chains have different hashes there, and comparing them would
// look exactly like a reorg — invalidating perfectly good evidence.
func TestCachedQuantity_BlockHashEvidenceIsChainScoped(t *testing.T) {
	blockVerifyCache := NewBlockHashCache(5 * time.Minute)
	// Chain 1's block 100 carries a different hash.
	blockVerifyCache.Set(1, 100, "0xchain-one-hash")

	prover := &mockBlockProver{byChain: map[int64]*BlockHeaderInfo{
		2: {Number: 100, Hash: "0xchain-two-hash"},
	}}
	config := &NFTGateConfig{
		BlockProver:      prover,
		BlockVerifyCache: blockVerifyCache,
	}
	entry := NFTAccessEntry{
		Balance:     big.NewInt(3),
		BlockNumber: 100,
		BlockHash:   "0xchain-two-hash",
		Expires:     time.Now().Add(time.Minute),
	}

	quantity, usable := cachedQuantity(config, zap.NewNop(), "2:0xOwner:0xC:__collection__:erc721", 2, entry)

	assert.True(t, usable, "chain 2's own evidence at height 100 must be usable")
	assert.Equal(t, 0, quantity.Cmp(big.NewInt(3)))
	assert.Equal(t, int64(2), prover.gotChain, "the prover must be asked about chain 2, not the default chain")
	assert.False(t, config.reorgActive.Load(), "a different chain's hash is not a reorg")
}

// Within a single chain, a hash change at the same height IS a reorg and must
// still invalidate the evidence.
func TestCachedQuantity_ReorgStillDetectedWithinChain(t *testing.T) {
	config := &NFTGateConfig{
		BlockProver:      &mockBlockProver{byChain: map[int64]*BlockHeaderInfo{1: {Number: 100, Hash: "0xnew"}}},
		BlockVerifyCache: NewBlockHashCache(5 * time.Minute),
	}
	entry := NFTAccessEntry{
		Balance:     big.NewInt(1),
		BlockNumber: 100,
		BlockHash:   "0xold",
		Expires:     time.Now().Add(time.Minute),
	}

	_, usable := cachedQuantity(config, zap.NewNop(), "1:0xOwner:0xC:__collection__:erc721", 1, entry)

	assert.False(t, usable, "a hash change at the same height on the same chain is a reorg")
	assert.True(t, config.reorgActive.Load())
	assert.Equal(t, int64(1), config.BlockProver.(*mockBlockProver).gotChain)
}

// A hit on the same (chain, height) pair with a matching hash is served from
// the block-verify cache without touching the prover.
func TestCachedQuantity_SameChainCacheHitSkipsProver(t *testing.T) {
	blockVerifyCache := NewBlockHashCache(5 * time.Minute)
	blockVerifyCache.Set(2, 100, "0xsame")

	prover := &mockBlockProver{err: assert.AnError}
	config := &NFTGateConfig{BlockProver: prover, BlockVerifyCache: blockVerifyCache}
	entry := NFTAccessEntry{
		Balance:     big.NewInt(7),
		BlockNumber: 100,
		BlockHash:   "0xsame",
		Expires:     time.Now().Add(time.Minute),
	}

	quantity, usable := cachedQuantity(config, zap.NewNop(), "2:0xOwner:0xC:__collection__:erc721", 2, entry)

	assert.True(t, usable)
	assert.Equal(t, 0, quantity.Cmp(big.NewInt(7)))
	assert.Equal(t, int64(0), prover.gotChain, "the prover must not have been consulted")
}

package middleware

import (
	"context"
	"math/big"
	"testing"

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

	quantity, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "k", 1, "0xC", "", "0xW")

	require.NoError(t, err)
	assert.Equal(t, 0, quantity.Cmp(big.NewInt(3)))
	require.NotNil(t, verifier.gotBlock, "the verifier must receive a block, never a bare tag")
	assert.Equal(t, int64(4242), verifier.gotBlock.Int64(), "the verifier must be pinned to the anchor block")
	assert.Equal(t, "safe", prover.gotTag, "the anchor must be resolved from the configured finality tag")
}

// Without a block prover there is no anchor, so there is no authorization:
// falling back to "latest" would silently reintroduce mixed-height evidence.
func TestResolveQuantity_WithoutProverFailsClosed(t *testing.T) {
	verifier := &recordingBlockVerifier{quantity: big.NewInt(1)}
	config := NFTGateConfig{Verifier: verifier, DefaultChainID: 1}

	_, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "k", 1, "0xC", "", "0xW")

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

	_, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "k", 1, "0xC", "", "0xW")

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

	_, err := resolveQuantity(context.Background(), &config, zap.NewNop(), "k", 1, "0xC", "", "0xW")

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

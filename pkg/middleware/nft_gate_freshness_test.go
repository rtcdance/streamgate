package middleware

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// agingHeaderProver delays only the first HeaderByNumber call, which is the
// reorg check a cached entry goes through, so the entry crosses the freshness
// bound while it is being validated.
type agingHeaderProver struct {
	mu     sync.Mutex
	calls  int
	delay  time.Duration
	cached *BlockHeaderInfo
	fresh  *BlockHeaderInfo
}

func (p *agingHeaderProver) HeaderByNumber(_ context.Context, _ int64, number *big.Int) (*BlockHeaderInfo, error) {
	p.mu.Lock()
	p.calls++
	first := p.calls == 1
	p.mu.Unlock()
	if first {
		time.Sleep(p.delay)
	}
	if number.Uint64() == p.cached.Number {
		return p.cached, nil
	}
	return p.fresh, nil
}

func (p *agingHeaderProver) HeaderByTag(context.Context, int64, string) (*BlockHeaderInfo, error) {
	return p.fresh, nil
}

func TestResolveQuantity_EntryExpiringDuringReorgCheckIsReRead(t *testing.T) {
	bound := 200 * time.Millisecond
	prover := &agingHeaderProver{
		delay:  120 * time.Millisecond,
		cached: &BlockHeaderInfo{Number: 7, Hash: "0xcached"},
		fresh:  &BlockHeaderInfo{Number: 8, Hash: "0xfresh"},
	}
	verifier := &countingQuantityVerifier{fn: func(string, string) (*big.Int, error) { return big.NewInt(1), nil }}
	cache := newQuantityCache()
	cache.Set(context.Background(), "aging", NFTAccessEntry{
		HasNFT: true, Balance: big.NewInt(1), BlockNumber: 7, BlockHash: "0xcached",
		Expires: time.Now().Add(time.Minute), ObservedAt: time.Now().Add(-120 * time.Millisecond),
	})
	config := NFTGateConfig{
		Verifier: verifier, BlockProver: prover, Cache: cache, BlockVerifyCache: NewBlockHashCache(),
		DefaultChainID: 1, CacheTTL: time.Minute, MaxEvidenceAge: bound,
	}

	quantity, anchor, observedAt, err := resolveQuantityObserved(context.Background(), &config, zap.NewNop(), "aging", 1, "0xC", "", "0xW")

	require.NoError(t, err, "an entry that expired during its reorg check must be re-read, not refused")
	assert.Equal(t, 0, quantity.Cmp(big.NewInt(1)))
	assert.EqualValues(t, 1, verifier.calls.Load(), "the stale entry must be replaced by one fresh chain read")
	require.NotNil(t, anchor)
	assert.Equal(t, "0xfresh", anchor.Hash, "the decision must cite the fresh anchor, not the expired entry")
	assert.Less(t, time.Since(observedAt), prover.delay, "the fresh read's clock starts after the slow cache check, not at request start")
}

func TestResolveQuantity_FreshCachedEntryIsServedWithoutChainRead(t *testing.T) {
	prover := &agingHeaderProver{cached: &BlockHeaderInfo{Number: 7, Hash: "0xcached"}, fresh: &BlockHeaderInfo{Number: 8, Hash: "0xfresh"}}
	verifier := &countingQuantityVerifier{fn: func(string, string) (*big.Int, error) { return big.NewInt(1), nil }}
	cache := newQuantityCache()
	cache.Set(context.Background(), "fresh", NFTAccessEntry{
		HasNFT: true, Balance: big.NewInt(2), BlockNumber: 7, BlockHash: "0xcached",
		Expires: time.Now().Add(time.Minute), ObservedAt: time.Now(),
	})
	config := NFTGateConfig{
		Verifier: verifier, BlockProver: prover, Cache: cache, BlockVerifyCache: NewBlockHashCache(),
		DefaultChainID: 1, CacheTTL: time.Minute, MaxEvidenceAge: time.Second,
	}

	quantity, anchor, _, err := resolveQuantityObserved(context.Background(), &config, zap.NewNop(), "fresh", 1, "0xC", "", "0xW")

	require.NoError(t, err)
	assert.Equal(t, 0, quantity.Cmp(big.NewInt(2)))
	assert.EqualValues(t, 0, verifier.calls.Load(), "a fresh, unreorged entry needs no chain read")
	assert.Equal(t, "0xcached", anchor.Hash)
}

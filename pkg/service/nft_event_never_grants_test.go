package service

import (
	"context"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/cachetypes"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/rtcdance/streamgate/pkg/web3/event"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// recordingCacheBackend is an NFTService ownership cache that counts every
// write and delete, so a test can prove a Transfer event only ever deletes.
type recordingCacheBackend struct {
	setCount    int
	deleteCount int
}

func (r *recordingCacheBackend) Get(_ string) (interface{}, error) { return nil, nil }
func (r *recordingCacheBackend) Set(_ string, _ interface{}) error {
	r.setCount++
	return nil
}
func (r *recordingCacheBackend) SetWithExpiration(_ string, _ interface{}, _ time.Duration) error {
	r.setCount++
	return nil
}
func (r *recordingCacheBackend) Delete(_ string) error {
	r.deleteCount++
	return nil
}

var _ cachetypes.CacheBackend = (*recordingCacheBackend)(nil)

// countingNFTAccessCache is a middleware access cache that counts Set calls
// (grant-shaped writes) and records Delete calls (invalidations).
type countingNFTAccessCache struct {
	setCount      int
	deletedKeys   []string
	deletedPrefix []string
}

func (c *countingNFTAccessCache) Get(_ context.Context, _ string) (middleware.NFTAccessEntry, bool) {
	return middleware.NFTAccessEntry{}, false
}
func (c *countingNFTAccessCache) Set(_ context.Context, _ string, _ middleware.NFTAccessEntry) {
	c.setCount++
}
func (c *countingNFTAccessCache) Delete(_ context.Context, key string) {
	c.deletedKeys = append(c.deletedKeys, key)
}
func (c *countingNFTAccessCache) DeleteByPrefix(_ context.Context, prefix string) {
	c.deletedPrefix = append(c.deletedPrefix, prefix)
}

// TestAcceptance_NFTEventNeverGrants pins the SG-13 B invariant: a Transfer
// event reaches the pipeline as an INVALIDATION only. It must invalidate the
// ownership cache and the middleware access cache, and it must never write a
// grant (no cache Set of any kind, and specifically never a HasNFT=true entry).
func TestAcceptance_NFTEventNeverGrants(t *testing.T) {
	ownershipCache := &recordingCacheBackend{}
	nftSvc, err := NewNFTServiceWithCaller(nil, "", ownershipCache)
	require.NoError(t, err)

	middlewareCache := &countingNFTAccessCache{}
	const chainID int64 = 31337
	handler := NewNFTEventHandlerWithCache(nftSvc, middlewareCache, chainID, zap.NewNop())

	listener := web3.NewEventListener(nil, zap.NewNop())
	listener.On("Transfer", handler.HandleTransfer)
	listener.On("TransferSingle", handler.HandleTransferSingle)

	transfer := &event.IndexedEvent{
		ID:              "31337-0xblockhash-0xtxhash-0",
		ChainID:         chainID,
		EventType:       "Transfer",
		ContractAddress: "0xcontract",
		Topics: []string{
			transferTopic,
			"0x000000000000000000000000000000000000000000000000000000000000000a",
			"0x000000000000000000000000000000000000000000000000000000000000000b",
			"0x2a",
		},
		Decoded: map[string]interface{}{
			"from":    "0xfrom",
			"to":      "0xto",
			"tokenId": "42",
		},
	}
	require.NoError(t, listener.Emit(context.Background(), transfer))
	handler.FlushNow()

	assert.GreaterOrEqual(t, ownershipCache.deleteCount, 1,
		"Transfer must invalidate the ownership cache")
	assert.Contains(t, middlewareCache.deletedKeys, "31337:0xfrom:0xcontract:42",
		"Transfer must invalidate the sender's middleware cache entry")
	assert.Contains(t, middlewareCache.deletedKeys, "31337:0xto:0xcontract:42",
		"Transfer must invalidate the receiver's middleware cache entry")

	assert.Zero(t, ownershipCache.setCount,
		"a Transfer event must never write to the ownership cache")
	assert.Zero(t, middlewareCache.setCount,
		"a Transfer event must never write a grant entry to the middleware cache")
}

// TestClassifyTransferEvent pins the topic→event-type routing the production
// dispatch path relies on: indexed events arrive as "ContractEvent" and are
// retyped to the canonical Transfer names before the listener routes them.
func TestClassifyTransferEvent(t *testing.T) {
	tests := []struct {
		name    string
		event   *web3.IndexedEvent
		wantTyp string
	}{
		{
			name:    "transfer topic",
			event:   &web3.IndexedEvent{EventType: "ContractEvent", Topics: []string{transferTopic}},
			wantTyp: "Transfer",
		},
		{
			name:    "transfer single topic",
			event:   &web3.IndexedEvent{EventType: "ContractEvent", Topics: []string{transferSingleTopic}},
			wantTyp: "TransferSingle",
		},
		{
			name:    "transfer batch topic is not retyped",
			event:   &web3.IndexedEvent{EventType: "ContractEvent", Topics: []string{transferBatchTopic}},
			wantTyp: "ContractEvent",
		},
		{
			name:    "unknown topic untouched",
			event:   &web3.IndexedEvent{EventType: "ContractEvent", Topics: []string{"0xdead"}},
			wantTyp: "ContractEvent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyTransferEvent(tt.event)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantTyp, got.EventType)
		})
	}

	t.Run("nil event passes through", func(t *testing.T) {
		assert.Nil(t, classifyTransferEvent(nil))
	})
}

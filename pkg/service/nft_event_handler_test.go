package service

import (
	"context"
	"testing"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/web3/event"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockNFTAccessCache struct {
	deletedKeys     []string
	deletedPrefixes []string
}

func (m *mockNFTAccessCache) Get(_ context.Context, _ string) (middleware.NFTAccessEntry, bool) {
	return middleware.NFTAccessEntry{}, false
}
func (m *mockNFTAccessCache) Set(_ context.Context, _ string, _ middleware.NFTAccessEntry) {}
func (m *mockNFTAccessCache) Delete(_ context.Context, key string) {
	m.deletedKeys = append(m.deletedKeys, key)
}
func (m *mockNFTAccessCache) DeleteByPrefix(_ context.Context, prefix string) {
	m.deletedPrefixes = append(m.deletedPrefixes, prefix)
}

type mockNFTService struct {
	invalidatedContract string
	invalidatedTokenID  string
}

func (m *mockNFTService) InvalidateOwnershipCache(_ context.Context, contractAddress, tokenID string) {
	m.invalidatedContract = contractAddress
	m.invalidatedTokenID = tokenID
}

func (m *mockNFTService) setLogger(_ *zap.Logger) {}

func TestNewNFTEventHandler(t *testing.T) {
	t.Run("with nil logger", func(t *testing.T) {
		nftSvc := &NFTService{}
		h := NewNFTEventHandler(nftSvc, nil)
		require.NotNil(t, h)
		assert.Equal(t, nftSvc, h.nftService)
	})

	t.Run("with logger", func(t *testing.T) {
		nftSvc := &NFTService{}
		h := NewNFTEventHandler(nftSvc, zap.NewNop())
		require.NotNil(t, h)
	})
}

func TestNewNFTEventHandlerWithCache(t *testing.T) {
	nftSvc := &NFTService{}
	cache := &mockNFTAccessCache{}
	h := NewNFTEventHandlerWithCache(nftSvc, cache, 1, zap.NewNop())
	require.NotNil(t, h)
	assert.Equal(t, cache, h.middlewareCache)
	assert.Equal(t, int64(1), h.defaultChainID)
}

func TestNFTEventHandler_HandleTransfer(t *testing.T) {
	t.Run("with decoded tokenId", func(t *testing.T) {
		h := NewNFTEventHandler(&NFTService{}, zap.NewNop())
		evt := &event.IndexedEvent{
			ContractAddress: "0xcontract",
			Decoded: map[string]interface{}{
				"tokenId": "42",
			},
		}
		err := h.HandleTransfer(context.Background(), evt)
		require.NoError(t, err)
	})

	t.Run("with decoded tokenID (uppercase D)", func(t *testing.T) {
		h := NewNFTEventHandler(&NFTService{}, zap.NewNop())
		evt := &event.IndexedEvent{
			ContractAddress: "0xcontract",
			Decoded: map[string]interface{}{
				"tokenID": "99",
			},
		}
		err := h.HandleTransfer(context.Background(), evt)
		require.NoError(t, err)
	})

	t.Run("with topics fallback", func(t *testing.T) {
		h := NewNFTEventHandler(&NFTService{}, zap.NewNop())
		evt := &event.IndexedEvent{
			ContractAddress: "0xcontract",
			Topics:          []string{"topic0", "topic1", "topic2", "42"},
		}
		err := h.HandleTransfer(context.Background(), evt)
		require.NoError(t, err)
	})

	t.Run("no token ID found", func(t *testing.T) {
		h := NewNFTEventHandler(&NFTService{}, zap.NewNop())
		evt := &event.IndexedEvent{
			ContractAddress: "0xcontract",
		}
		err := h.HandleTransfer(context.Background(), evt)
		assert.NoError(t, err)
	})

	t.Run("with middleware cache invalidation", func(t *testing.T) {
		cache := &mockNFTAccessCache{}
		h := NewNFTEventHandlerWithCache(&NFTService{}, cache, 1, zap.NewNop())
		evt := &event.IndexedEvent{
			ContractAddress: "0xcontract",
			Decoded: map[string]interface{}{
				"tokenId": "42",
				"from":    "0xfrom",
				"to":      "0xto",
			},
		}
		err := h.HandleTransfer(context.Background(), evt)
		require.NoError(t, err)
		h.FlushNow()
		assert.Contains(t, cache.deletedKeys, "1:0xfrom:0xcontract:42")
		assert.Contains(t, cache.deletedKeys, "1:0xto:0xcontract:42")
		assert.Contains(t, cache.deletedPrefixes, "1::0xcontract:42")
	})
}

func TestNFTEventHandler_HandleTransferSingle(t *testing.T) {
	t.Run("with decoded id", func(t *testing.T) {
		cache := &mockNFTAccessCache{}
		h := NewNFTEventHandlerWithCache(&NFTService{}, cache, 1, zap.NewNop())
		evt := &event.IndexedEvent{
			ContractAddress: "0xcontract",
			Decoded: map[string]interface{}{
				"id":   "100",
				"from": "0xfrom",
				"to":   "0xto",
			},
		}
		err := h.HandleTransferSingle(context.Background(), evt)
		require.NoError(t, err)
		h.FlushNow()
		assert.Contains(t, cache.deletedKeys, "1:0xfrom:0xcontract:100")
		assert.Contains(t, cache.deletedKeys, "1:0xto:0xcontract:100")
	})

	t.Run("no token ID in decoded", func(t *testing.T) {
		h := NewNFTEventHandler(&NFTService{}, zap.NewNop())
		evt := &event.IndexedEvent{
			ContractAddress: "0xcontract",
		}
		err := h.HandleTransferSingle(context.Background(), evt)
		assert.NoError(t, err)
	})
}

func TestNFTEventHandler_ExtractAddresses(t *testing.T) {
	tests := []struct {
		name     string
		evt      *event.IndexedEvent
		wantFrom string
		wantTo   string
	}{
		{
			name: "from decoded map",
			evt: &event.IndexedEvent{
				Decoded: map[string]interface{}{
					"from": "0xdecoded_from",
					"to":   "0xdecoded_to",
				},
			},
			wantFrom: "0xdecoded_from",
			wantTo:   "0xdecoded_to",
		},
		{
			name: "from topics fallback",
			evt: &event.IndexedEvent{
				Topics: []string{"topic0", "0xtopic_from", "0xtopic_to"},
			},
			wantFrom: "0xtopic_from",
			wantTo:   "0xtopic_to",
		},
		{
			name: "decoded overrides topics",
			evt: &event.IndexedEvent{
				Decoded: map[string]interface{}{
					"from": "0xdecoded_from",
				},
				Topics: []string{"topic0", "0xtopic_from", "0xtopic_to"},
			},
			wantFrom: "0xdecoded_from",
			wantTo:   "0xtopic_to",
		},
		{
			name:     "no data",
			evt:      &event.IndexedEvent{},
			wantFrom: "",
			wantTo:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNFTEventHandler(&NFTService{}, zap.NewNop())
			from, to := h.extractAddresses(tt.evt)
			assert.Equal(t, tt.wantFrom, from)
			assert.Equal(t, tt.wantTo, to)
		})
	}
}

func TestNFTEventHandler_ExtractTokenID(t *testing.T) {
	tests := []struct {
		name    string
		evt     *event.IndexedEvent
		want    string
		wantErr bool
	}{
		{
			name: "decoded tokenId",
			evt: &event.IndexedEvent{
				Decoded: map[string]interface{}{"tokenId": "42"},
			},
			want:    "42",
			wantErr: false,
		},
		{
			name: "decoded tokenID uppercase",
			evt: &event.IndexedEvent{
				Decoded: map[string]interface{}{"tokenID": "99"},
			},
			want:    "99",
			wantErr: false,
		},
		{
			name: "topics fallback index 3",
			evt: &event.IndexedEvent{
				Topics: []string{"t0", "t1", "t2", "42"},
			},
			want:    "42",
			wantErr: false,
		},
		{
			name:    "no token ID",
			evt:     &event.IndexedEvent{},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNFTEventHandler(&NFTService{}, zap.NewNop())
			got, err := h.extractTokenID(tt.evt)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestNFTEventHandler_ExtractERC1155TokenID(t *testing.T) {
	t.Run("decoded id", func(t *testing.T) {
		h := NewNFTEventHandler(&NFTService{}, zap.NewNop())
		evt := &event.IndexedEvent{
			Decoded: map[string]interface{}{"id": "100"},
		}
		got := h.extractERC1155TokenID(evt)
		assert.Equal(t, "100", got)
	})

	t.Run("no decoded", func(t *testing.T) {
		h := NewNFTEventHandler(&NFTService{}, zap.NewNop())
		evt := &event.IndexedEvent{}
		got := h.extractERC1155TokenID(evt)
		assert.Equal(t, "", got)
	})
}

func TestNFTEventHandler_InvalidateMiddlewareCache(t *testing.T) {
	t.Run("from and to addresses", func(t *testing.T) {
		cache := &mockNFTAccessCache{}
		h := NewNFTEventHandlerWithCache(&NFTService{}, cache, 137, zap.NewNop())
		evt := &event.IndexedEvent{
			Decoded: map[string]interface{}{
				"from": "0xfrom_addr",
				"to":   "0xto_addr",
			},
		}
		h.invalidateMiddlewareCache(context.Background(), "0xcontract", "42", evt)
		assert.Contains(t, cache.deletedKeys, "137:0xfrom_addr:0xcontract:42")
		assert.Contains(t, cache.deletedKeys, "137:0xto_addr:0xcontract:42")
		assert.Contains(t, cache.deletedPrefixes, "137::0xcontract:42")
	})

	t.Run("empty addresses", func(t *testing.T) {
		cache := &mockNFTAccessCache{}
		h := NewNFTEventHandlerWithCache(&NFTService{}, cache, 1, zap.NewNop())
		evt := &event.IndexedEvent{}
		h.invalidateMiddlewareCache(context.Background(), "0xcontract", "42", evt)
		assert.Empty(t, cache.deletedKeys)
		assert.Contains(t, cache.deletedPrefixes, "1::0xcontract:42")
	})
}

package web3

import (
	"context"
	"math/big"
	"testing"

	"github.com/rtcdance/streamgate/pkg/web3/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewEventIndexer(t *testing.T) {
	ei, err := event.NewEventIndexer(nil, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "ContentRegistered(bytes32,address,uint256)", zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, ei)
}

func TestNewEventIndexerWithConfig(t *testing.T) {
	ei, err := event.NewEventIndexerWithConfig(nil, event.EventIndexerConfig{
		ContractAddresses: []string{"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"},
		EventSignatures:   []string{"ContentRegistered(bytes32,address,uint256)"},
		MaxEvents:         100,
	}, zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, ei)
}

func TestEventIndexer_SetEventStore(t *testing.T) {
	ei, _ := event.NewEventIndexer(nil, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "", zap.NewNop())
	store := event.NewMemoryEventStore()
	ei.SetEventStore(store)
}

func TestEventIndexer_SetEventParser(t *testing.T) {
	ei, _ := event.NewEventIndexer(nil, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "", zap.NewNop())
	parser := event.NewEventParser(zap.NewNop())
	ei.SetEventParser(parser)
}

func TestMemoryEventStore(t *testing.T) {
	store := event.NewMemoryEventStore()

	evt := &event.IndexedEvent{ID: "evt-1", BlockNumber: 100}
	err := store.SaveEvent(evt)
	require.NoError(t, err)

	events, err := store.GetEventsByBlockRange(0, 999999)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "evt-1", events[0].ID)
}

func TestContractWriteResult(t *testing.T) {
	result := &ContractWriteResult{
		TxHash:       "0xabc123",
		Nonce:        5,
		GasLimit:     21000,
		GasPrice:     big.NewInt(1000000000),
		MaxFeePerGas: big.NewInt(2000000000),
		TipCap:       big.NewInt(1000000000),
	}
	assert.Equal(t, "0xabc123", result.TxHash)
	assert.Equal(t, uint64(5), result.Nonce)
	assert.Equal(t, uint64(21000), result.GasLimit)
}

func TestNewContractWriter(t *testing.T) {
	cfg := ContractWriterConfig{
		Client:      nil,
		Key:         nil,
		NonceMgr:    nil,
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	}
	cw := NewContractWriter(cfg)
	assert.NotNil(t, cw)
}

func TestNewContentRegistryBinding(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil,
		nil,
		zap.NewNop(),
	)
	assert.NotNil(t, binding)
}

func TestContentRegistryBinding_RegisterContent_NilWriter(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	var hash [32]byte
	_, err := binding.RegisterContent(context.Background(), hash, "metadata")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "writer not configured")
}

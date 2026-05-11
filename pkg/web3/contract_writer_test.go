package web3

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- ContractWriter Tests ---

func TestNewContractWriter(t *testing.T) {
	cfg := ContractWriterConfig{
		Client:      nil, // would be *ChainClient in production
		Key:         nil,
		NonceMgr:    nil,
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	}
	cw := NewContractWriter(cfg)
	assert.NotNil(t, cw)
	assert.Equal(t, common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"), cw.fromAddress)
	assert.Equal(t, big.NewInt(1), cw.chainID)
}

func TestContractWriter_WithTracker(t *testing.T) {
	cw := NewContractWriter(ContractWriterConfig{
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	tracker := &TxTracker{}
	result := cw.WithTracker(tracker)
	assert.Equal(t, cw, result) // fluent API returns same instance
	assert.Equal(t, tracker, cw.tracker)
}

func TestContractWriter_SendTx_NilABI(t *testing.T) {
	cw := NewContractWriter(ContractWriterConfig{
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	_, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:       "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		Method:   "transfer",
		ParsedABI: nil,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ParsedABI is required")
}

func TestContractWriter_SendTx_NilNonceMgr(t *testing.T) {
	parsedABI := getTestABI(t)

	cw := NewContractWriter(ContractWriterConfig{
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	_, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		Method:    "registerContent",
		ParsedABI: parsedABI,
	})
	assert.Error(t, err)
	// Will panic or return error because nonceMgr is nil
}

// --- ContentRegistryBinding Tests ---

func TestNewContentRegistryBinding(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, // reader
		nil, // writer
		zap.NewNop(),
	)
	assert.NotNil(t, binding)
	assert.Equal(t, common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"), binding.address)
}

func TestContentRegistryBinding_ContentRegisteredTopic(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	topic := binding.ContentRegisteredTopic()
	// Should be keccak256 of "ContentRegistered(bytes32,address,uint256)"
	assert.NotEqual(t, common.Hash{}, topic)
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

func TestContentRegistryBinding_VerifyContent_NilReader(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	var hash [32]byte
	// Without a reader, VerifyContent will panic on nil dereference
	assert.Panics(t, func() {
		_, _ = binding.VerifyContent(context.Background(), hash)
	})
}

func TestContentRegistryBinding_GetContentInfo_NilReader(t *testing.T) {
	binding := NewContentRegistryBinding(
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		nil, nil, zap.NewNop(),
	)

	var hash [32]byte
	// Without a reader, GetContentInfo will panic on nil dereference
	assert.Panics(t, func() {
		_, _ = binding.GetContentInfo(context.Background(), hash)
	})
}

// --- NonceManager Tests ---

func TestNewNonceManager(t *testing.T) {
	nm := NewNonceManager(nil, zap.NewNop())
	assert.NotNil(t, nm)
	assert.Equal(t, 30*time.Minute, nm.evictTTL)
}

func TestNonceManager_EvictStaleLocked(t *testing.T) {
	nm := NewNonceManager(nil, zap.NewNop())

	// Add stale entries directly
	nm.mu.Lock()
	nm.cached["0xold"] = 0
	nm.lastSync["0xold"] = time.Time{} // epoch time, very stale
	nm.cached["0xnew"] = 5
	nm.lastSync["0xnew"] = time.Now()
	nm.mu.Unlock()

	// Call the eviction directly (it's unexported, but we're in the same package)
	nm.evictStaleLocked()

	nm.mu.Lock()
	_, oldExists := nm.cached["0xold"]
	_, newExists := nm.cached["0xnew"]
	nm.mu.Unlock()

	assert.False(t, oldExists, "stale entry should have been evicted")
	assert.True(t, newExists, "fresh entry should remain")
}

func TestNonceManager_ResetAddress(t *testing.T) {
	nm := NewNonceManager(nil, zap.NewNop())

	nm.mu.Lock()
	nm.cached["0xaddr"] = 42
	nm.lastSync["0xaddr"] = time.Now()
	nm.mu.Unlock()

	nm.Reset("0xaddr")

	nm.mu.Lock()
	assert.Equal(t, uint64(0), nm.cached["0xaddr"])
	nm.mu.Unlock()
}

// --- EventIndexer Tests ---

func TestNewEventIndexer(t *testing.T) {
	ei, err := NewEventIndexer(nil, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "ContentRegistered(bytes32,address,uint256)", zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, ei)
}

func TestNewEventIndexerWithConfig(t *testing.T) {
	ei, err := NewEventIndexerWithConfig(nil, EventIndexerConfig{
		ContractAddresses: []string{"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"},
		EventSignatures:   []string{"ContentRegistered(bytes32,address,uint256)"},
		MaxEvents:         100,
	}, zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, ei)
	assert.Equal(t, 100, ei.maxEvents)
}

func TestEventIndexer_SetEventStore(t *testing.T) {
	ei, _ := NewEventIndexer(nil, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "", zap.NewNop())
	store := &MemoryEventStore{}
	ei.SetEventStore(store)
	assert.Equal(t, store, ei.store)
}

func TestEventIndexer_SetEventParser(t *testing.T) {
	ei, _ := NewEventIndexer(nil, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "", zap.NewNop())
	parser := &EventParser{}
	ei.SetEventParser(parser)
	assert.Equal(t, parser, ei.eventParser)
}

// --- MemoryEventStore Tests ---

func TestMemoryEventStore(t *testing.T) {
	store := NewMemoryEventStore()

	// Store an event
	evt := &IndexedEvent{ID: "evt-1", BlockNumber: 100}
	err := store.SaveEvent(evt)
	require.NoError(t, err)

	// GetEventsByBlockRange should find it
	events, err := store.GetEventsByBlockRange(0, 999999)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "evt-1", events[0].ID)
}

// --- ContractWriteResult test ---

func TestContractWriteResult(t *testing.T) {
	result := &ContractWriteResult{
		TxHash:      "0xabc123",
		Nonce:       5,
		GasLimit:    21000,
		GasPrice:    big.NewInt(1000000000),
		MaxFeePerGas: big.NewInt(2000000000),
		TipCap:      big.NewInt(1000000000),
	}
	assert.Equal(t, "0xabc123", result.TxHash)
	assert.Equal(t, uint64(5), result.Nonce)
	assert.Equal(t, uint64(21000), result.GasLimit)
}

// --- Smart Contract Event Topics ---

func TestEventTopics_NonEmpty(t *testing.T) {
	// Verify the real keccak256 topics are non-zero
	assert.NotEqual(t, common.Hash{}, contentRegisteredTopic)
	assert.NotEqual(t, common.Hash{}, contentVerifiedTopic)
	assert.NotEqual(t, common.Hash{}, contentDeletedTopic)
	assert.NotEqual(t, common.Hash{}, transferTopic)
	assert.NotEqual(t, common.Hash{}, approvalTopic)
}

// --- Helper ---

func getTestABI(t *testing.T) *abi.ABI {
	t.Helper()
	// Minimal ABI with a "registerContent" method
	json := `[
		{
			"inputs": [
				{"name": "contentHash", "type": "bytes32"},
				{"name": "metadata", "type": "string"}
			],
			"name": "registerContent",
			"outputs": [],
			"type": "function"
		}
	]`
	parsed, err := abi.JSON(bytes.NewReader([]byte(json)))
	require.NoError(t, err)
	return &parsed
}

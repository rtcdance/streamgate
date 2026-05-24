package web3

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewERC20Reader(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	reader := NewERC20Reader(client, zap.NewNop())
	assert.NotNil(t, reader)
}

func TestNewERC1155Verifier(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	verifier := NewERC1155Verifier(client, zap.NewNop(), nil)
	assert.NotNil(t, verifier)
}

func TestNewEIP712Verifier(t *testing.T) {
	v := NewEIP712Verifier(zap.NewNop())
	assert.NotNil(t, v)
}

func TestNewSecurePrivateKeyFromHex(t *testing.T) {
	key, err := NewSecurePrivateKeyFromHex("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	assert.NotNil(t, key)
}

func TestNewSecurePrivateKeyFromHex_Invalid(t *testing.T) {
	_, err := NewSecurePrivateKeyFromHex("not-a-hex-key")
	require.Error(t, err)
}

func TestNewSIWEMessage(t *testing.T) {
	msg := NewSIWEMessage("example.com", "0x1234", "https://example.com", 1, "nonce123", time.Now())
	assert.NotNil(t, msg)
	assert.Equal(t, "example.com", msg.Domain)
}

func TestWithSIWEExpirationTime(t *testing.T) {
	expiry := time.Now().Add(1 * time.Hour)
	opt := WithSIWEExpirationTime(expiry)
	assert.NotNil(t, opt)
}

func TestBuildSIWEMessage(t *testing.T) {
	msg := NewSIWEMessage("example.com", "0x1234", "https://example.com", 1, "nonce123", time.Now())
	result := BuildSIWEMessage(msg)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "example.com")
}

func TestNewEventIndexer_Wrapper(t *testing.T) {
	reader := &stubEventReaderForInterfaces{blockNum: 100}
	indexer, err := NewEventIndexer(reader, "0x0000000000000000000000000000000000000abc", "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, indexer)
}

func TestNewEventIndexerWithConfig_Wrapper(t *testing.T) {
	reader := &stubEventReaderForInterfaces{blockNum: 100}
	cfg := EventIndexerConfig{
		ContractAddresses: []string{"0x0000000000000000000000000000000000000abc"},
		EventSignatures:   []string{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"},
	}
	indexer, err := NewEventIndexerWithConfig(reader, cfg, zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, indexer)
}

func TestNewMemoryEventStore_Wrapper(t *testing.T) {
	store := NewMemoryEventStore()
	assert.NotNil(t, store)
}

func TestNewEventParser_Wrapper(t *testing.T) {
	parser := NewEventParser(zap.NewNop())
	assert.NotNil(t, parser)
}

func TestNewEventListener_Wrapper(t *testing.T) {
	reader := &stubEventReaderForInterfaces{blockNum: 100}
	indexer, err := NewEventIndexer(reader, "0x0000000000000000000000000000000000000abc", "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", zap.NewNop())
	require.NoError(t, err)
	listener := NewEventListener(indexer, zap.NewNop())
	assert.NotNil(t, listener)
}

func TestDecodeContentRegisteredEvent_Wrapper(t *testing.T) {
	evt := &IndexedEvent{
		Topics: []string{"0xsig", "0xhash", "0xowner"},
	}
	decoded, err := DecodeContentRegisteredEvent(evt)
	require.NoError(t, err)
	assert.Equal(t, "0xhash", decoded.ContentHash)
}

func TestDecodeNFTMintedEvent_Wrapper(t *testing.T) {
	evt := &IndexedEvent{
		Topics: []string{"0xsig", "0xfrom", "0xowner", "0xtokenid"},
	}
	decoded, err := DecodeNFTMintedEvent(evt)
	require.NoError(t, err)
	assert.Equal(t, "0xtokenid", decoded.TokenID)
}

func TestNewContentRegistry(t *testing.T) {
	cr := NewContentRegistry("0x1234")
	assert.NotNil(t, cr)
}

func TestNewNFTContract(t *testing.T) {
	nc := NewNFTContract("0x5678")
	assert.NotNil(t, nc)
}

func TestNewSmartContractRegistry(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	assert.NotNil(t, scr)
}

func TestNewTransactionBuilder(t *testing.T) {
	tb := NewTransactionBuilder(zap.NewNop())
	assert.NotNil(t, tb)
}

func TestMulticall3DeployedAddress(t *testing.T) {
	addr := Multicall3DeployedAddress(1)
	assert.NotEqual(t, common.Address{}, addr)
}

func TestIsStuck(t *testing.T) {
	pending := &PendingTx{
		SentAt: time.Now().Add(-5 * time.Minute),
	}
	assert.True(t, IsStuck(pending, 1*time.Minute))
	assert.False(t, IsStuck(pending, 10*time.Minute))
}

func TestDefaultTxLifecycleConfig(t *testing.T) {
	cfg := DefaultTxLifecycleConfig()
	assert.NotNil(t, cfg)
}

func TestNewTxLifecycleManager(t *testing.T) {
	mgr := NewTxLifecycleManager(nil, nil, DefaultTxLifecycleConfig(), zap.NewNop())
	assert.NotNil(t, mgr)
}

func TestNewNonceManager(t *testing.T) {
	mgr := NewNonceManager(nil, zap.NewNop())
	assert.NotNil(t, mgr)
}

func TestNewTxTracker(t *testing.T) {
	tracker := NewTxTracker(nil, zap.NewNop())
	assert.NotNil(t, tracker)
}

func TestNewContractInteractor(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ci := NewContractInteractor(client, zap.NewNop())
	assert.NotNil(t, ci)
}

func TestNewContentRegistryBinding_Wrapper(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	reader := NewContractInteractor(client, zap.NewNop())
	writer := NewContractWriter(ContractWriterConfig{})
	binding := NewContentRegistryBinding("0x1234", reader, writer, zap.NewNop())
	assert.NotNil(t, binding)
}

func TestChainReaderInterface(t *testing.T) {
	var _ ChainReader = NewMultiChainManager(zap.NewNop())
}

func TestChainAdminInterface(t *testing.T) {
	var _ ChainAdmin = NewMultiChainManager(zap.NewNop())
}

func TestChainLifecycleInterface(t *testing.T) {
	var _ ChainLifecycle = NewMultiChainManager(zap.NewNop())
}

func TestChainManagerInterface(t *testing.T) {
	var _ ChainManagerInterface = NewMultiChainManager(zap.NewNop())
}

type stubEventReaderForInterfaces struct {
	blockNum uint64
}

func (s *stubEventReaderForInterfaces) BlockNumber(_ context.Context) (uint64, error) {
	return s.blockNum, nil
}

func (s *stubEventReaderForInterfaces) FilterLogs(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
	return nil, nil
}

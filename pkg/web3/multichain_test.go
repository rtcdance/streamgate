package web3

import (
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewMultiChainManager(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	require.NotNil(t, mcm)
	assert.NotNil(t, mcm.clients)
	assert.NotNil(t, mcm.solanaClients)
}

func TestMultiChainManager_AddChainWithClient(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())

	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)

	mcm.AddChainWithClient(1, client)

	got, err := mcm.GetClient(1)
	require.NoError(t, err)
	assert.Equal(t, client, got)
}

func TestMultiChainManager_GetClient_NotFound(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	_, err := mcm.GetClient(999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EVM chain client not found")
}

func TestMultiChainManager_RemoveChain(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())

	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)

	mcm.AddChainWithClient(1, client)
	mcm.RemoveChain(1)

	_, err = mcm.GetClient(1)
	require.Error(t, err)
}

func TestMultiChainManager_RemoveChain_NonExistent(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	assert.NotPanics(t, func() {
		mcm.RemoveChain(999)
	})
}

func TestMultiChainManager_GetChainConfig(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())

	cfg, err := mcm.GetChainConfig(1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), cfg.ID)
	assert.Equal(t, "Ethereum", cfg.Name)
	assert.False(t, cfg.IsTestnet)
}

func TestMultiChainManager_GetChainConfig_NotFound(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	_, err := mcm.GetChainConfig(999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chain not supported")
}

func TestMultiChainManager_GetSupportedChains(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	chains := mcm.GetSupportedChains()
	assert.NotEmpty(t, chains)
}

func TestGetSupportedChains_PackageLevel(t *testing.T) {
	chains := GetSupportedChains()
	assert.NotEmpty(t, chains)
}

func TestGetChainConfig_PackageLevel(t *testing.T) {
	cfg, ok := GetChainConfig(1)
	assert.True(t, ok)
	assert.Equal(t, "Ethereum", cfg.Name)

	_, ok = GetChainConfig(999)
	assert.False(t, ok)
}

func TestMultiChainManager_GetTestnetChains(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	chains := mcm.GetTestnetChains()
	assert.NotEmpty(t, chains)
	for _, c := range chains {
		assert.True(t, c.IsTestnet)
	}
}

func TestMultiChainManager_GetMainnetChains(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	chains := mcm.GetMainnetChains()
	assert.NotEmpty(t, chains)
	for _, c := range chains {
		assert.False(t, c.IsTestnet)
	}
}

func TestMultiChainManager_GetRPCStatuses(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())

	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)

	mcm.AddChainWithClient(1, client)
	statuses := mcm.GetRPCStatuses()
	assert.NotEmpty(t, statuses)
	_, ok := statuses[1]
	assert.True(t, ok)
}

func TestMultiChainManager_Close(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())

	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)

	mcm.AddChainWithClient(1, client)
	mcm.Close()

	mcm.mu.RLock()
	count := len(mcm.clients)
	mcm.mu.RUnlock()
	assert.Equal(t, 0, count)
}

func TestMultiChainManager_SetRateLimiter(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	rl := NewRPCRateLimiter(10, 20, zap.NewNop())
	mcm.SetRateLimiter(rl)
	assert.Equal(t, rl, mcm.rateLimiter)
}

func TestMultiChainManager_AddChain_Unsupported(t *testing.T) {
	mcm := NewMultiChainManager(zap.NewNop())
	err := mcm.AddChain(999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chain not supported")
}

func TestApplyChainConfigs(t *testing.T) {
	original, ok := GetChainConfig(1)
	require.True(t, ok)

	ApplyChainConfigs([]config.ChainConfigEntry{
		{
			ID:          1,
			Name:        "Ethereum Custom",
			RPC:         "https://custom-rpc.example.com",
			ExplorerURL: "https://custom-explorer.example.com",
			Currency:    "ETH",
			IsTestnet:   false,
		},
	})

	cfg, ok := GetChainConfig(1)
	require.True(t, ok)
	assert.Equal(t, "Ethereum Custom", cfg.Name)

	ApplyChainConfigs([]config.ChainConfigEntry{
		{
			ID:          1,
			Name:        original.Name,
			RPC:         original.RPC,
			ExplorerURL: original.Explorer,
			Currency:    original.Currency,
			IsTestnet:   original.IsTestnet,
		},
	})
}

func TestApplyChainConfigs_NewChain(t *testing.T) {
	ApplyChainConfigs([]config.ChainConfigEntry{
		{
			ID:          9999,
			Name:        "Test Chain",
			RPC:         "https://test.example.com",
			ExplorerURL: "https://test.example.com",
			Currency:    "TST",
			IsTestnet:   true,
		},
	})

	cfg, ok := GetChainConfig(9999)
	assert.True(t, ok)
	assert.Equal(t, "Test Chain", cfg.Name)
	assert.True(t, cfg.IsTestnet)
}

func TestChainConfig_Fields(t *testing.T) {
	cfg, ok := GetChainConfig(137)
	require.True(t, ok)
	assert.Equal(t, int64(137), cfg.ID)
	assert.Equal(t, "Polygon", cfg.Name)
	assert.Equal(t, "MATIC", cfg.Currency)
	assert.False(t, cfg.IsTestnet)
	assert.NotEmpty(t, cfg.RPC)
	assert.NotEmpty(t, cfg.RPCs)
}

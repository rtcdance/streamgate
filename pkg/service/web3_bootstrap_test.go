package service

import (
	"errors"
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type bootstrapChainRecorder struct {
	web3.ChainManagerInterface
	added []int64
	fail  bool
}

func (m *bootstrapChainRecorder) AddChain(chain int64) error {
	m.added = append(m.added, chain)
	if m.fail {
		return errors.New("unavailable fixture endpoint with private-provider-marker")
	}
	return nil
}

func (m *bootstrapChainRecorder) GetClient(int64) (*web3.ChainClient, error) {
	return nil, errors.New("fixture client unavailable; no RPC")
}

func (m *bootstrapChainRecorder) Close() {}

func TestWeb3Service_SepoliaBootstrapDoesNotAddDefaultChains(t *testing.T) {
	cfg := &config.Config{Environment: "w3sepolia", Web3: config.Web3Config{ChainID: 11155111, Chains: []config.ChainConfigEntry{{ID: 11155111}}}}
	manager := &bootstrapChainRecorder{}
	svc, err := NewWeb3Service(Web3Deps{ChainManager: manager}, cfg, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	require.Equal(t, []int64{11155111}, manager.added)
	manager = &bootstrapChainRecorder{fail: true}
	svc, err = NewWeb3Service(Web3Deps{ChainManager: manager}, cfg, zap.NewNop())
	require.Error(t, err)
	require.Nil(t, svc)
	require.NotContains(t, err.Error(), "private-provider-marker")
	require.Equal(t, []int64{11155111}, manager.added, "failure must not fall back to other networks")
}

func TestWeb3Service_RestrictedBootstrapRejectsWorkerOrForeignChain(t *testing.T) {
	for _, cfg := range []*config.Config{
		{Environment: "w3sepolia-worker"},
		{Environment: "w3sepolia", Web3: config.Web3Config{ChainID: 31337, Chains: []config.ChainConfigEntry{{ID: 31337}}}},
		{Environment: "w3sepolia", Web3: config.Web3Config{ChainID: 11155111}},
	} {
		manager := &bootstrapChainRecorder{}
		svc, err := NewWeb3Service(Web3Deps{ChainManager: manager}, cfg, zap.NewNop())
		require.Error(t, err)
		require.Nil(t, svc)
		require.Empty(t, manager.added, "reject before attempting any RPC")
	}
}

func TestWeb3Service_LegacyBootstrapPreservesDefaultChains(t *testing.T) {
	manager := &bootstrapChainRecorder{fail: true}
	svc, err := NewWeb3Service(Web3Deps{ChainManager: manager}, &config.Config{}, zap.NewNop())
	require.NoError(t, err, "legacy profiles retain optional chain bootstrap")
	t.Cleanup(svc.Close)
	require.Equal(t, []int64{11155111, 80002, 31337, -1, -2}, manager.added)
}

func TestDefaultWeb3Deps_RestrictedProfilesHaveNoSolanaVerifier(t *testing.T) {
	for _, profile := range []string{"w3sepolia", "w3sepolia-worker"} {
		deps := DefaultWeb3Deps(&config.Config{Environment: profile}, zap.NewNop())
		require.Nil(t, deps.SolanaVerif)
		deps.ChainManager.Close()
	}
}

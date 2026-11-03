package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// sepoliaChainsFixture mirrors the repo's chain list shape: an Anvil entry plus
// the Sepolia entry whose tracked endpoints the profile inputs must override.
func sepoliaChainsFixture(withSepolia bool) []map[string]any {
	chains := []map[string]any{
		{"id": 31337, "name": "anvil-local", "rpc_url": "http://anvil:8545", "rpc_urls": []string{"http://anvil:8545"}, "currency": "ETH"},
	}
	if withSepolia {
		chains = append(chains, map[string]any{
			"id": 11155111, "name": "ethereum-sepolia",
			"rpc_url": "https://tracked-sepolia.example", "rpc_urls": []string{"https://tracked-sepolia.example"},
			"currency": "ETH", "testnet": true,
		})
	}
	return chains
}

func loadWithChains(t *testing.T, chains []map[string]any) *Config {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("web3.chains", chains)
	cfg, err := LoadConfig()
	require.NoError(t, err)
	return cfg
}

func sepoliaEntry(cfg *Config) (ChainConfigEntry, bool) {
	for _, chain := range cfg.Web3.Chains {
		if chain.ID == 11155111 {
			return chain, true
		}
	}
	return ChainConfigEntry{}, false
}

func TestSepoliaProfile_EnvEndpointsOverrideChain(t *testing.T) {
	t.Setenv("SEPOLIA_RPC_URL", "https://primary.example")
	t.Setenv("SEPOLIA_RPC_FALLBACK_URL", "https://fallback.example")
	cfg := loadWithChains(t, sepoliaChainsFixture(true))
	chain, ok := sepoliaEntry(cfg)
	require.True(t, ok)
	require.Equal(t, []string{"https://primary.example", "https://fallback.example"}, chain.RPCs)
	require.Equal(t, "https://primary.example", chain.RPC)
	// The override is scoped to chain 11155111; every other chain keeps its own
	// endpoints.
	for _, chain := range cfg.Web3.Chains {
		if chain.ID == 31337 {
			require.Equal(t, []string{"http://anvil:8545"}, chain.RPCs)
		}
	}
}

func TestSepoliaProfile_FallbackFallsBackToPrimaryAlone(t *testing.T) {
	t.Setenv("SEPOLIA_RPC_URL", "https://primary.example")
	t.Setenv("SEPOLIA_RPC_FALLBACK_URL", "https://primary.example")
	cfg := loadWithChains(t, sepoliaChainsFixture(true))
	chain, ok := sepoliaEntry(cfg)
	require.True(t, ok)
	require.Equal(t, []string{"https://primary.example"}, chain.RPCs)
}

func TestSepoliaProfile_LegacySEPOLIARPCMapsToPrimary(t *testing.T) {
	t.Setenv("SEPOLIA_RPC", "https://legacy.example")
	cfg := loadWithChains(t, sepoliaChainsFixture(true))
	chain, ok := sepoliaEntry(cfg)
	require.True(t, ok)
	require.Equal(t, []string{"https://legacy.example"}, chain.RPCs)
}

func TestSepoliaProfile_LegacyDisagreementFails(t *testing.T) {
	t.Setenv("SEPOLIA_RPC", "https://legacy.example")
	t.Setenv("SEPOLIA_RPC_URL", "https://current.example")
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("web3.chains", sepoliaChainsFixture(true))
	_, err := LoadConfig()
	require.ErrorContains(t, err, "disagree")
	// The error must describe the conflict without echoing endpoint values:
	// they may carry provider credentials.
	require.NotContains(t, err.Error(), "https://")
}

func TestSepoliaProfile_MissingChainEntryFails(t *testing.T) {
	t.Setenv("SEPOLIA_RPC_URL", "https://primary.example")
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("web3.chains", sepoliaChainsFixture(false))
	_, err := LoadConfig()
	require.ErrorContains(t, err, "11155111")
}

func TestSepoliaProfile_NoEnvKeepsTrackedEndpoints(t *testing.T) {
	cfg := loadWithChains(t, sepoliaChainsFixture(true))
	chain, ok := sepoliaEntry(cfg)
	require.True(t, ok)
	require.Equal(t, []string{"https://tracked-sepolia.example"}, chain.RPCs)
}

func TestSepoliaProfile_SiweOriginBinding(t *testing.T) {
	t.Setenv("STREAMGATE_SIWE_DOMAIN", "localhost:53836")
	t.Setenv("STREAMGATE_SIWE_URI", "http://localhost:53836/login")
	cfg := loadWithChains(t, sepoliaChainsFixture(true))
	require.Equal(t, "localhost:53836", cfg.Auth.SIWEDomain)
	require.Equal(t, "http://localhost:53836/login", cfg.Auth.SIWEURI)
}

func TestSepoliaOverlay_Contract(t *testing.T) {
	// The overlay must present the real testnet shape: active chain 11155111,
	// a single chain entry (no Anvil), no deployer key, demo signing off, and
	// placeholder endpoints that fail closed unless SEPOLIA_RPC_URL is set.
	viper.Reset()
	t.Cleanup(viper.Reset)
	overlay := viper.New()
	overlay.SetConfigFile("../../../config/config.w3sepolia.yaml")
	require.NoError(t, overlay.ReadInConfig())
	require.Equal(t, 11155111, overlay.GetInt("web3.chain_id"))
	require.Equal(t, "", overlay.GetString("web3.anvil_deployer_key"))
	require.Equal(t, "", overlay.GetString("web3.anvil_demo_contract"))
	require.False(t, overlay.GetBool("features.demo_tx_signer"))
	var chains []ChainConfigEntry
	require.NoError(t, overlay.UnmarshalKey("web3.chains", &chains))
	require.Len(t, chains, 1)
	require.Equal(t, int64(11155111), chains[0].ID)
	require.True(t, chains[0].IsTestnet)
	for _, entry := range chains {
		for _, endpoint := range append([]string{entry.RPC}, entry.RPCs...) {
			require.Contains(t, endpoint, ".invalid", "overlay endpoints must fail closed until SEPOLIA_RPC_URL overrides them")
		}
	}
}

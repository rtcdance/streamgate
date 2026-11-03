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
	require.Equal(t, []string{"360p"}, overlay.GetStringSlice("transcode.profiles"))
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

func strictSepoliaFixture() *Config {
	cfg := DefaultConfig()
	cfg.Environment = "w3sepolia"
	cfg.Web3.ChainID = 11155111
	cfg.Web3.BlockTag = "safe"
	cfg.Web3.EthereumRPC = "https://primary.example"
	cfg.Web3.AnvilDemoContract = ""
	cfg.Web3.AnvilDeployerKey = ""
	cfg.Web3.Transaction.PrivateKeyHex = ""
	cfg.Features.DemoTxSigner = false
	cfg.Transcode.Profiles = []string{"360p"}
	cfg.Web3.Chains = []ChainConfigEntry{{ID: 11155111, RPC: "https://primary.example", RPCs: []string{"https://primary.example", "https://fallback.example"}}}
	cfg.Auth.SIWEDomain = "localhost:18000"
	cfg.Auth.SIWEURI = "http://localhost:18000/login"
	return cfg
}

func TestSepoliaProfile_StartupRejectsDowngrades(t *testing.T) {
	require.NoError(t, validateSepoliaProfile(strictSepoliaFixture()))
	cases := []struct {
		name   string
		change func(*Config)
	}{
		{"multiple_upload_renditions", func(c *Config) { c.Transcode.Profiles = []string{"360p", "720p"} }},
		{"abr_upload", func(c *Config) { c.Transcode.Profiles = []string{"abr"} }},
		{"local_chain", func(c *Config) { c.Web3.ChainID = 31337 }},
		{"extra_chain", func(c *Config) { c.Web3.Chains = append(c.Web3.Chains, ChainConfigEntry{ID: 31337}) }},
		{"demo_signer", func(c *Config) { c.Features.DemoTxSigner = true }},
		{"deployer", func(c *Config) { c.Web3.AnvilDeployerKey = "sensitive-test-key" }},
		{"backend_key", func(c *Config) { c.Web3.Transaction.PrivateKeyHex = "sensitive-test-key" }},
		{"local_nft", func(c *Config) { c.Web3.AnvilDemoContract = "0x1234" }},
		{"latest", func(c *Config) { c.Web3.BlockTag = "latest" }},
		{"missing_fallback", func(c *Config) { c.Web3.Chains[0].RPCs = c.Web3.Chains[0].RPCs[:1] }},
		{"duplicate_host", func(c *Config) { c.Web3.Chains[0].RPCs[1] = "https://primary.example/other-project" }},
		{"wrong_ethereum_rpc", func(c *Config) { c.Web3.EthereumRPC = "http://anvil:8545" }},
		{"placeholder", func(c *Config) { c.Web3.Chains[0].RPCs[1] = "https://placeholder.invalid" }},
		{"http", func(c *Config) { c.Web3.Chains[0].RPCs[1] = "http://fallback.example" }},
		{"credentials", func(c *Config) { c.Web3.Chains[0].RPCs[1] = "https://user:sensitive-test-key@fallback.example" }},
		{"origin_mismatch", func(c *Config) { c.Auth.SIWEDomain = "other.example" }},
		{"remote_origin", func(c *Config) { c.Auth.SIWEDomain = "remote.example"; c.Auth.SIWEURI = "https://remote.example/login" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := strictSepoliaFixture()
			tc.change(c)
			err := validateSepoliaProfile(c)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "sensitive-test-key")
			require.NotContains(t, err.Error(), "https://")
		})
	}
}

func TestSepoliaUploadProfile_DoesNotChangeLegacyABRDefault(t *testing.T) {
	t.Setenv("STREAMGATE_ENV", "dev")
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("transcode.profiles", []string{"360p"})
	cfg, err := LoadConfig()
	require.NoError(t, err)
	// Existing profiles leave this empty, so their upload hook still selects ABR.
	require.Empty(t, cfg.Transcode.Profiles)
}

func TestSepoliaProfile_LoadEnforcesPublicProfileBoundary(t *testing.T) {
	t.Setenv("STREAMGATE_ENV", "w3sepolia")
	t.Setenv("SEPOLIA_RPC_URL", "https://primary.example")
	t.Setenv("SEPOLIA_RPC_FALLBACK_URL", "https://fallback.example")
	t.Setenv("STREAMGATE_SIWE_DOMAIN", "localhost:18000")
	t.Setenv("STREAMGATE_SIWE_URI", "http://localhost:18000/login")
	viper.Reset()
	t.Cleanup(viper.Reset)
	c := strictSepoliaFixture()
	viper.Set("web3.chain_id", c.Web3.ChainID)
	viper.Set("web3.chains", []map[string]any{{"id": 11155111, "rpc_url": "https://placeholder.invalid", "rpc_urls": []string{"https://placeholder.invalid"}}})
	viper.Set("web3.anvil_demo_contract", "")
	viper.Set("web3.anvil_deployer_key", "")
	viper.Set("web3.transaction.private_key_hex", "")
	viper.Set("features.demo_tx_signer", false)
	viper.Set("transcode.profiles", []string{"360p"})
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "https://primary.example", cfg.Web3.EthereumRPC)
	require.Equal(t, []string{"360p"}, cfg.Transcode.Profiles)
	t.Setenv("FEATURES_DEMO_TX_SIGNER", "true")
	// Viper overrides have precedence over environment values; remove this
	// explicit fixture override before checking the real environment path.
	viper.Set("features.demo_tx_signer", nil)
	_, err = LoadConfig()
	require.Error(t, err)
}

func TestSepoliaWorkerProfile_RefusesWalletTrustDomain(t *testing.T) {
	require.NoError(t, validateSepoliaWorkerProfile(&Config{}))
	for _, change := range []func(*Config){
		func(c *Config) { c.Auth.JWTSecret = "secret" },
		func(c *Config) { c.Auth.AccessSecret = "secret" },
		func(c *Config) { c.Auth.CapabilitySecret = "secret" },
		func(c *Config) { c.Auth.PreviousAccessSecrets = []string{"secret"} },
		func(c *Config) { c.Auth.PreviousCapabilitySecrets = []string{"secret"} },
		func(c *Config) { c.Web3.ChainID = 11155111 },
		func(c *Config) { c.Web3.Chains = []ChainConfigEntry{{ID: 31337}} },
		func(c *Config) { c.Web3.EthereumRPC = "https://rpc.example" },
		func(c *Config) { c.Web3.EthereumWSURL = "wss://rpc.example" },
		func(c *Config) { c.Web3.AnvilDemoContract = "0x1234" },
		func(c *Config) { c.Web3.AnvilDeployerKey = "secret" },
		func(c *Config) { c.Web3.Transaction.PrivateKeyHex = "secret" },
		func(c *Config) { c.Web3.ContentRegistryAddress = "0x1234" },
		func(c *Config) { c.Features.DemoTxSigner = true },
	} {
		cfg := &Config{}
		change(cfg)
		require.Error(t, validateSepoliaWorkerProfile(cfg))
	}
}

func TestSepoliaWorkerOverlay_LoadsWithoutWalletSecretsOrRPC(t *testing.T) {
	t.Setenv("STREAMGATE_ENV", "w3sepolia-worker")
	viper.Reset()
	t.Cleanup(viper.Reset)
	// LoadConfig searches its runtime directories; explicitly load the tracked
	// base/overlay here so this test also detects stale inherited chain entries.
	viper.SetConfigFile("../../../config/config.yaml")
	require.NoError(t, viper.ReadInConfig())
	viper.SetConfigFile("../../../config/config.w3sepolia-worker.yaml")
	require.NoError(t, viper.MergeInConfig())
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Empty(t, cfg.Web3.Chains)
	require.Zero(t, cfg.Web3.ChainID)
	require.Empty(t, cfg.Auth.JWTSecret)
	require.Equal(t, 1, cfg.Transcoding.MaxWorkers)
	require.Equal(t, 1, cfg.Transcoding.FFmpegThreads)
	t.Setenv("STREAMGATE_JWT_SECRET", "must-never-enter-the-worker")
	_, err = LoadConfig()
	require.ErrorContains(t, err, "forbids JWT")
}

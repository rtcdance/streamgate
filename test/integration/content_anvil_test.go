//go:build anvil

package integration_test

// Anvil integration test for TRANSITION_PLAN B3.
//
// Validates the end-to-end on-chain registration path the service uses:
//   - the Go ContentRegistryBinding ABI matches the DEPLOYED
//     contracts/src/ContentRegistry.sol (the 2026-10-10 review found them
//     diverged, which made every registration revert);
//   - the tx is signed with the deployer key and accepted (onlyOwner);
//   - contentExists flips to true and getContent returns the record.
//
// Run with: make test-anvil  (Anvil up, ContentRegistry deployed via
// contracts/deploy-anvil.sh, env CONTENT_REGISTRY_ADDRESS + ANVIL_DEPLOYER_KEY set).

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/gateway"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAnvil_ContentRegistry_RegisterAndReadBack(t *testing.T) {
	registryAddr := os.Getenv("CONTENT_REGISTRY_ADDRESS")
	deployerKey := os.Getenv("ANVIL_DEPLOYER_KEY")
	rpcURL := os.Getenv("ANVIL_RPC")
	if rpcURL == "" {
		rpcURL = "http://localhost:8545"
	}
	if registryAddr == "" || deployerKey == "" {
		t.Skip("CONTENT_REGISTRY_ADDRESS / ANVIL_DEPLOYER_KEY not set")
	}

	ethClient, err := ethclient.Dial(rpcURL)
	require.NoError(t, err, "anvil must be reachable at %s", rpcURL)
	defer ethClient.Close()

	cfg := &config.Config{}
	cfg.Web3.ContentRegistryAddress = registryAddr
	cfg.Web3.AnvilDeployerKey = deployerKey
	cfg.Web3.EthereumRPC = rpcURL

	registry := gateway.ProvideContentRegistry(cfg, zap.NewNop())
	require.NotNil(t, registry, "binding must be constructible with the config above")

	// The service-facing interface only exposes RegisterContent; the read-back
	// assertions need the concrete binding.
	binding, ok := registry.(*web3.ContentRegistryBinding)
	require.True(t, ok, "provider must return the concrete binding")

	contentID := time.Now().Format("it-20060102-150405")
	contentHash := "0x" + contentID // opaque string on-chain; only existence is asserted

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	existsBefore, err := binding.ContentExists(ctx, contentID)
	require.NoError(t, err)
	require.False(t, existsBefore, "content must not exist before registration")

	txHash, err := binding.RegisterContent(ctx, contentID, contentHash, "ipfs://integration-test")
	require.NoError(t, err, "RegisterContent tx must be accepted by the deployed contract")
	require.True(t, len(txHash) >= 66, "tx hash must look like a hash: %s", txHash)

	existsAfter, err := binding.ContentExists(ctx, contentID)
	require.NoError(t, err)
	require.True(t, existsAfter, "content must exist on-chain after registration")

	record, err := binding.GetContent(ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, contentID, record.ContentID)
	require.Equal(t, contentHash, record.ContentHash)
}

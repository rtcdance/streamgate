package gateway

import (
	"errors"
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	registryTestAddress = "0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"
	registryDemoKey     = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	registryTxKey       = "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"
)

// Production registry transactions are signed by the CONFIGURED transaction key,
// not by the demo deployer key.
func TestResolveRegistrySigner_PrefersTheConfiguredTransactionKey(t *testing.T) {
	cfg := &config.Config{Profile: config.ProfileProduction}
	cfg.Web3.Transaction.PrivateKeyHex = registryTxKey
	cfg.Web3.AnvilDeployerKey = registryDemoKey

	key, err := resolveRegistrySigner(cfg)
	require.NoError(t, err)
	assert.Equal(t, registryTxKey, key)
}

// The demo stack has no transaction key configured and signs with the Anvil
// deployer key. Outside a strict profile that is legitimate.
func TestResolveRegistrySigner_DemoMayUseTheDeployerKey(t *testing.T) {
	cfg := &config.Config{Profile: config.ProfileDemo}
	cfg.Web3.AnvilDeployerKey = registryDemoKey

	key, err := resolveRegistrySigner(cfg)
	require.NoError(t, err)
	assert.Equal(t, registryDemoKey, key)
}

// A strict profile must NOT let a well-known demo key sign production registry
// transactions, even when one is configured.
func TestResolveRegistrySigner_StrictRefusesTheDemoKey(t *testing.T) {
	for _, profile := range []string{config.ProfileProduction, config.ProfileDurable} {
		cfg := &config.Config{Profile: profile}
		cfg.Web3.AnvilDeployerKey = registryDemoKey

		_, err := resolveRegistrySigner(cfg)
		require.Error(t, err, "profile %s must refuse the demo deployer key", profile)
		assert.Contains(t, err.Error(), "web3.transaction.private_key_hex")
	}
}

func TestResolveRegistrySigner_WithoutAnyKeyIsAnError(t *testing.T) {
	cfg := &config.Config{Profile: config.ProfileDemo}
	_, err := resolveRegistrySigner(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "web3.transaction.private_key_hex")
}

// An empty address is the documented way to disable on-chain registration — it
// must stay a clean "disabled", never an error, and never a silent attempt to
// build a binding.
func TestBuildContentRegistryBinding_EmptyAddressIsExplicitlyDisabled(t *testing.T) {
	cfg := &config.Config{Profile: config.ProfileDemo}

	_, _, _, _, err := buildContentRegistryBinding(cfg, zap.NewNop())
	assert.ErrorIs(t, err, ErrContentRegistryDisabled)
}

func TestProvideContentRegistry_ExplicitlyDisabledIsNotAnError(t *testing.T) {
	cfg := &config.Config{Profile: config.ProfileDemo}

	registry, err := ProvideContentRegistry(cfg, zap.NewNop())
	assert.NoError(t, err)
	assert.Nil(t, registry)
}

func TestProvideRegistryHub_ExplicitlyDisabledIsNotAnError(t *testing.T) {
	cfg := &config.Config{Profile: config.ProfileDemo}

	hub, err := ProvideRegistryHub(cfg, zap.NewNop(), nil)
	assert.NoError(t, err)
	assert.Nil(t, hub)
}

// An ENABLED registry with an unqualified signer is a hard error, not a nil hub
// with a log line: a deployment that advertises registration availability it
// cannot honour must fail startup.
func TestProvideRegistryHub_EnabledButUnqualifiedIsAnError(t *testing.T) {
	cfg := &config.Config{Profile: config.ProfileProduction}
	cfg.Web3.ContentRegistryAddress = registryTestAddress

	hub, err := ProvideRegistryHub(cfg, zap.NewNop(), nil)
	require.Error(t, err, "an enabled registry without a signer must not degrade silently")
	assert.Nil(t, hub)
	assert.Contains(t, err.Error(), "signer")
}

// The disabled sentinel must remain distinguishable: callers branch on it, so it
// must not be wrapped into something errors.Is cannot see.
func TestErrContentRegistryDisabled_IsRecognisableThroughWrapping(t *testing.T) {
	wrapped := errors.New("outer")
	assert.False(t, errors.Is(wrapped, ErrContentRegistryDisabled))
	assert.False(t, errors.Is(nil, ErrContentRegistryDisabled))
	assert.Equal(t, "web3.content_registry_address not set (on-chain registration disabled)", ErrContentRegistryDisabled.Error())
}

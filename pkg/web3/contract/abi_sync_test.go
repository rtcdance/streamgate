package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoRoot walks up from the test working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "go.mod not found above %s", dir)
		dir = parent
	}
}

// loadForgeABI parses a Forge build artifact's ABI.
func loadForgeABI(t *testing.T, rel string) abi.ABI {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	require.NoError(t, err, "run `forge build` in contracts/ first")
	var art struct {
		ABI json.RawMessage `json:"abi"`
	}
	require.NoError(t, json.Unmarshal(raw, &art))
	parsed, err := abi.JSON(strings.NewReader(string(art.ABI)))
	require.NoError(t, err)
	return parsed
}

func typeList(args abi.Arguments) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a.Type.String()
	}
	return out
}

// TestContentRegistryABI_MatchesForgeArtifact is the anti-drift guard for the
// bug that broke every on-chain registration: the hardcoded Go ABI and
// contracts/src/ContentRegistry.sol silently diverged. Any function, event or
// error that exists on one side must match the other exactly, and the event
// topic hashes used by the Go binding must equal the artifact's event IDs.
func TestContentRegistryABI_MatchesForgeArtifact(t *testing.T) {
	hardcoded, err := abi.JSON(strings.NewReader(ContentRegistryABI))
	require.NoError(t, err, "hardcoded ContentRegistryABI must be valid JSON")
	onchain := loadForgeABI(t, "contracts/out/ContentRegistry.sol/ContentRegistry.json")

	// The critical selector must be the SG-11 shape.
	reg, ok := onchain.Methods["registerContent"]
	require.True(t, ok, "registerContent missing from the deployed contract")
	assert.Equal(t, "registerContent(string,address,bytes32,string)", reg.Sig)

	for name, m := range hardcoded.Methods {
		om, found := onchain.Methods[name]
		require.True(t, found, "function %s is in the Go ABI but not in the contract", name)
		assert.Equal(t, om.Sig, m.Sig, "function %s signature drifted", name)
		assert.Equal(t, om.ID, m.ID, "function %s selector drifted", name)
		assert.Equal(t, om.StateMutability, m.StateMutability, "function %s mutability drifted", name)
		assert.Equal(t, typeList(om.Inputs), typeList(m.Inputs), "function %s inputs drifted", name)
		assert.Equal(t, typeList(om.Outputs), typeList(m.Outputs), "function %s outputs drifted", name)
	}

	for name, e := range hardcoded.Events {
		oe, found := onchain.Events[name]
		require.True(t, found, "event %s is in the Go ABI but not in the contract", name)
		assert.Equal(t, oe.Sig, e.Sig, "event %s signature drifted", name)
		assert.Equal(t, oe.ID, e.ID, "event %s topic drifted", name)
	}

	for name, e := range hardcoded.Errors {
		oe, found := onchain.Errors[name]
		require.True(t, found, "error %s is in the Go ABI but not in the contract", name)
		assert.Equal(t, oe.Sig, e.Sig, "error %s signature drifted", name)
	}

	// The topic hashes the binding filters on must come from the same contract.
	assert.Equal(t, onchain.Events["ContentRegistered"].ID, contentRegisteredTopic)
	assert.Equal(t, onchain.Events["ContentStatusUpdated"].ID, contentStatusTopic)
}

// TestERC721ABI_MatchesDemoNFTArtifact applies the same guard to the demo NFT.
func TestERC721ABI_MatchesDemoNFTArtifact(t *testing.T) {
	hardcoded, err := abi.JSON(strings.NewReader(ERC721ABI))
	require.NoError(t, err)
	onchain := loadForgeABI(t, "contracts/out/DemoNFT.sol/DemoNFT.json")

	for name, m := range hardcoded.Methods {
		om, found := onchain.Methods[name]
		require.True(t, found, "function %s is in the Go ABI but not in the contract", name)
		assert.Equal(t, om.Sig, m.Sig, "function %s signature drifted", name)
		assert.Equal(t, typeList(om.Inputs), typeList(m.Inputs), "function %s inputs drifted", name)
		assert.Equal(t, typeList(om.Outputs), typeList(m.Outputs), "function %s outputs drifted", name)
	}

	for name, e := range hardcoded.Events {
		oe, found := onchain.Events[name]
		require.True(t, found, "event %s is in the Go ABI but not in the contract", name)
		assert.Equal(t, oe.ID, e.ID, "event %s topic drifted", name)
	}
}

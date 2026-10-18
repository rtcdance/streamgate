package contract

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forgeArtifactABI reads the raw ABI document out of a Forge build artifact.
func forgeArtifactABI(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	require.NoError(t, err, "run `forge build` in contracts/ first")
	var art struct {
		ABI json.RawMessage `json:"abi"`
	}
	require.NoError(t, json.Unmarshal(raw, &art))
	require.NotEmpty(t, art.ABI)
	return string(art.ABI)
}

// The deployment record the image ships must describe the ABI this binary
// implements. This is the unit-level form of the startup check: if the two ever
// drift, the image would revert every registration against the deployed
// contract, so the drift must fail here first.
func TestDeploymentManifest_ABIHashMatchesTheRuntimeABI(t *testing.T) {
	manifest, err := LoadContentRegistryManifest(filepath.Join(repoRoot(t), ContentRegistryManifestPath(31337)))
	require.NoError(t, err, "the demo chain's deployment record must exist and be complete")

	runtimeABI, err := RuntimeRegistryABIHash()
	require.NoError(t, err)
	assert.Equal(t, runtimeABI, manifest.ABIHash,
		"the image's ABI hash must equal the deployed contract's recorded ABI hash")
}

// The chain's record must also match the artifact the contract was actually
// built from — otherwise the record describes a different build than the one
// deployed.
func TestDeploymentManifest_ABIHashMatchesTheForgeArtifact(t *testing.T) {
	manifest, err := LoadContentRegistryManifest(filepath.Join(repoRoot(t), ContentRegistryManifestPath(31337)))
	require.NoError(t, err)

	artifactHash, err := CanonicalABIHash(forgeArtifactABI(t, "contracts/out/ContentRegistry.sol/ContentRegistry.json"))
	require.NoError(t, err)
	assert.Equal(t, artifactHash, manifest.ABIHash,
		"the deployment record must hash the ABI of the artifact that was deployed")
}

// The canonical form must not depend on how a toolchain happens to order or
// space the document: the deploy tooling and the image compute it separately.
func TestCanonicalABIHash_IsOrderAndWhitespaceInsensitive(t *testing.T) {
	compact := `[{"type":"function","name":"b","inputs":[{"type":"uint256"}],"outputs":[],"stateMutability":"view"},
                {"type":"function","name":"a","inputs":[],"outputs":[{"type":"string"}],"stateMutability":"view"}]`
	spread := `[
	  {
	    "stateMutability": "view",
	    "outputs": [ { "type": "string" } ],
	    "name": "a",
	    "inputs": [],
	    "type": "function"
	  },
	  { "type": "function", "stateMutability": "view", "name": "b", "outputs": [], "inputs": [ { "type": "uint256" } ] }
	]`

	first, err := CanonicalABIHash(compact)
	require.NoError(t, err)
	second, err := CanonicalABIHash(spread)
	require.NoError(t, err)
	assert.Equal(t, first, second, "key order and whitespace must not change the canonical hash")
}

// A different ABI must produce a different hash: this is the drift the check
// exists to catch.
func TestCanonicalABIHash_ChangesWhenAnEntryChanges(t *testing.T) {
	base := `[{"type":"function","name":"registerContent","inputs":[{"type":"string"}],"outputs":[],"stateMutability":"nonpayable"}]`
	changed := `[{"type":"function","name":"registerContent","inputs":[{"type":"string"},{"type":"uint8"}],"outputs":[],"stateMutability":"nonpayable"}]`

	first, err := CanonicalABIHash(base)
	require.NoError(t, err)
	second, err := CanonicalABIHash(changed)
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
}

func TestCanonicalABIHash_RejectsAnEmptyOrInvalidDocument(t *testing.T) {
	_, err := CanonicalABIHash("[]")
	assert.Error(t, err)

	_, err = CanonicalABIHash("not json")
	assert.Error(t, err)
}

// runtimeCodeHash must be the keccak256 the deploy tooling records, in the same
// 0x-prefixed form.
func TestRuntimeCodeHash_IsKeccak256OfTheCode(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x02}
	assert.Equal(t, crypto.Keccak256Hash(code).Hex(), RuntimeCodeHash(code))
	assert.Len(t, RuntimeCodeHash(nil), 66, "an empty code hash is still a hash, so emptiness must be checked separately")
}

// stubCodeReader answers a scripted runtime code for a scripted address.
type stubCodeReader struct {
	code  []byte
	err   error
	asked []common.Address
}

func (s *stubCodeReader) CodeAt(_ context.Context, account common.Address, _ *big.Int) ([]byte, error) {
	s.asked = append(s.asked, account)
	return s.code, s.err
}

func manifestFor(t *testing.T, chainID int64, code []byte) *ContentRegistryManifest {
	t.Helper()
	m, err := BuildContentRegistryManifest(chainID, "0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512",
		"0xabc", "solc 0.8.24", "contracts/src/ContentRegistry.sol:ContentRegistry", "deadbeef", code)
	require.NoError(t, err)
	return m
}

func TestVerifyIdentity_AcceptsAMatchingDeployment(t *testing.T) {
	code := []byte{0x60, 0x80, 0x60, 0x40}
	m := manifestFor(t, 31337, code)

	require.NoError(t, m.VerifyIdentity(context.Background(), &stubCodeReader{code: code}, 31337))
}

// A different chain than the one the manifest names is a refusal: the same
// address on another chain is a different contract.
func TestVerifyIdentity_RefusesAChainMismatch(t *testing.T) {
	code := []byte{0x60, 0x80}
	m := manifestFor(t, 31337, code)

	err := m.VerifyIdentity(context.Background(), &stubCodeReader{code: code}, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chain")
}

// No code at the address means the contract is not deployed — nothing can be
// registered, so the registry must not claim to be available.
func TestVerifyIdentity_RefusesAnAddressWithoutRuntimeCode(t *testing.T) {
	code := []byte{0x60, 0x80}
	m := manifestFor(t, 31337, code)

	err := m.VerifyIdentity(context.Background(), &stubCodeReader{code: nil}, 31337)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no runtime code")
}

// Code that hashes differently means the manifest names a different build.
func TestVerifyIdentity_RefusesARuntimeCodeHashMismatch(t *testing.T) {
	m := manifestFor(t, 31337, []byte{0x60, 0x80})

	err := m.VerifyIdentity(context.Background(), &stubCodeReader{code: []byte{0x60, 0x81}}, 31337)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime code hash mismatch")
}

// An ABI hash mismatch is the drift this whole mechanism exists to catch.
func TestVerifyIdentity_RefusesAnABIHashMismatch(t *testing.T) {
	code := []byte{0x60, 0x80}
	m := manifestFor(t, 31337, code)
	m.ABIHash = "0000000000000000000000000000000000000000000000000000000000000000"

	err := m.VerifyIdentity(context.Background(), &stubCodeReader{code: code}, 31337)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ABI hash mismatch")
}

func TestVerifyIdentity_RefusesWithoutAChainReader(t *testing.T) {
	m := manifestFor(t, 31337, []byte{0x60, 0x80})
	require.Error(t, m.VerifyIdentity(context.Background(), nil, 31337))
}

func TestLoadContentRegistryManifest_RejectsAMissingOrIncompleteRecord(t *testing.T) {
	_, err := LoadContentRegistryManifest(filepath.Join(t.TempDir(), "absent.json"))
	require.Error(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "content-registry.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"chain_id":31337}`), 0o600))
	_, err = LoadContentRegistryManifest(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "address", "the missing fields must be named")
}

package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ContentRegistryManifest is the DEPLOYMENT RECORD of the on-chain content
// registry: which chain, which address, which deploy transaction, the keccak256
// of the deployed runtime code, the canonical ABI hash, the compiler and the
// artifact the contract was built from.
//
// It is what the running image is checked against at startup. An image whose
// ABI hash differs from the manifest is speaking an interface the deployed
// contract does not implement — the exact failure the 2026-10-10 review found
// when the Go ABI and the Solidity source had silently drifted apart.
type ContentRegistryManifest struct {
	ChainID         int64  `json:"chain_id"`
	Address         string `json:"address"`
	DeployTx        string `json:"deploy_tx"`
	RuntimeCodeHash string `json:"runtime_code_hash"`
	ABIHash         string `json:"abi_hash"`
	Compiler        string `json:"compiler"`
	Artifact        string `json:"artifact"`
	ArtifactHash    string `json:"artifact_hash"`
}

// ContentRegistryManifestPath is where a chain's deployment record lives. The
// directory is shipped inside the application image, so verification needs no
// extra mount.
func ContentRegistryManifestPath(chainID int64) string {
	return fmt.Sprintf("deployments/content-registry.%d.json", chainID)
}

// LoadContentRegistryManifest reads and validates a deployment record. A missing
// or malformed record is an error: an enabled registry without a deployment
// identity cannot be verified, and an unverifiable registry must not be
// advertised.
func LoadContentRegistryManifest(path string) (*ContentRegistryManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("content registry deployment manifest %s is unreadable: %w", path, err)
	}
	var m ContentRegistryManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("content registry deployment manifest %s is malformed: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("content registry deployment manifest %s: %w", path, err)
	}
	return &m, nil
}

// Validate checks the record is complete. Every field is required, because each
// one is an assertion about the deployment that the startup check relies on.
func (m *ContentRegistryManifest) Validate() error {
	var missing []string
	if m.ChainID <= 0 {
		missing = append(missing, "chain_id")
	}
	if m.Address == "" {
		missing = append(missing, "address")
	}
	if m.DeployTx == "" {
		missing = append(missing, "deploy_tx")
	}
	if m.RuntimeCodeHash == "" {
		missing = append(missing, "runtime_code_hash")
	}
	if m.ABIHash == "" {
		missing = append(missing, "abi_hash")
	}
	if m.Compiler == "" {
		missing = append(missing, "compiler")
	}
	if m.Artifact == "" {
		missing = append(missing, "artifact")
	}
	if m.ArtifactHash == "" {
		missing = append(missing, "artifact_hash")
	}
	if len(missing) > 0 {
		return fmt.Errorf("incomplete deployment identity — missing %s", strings.Join(missing, ", "))
	}
	return nil
}

// CanonicalABIHash hashes an ABI JSON document into a form two independent
// implementations can agree on.
//
// The canonical form is one line per ABI entry:
//
//	<type>\t<name>\t<inputs types joined by ",">\t<outputs types joined by ",">\t<stateMutability>
//
// with the lines sorted and hashed with SHA-256. Nothing else is included, so
// the hash does not depend on key order, whitespace or how a toolchain happens
// to render the document — which is what lets the deploy tooling and the running
// image compute the same value from their own copies.
func CanonicalABIHash(abiJSON string) (string, error) {
	var entries []struct {
		Type            string `json:"type"`
		Name            string `json:"name"`
		StateMutability string `json:"stateMutability"`
		Inputs          []struct {
			Type string `json:"type"`
		} `json:"inputs"`
		Outputs []struct {
			Type string `json:"type"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(abiJSON), &entries); err != nil {
		return "", fmt.Errorf("abi is not a JSON array of entries: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("abi is empty")
	}

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		ins := make([]string, 0, len(e.Inputs))
		for _, i := range e.Inputs {
			ins = append(ins, i.Type)
		}
		outs := make([]string, 0, len(e.Outputs))
		for _, o := range e.Outputs {
			outs = append(outs, o.Type)
		}
		lines = append(lines, strings.Join([]string{
			e.Type, e.Name, strings.Join(ins, ","), strings.Join(outs, ","), e.StateMutability,
		}, "\t"))
	}
	sort.Strings(lines)

	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

// RuntimeRegistryABIHash is the canonical hash of the ABI this binary implements.
// It is compared against the deployment manifest, so an image whose ABI drifted
// from the deployed contract refuses to start rather than reverting every
// registration on-chain.
func RuntimeRegistryABIHash() (string, error) {
	return CanonicalABIHash(ContentRegistryABI)
}

// RuntimeCodeReader is the chain subset the identity check needs. It is the
// signature of *ethclient.Client.CodeAt.
type RuntimeCodeReader interface {
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
}

// RuntimeCodeHash is the keccak256 of deployed runtime code, as a 0x-prefixed
// hex string — the same form `cast keccak` produces.
func RuntimeCodeHash(code []byte) string {
	return crypto.Keccak256Hash(code).Hex()
}

// BuildContentRegistryManifest assembles a record from a real deployment. It is
// the Go-side counterpart of the deploy tooling: given the deployed runtime code
// it derives the code hash, and it pins the ABI hash from the ABI this binary
// ships, so a manifest written here and a manifest written by the tooling are
// directly comparable.
func BuildContentRegistryManifest(chainID int64, address, deployTx, compiler, artifact, artifactHash string, runtimeCode []byte) (*ContentRegistryManifest, error) {
	abiHash, err := RuntimeRegistryABIHash()
	if err != nil {
		return nil, err
	}
	m := &ContentRegistryManifest{
		ChainID:         chainID,
		Address:         address,
		DeployTx:        deployTx,
		RuntimeCodeHash: RuntimeCodeHash(runtimeCode),
		ABIHash:         abiHash,
		Compiler:        compiler,
		Artifact:        artifact,
		ArtifactHash:    artifactHash,
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// VerifyIdentity checks the deployment record against the chain and the running
// image. Every check is a hard requirement:
//
//   - the manifest names the chain the process is configured for,
//   - the address holds NON-EMPTY runtime code,
//   - that code hashes to the recorded runtime code hash,
//   - the ABI this binary implements hashes to the recorded ABI hash.
func (m *ContentRegistryManifest) VerifyIdentity(ctx context.Context, reader RuntimeCodeReader, chainID int64) error {
	if reader == nil {
		return fmt.Errorf("cannot verify the content registry deployment without a chain reader")
	}
	if m.ChainID != chainID {
		return fmt.Errorf("content registry manifest is for chain %d but the process is configured for chain %d", m.ChainID, chainID)
	}
	address := common.HexToAddress(m.Address)
	code, err := reader.CodeAt(ctx, address, nil)
	if err != nil {
		return fmt.Errorf("failed to read runtime code at %s: %w", m.Address, err)
	}
	if len(code) == 0 {
		return fmt.Errorf("no runtime code at %s — the manifest names a contract that is not deployed on chain %d", m.Address, chainID)
	}
	if got := RuntimeCodeHash(code); !strings.EqualFold(got, m.RuntimeCodeHash) {
		return fmt.Errorf("runtime code hash mismatch at %s: chain has %s, manifest records %s", m.Address, got, m.RuntimeCodeHash)
	}
	runtimeABI, err := RuntimeRegistryABIHash()
	if err != nil {
		return err
	}
	if !strings.EqualFold(runtimeABI, m.ABIHash) {
		return fmt.Errorf("ABI hash mismatch: this image implements %s, the manifest records %s — the deployed contract's interface and this binary disagree", runtimeABI, m.ABIHash)
	}
	return nil
}

package web3

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/sha3"
)

// MerkleTree implements a binary Merkle tree compatible with Solidity's
// MerkleProof library (OpenZeppelin). It uses sorted pair hashing:
//
//	hash = keccak256(abi.encodePacked(min(a,b), max(a,b)))
//
// This matches OpenZeppelin's MerkleProof.sol verification on-chain.
type MerkleTree struct {
	leaves [][32]byte   // original leaf hashes
	layers [][][32]byte // all layers: [0]=leaves, [1]=parents, ..., [n]=root
	root   [32]byte
}

// NewMerkleTree constructs a Merkle tree from the given data items.
// Each item is hashed with HashLeaf before being used as a leaf.
// The number of items must be > 0; for a single item the root equals the leaf.
func NewMerkleTree(items [][]byte) (*MerkleTree, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("merkle tree: at least one item required")
	}

	leaves := make([][32]byte, len(items))
	for i, item := range items {
		leaves[i] = HashLeaf(item)
	}

	mt := &MerkleTree{leaves: leaves}
	mt.buildLayers()
	return mt, nil
}

// NewMerkleTreeFromHashes constructs a Merkle tree from pre-hashed leaves.
// Use this when leaves are already keccak256-hashed (e.g. from on-chain data).
func NewMerkleTreeFromHashes(hashes [][32]byte) (*MerkleTree, error) {
	if len(hashes) == 0 {
		return nil, fmt.Errorf("merkle tree: at least one hash required")
	}
	copied := make([][32]byte, len(hashes))
	copy(copied, hashes)
	mt := &MerkleTree{leaves: copied}
	mt.buildLayers()
	return mt, nil
}

// Root returns the Merkle root hash.
func (mt *MerkleTree) Root() [32]byte {
	return mt.root
}

// RootHex returns the Merkle root as a hex string (0x-prefixed).
func (mt *MerkleTree) RootHex() string {
	return "0x" + hex.EncodeToString(mt.root[:])
}

// Proof generates a Merkle proof for the leaf at the given index.
// Returns the sibling hashes needed to reconstruct the root.
func (mt *MerkleTree) Proof(index int) ([][32]byte, error) {
	if index < 0 || index >= len(mt.leaves) {
		return nil, fmt.Errorf("merkle proof: index %d out of range [0, %d)", index, len(mt.leaves))
	}

	var proof [][32]byte
	idx := index
	for layer := 0; layer < len(mt.layers)-1; layer++ {
		sibling := idx ^ 1 // XOR to get sibling
		if sibling < len(mt.layers[layer]) {
			proof = append(proof, mt.layers[layer][sibling])
		}
		idx /= 2
	}
	return proof, nil
}

// LeafHash returns the hash of the leaf at the given index.
func (mt *MerkleTree) LeafHash(index int) ([32]byte, error) {
	if index < 0 || index >= len(mt.leaves) {
		return [32]byte{}, fmt.Errorf("leaf index %d out of range", index)
	}
	return mt.leaves[index], nil
}

// buildLayers constructs all tree layers bottom-up.
func (mt *MerkleTree) buildLayers() {
	// Start with leaf layer
	current := make([][32]byte, len(mt.leaves))
	copy(current, mt.leaves)
	mt.layers = [][][32]byte{current}

	for len(current) > 1 {
		var parent [][32]byte
		for i := 0; i < len(current); i += 2 {
			if i+1 < len(current) {
				parent = append(parent, hashPair(current[i], current[i+1]))
			} else {
				// Odd number: promote unpaired node
				parent = append(parent, current[i])
			}
		}
		mt.layers = append(mt.layers, parent)
		current = parent
	}

	mt.root = current[0]
}

// VerifyMerkleProof verifies a Merkle proof against a known root.
// This is a standalone function that doesn't require a tree instance,
// matching the pattern used in Solidity's MerkleProof.verify().
func VerifyMerkleProof(root, leaf [32]byte, proof [][32]byte) bool {
	computed := leaf
	for _, sibling := range proof {
		computed = hashPair(computed, sibling)
	}
	return bytes.Equal(computed[:], root[:])
}

// HashLeaf hashes a data item to produce a leaf hash.
// Uses keccak256 to match Solidity: keccak256(abi.encodePacked(item))
func HashLeaf(data []byte) [32]byte {
	return keccak256(data)
}

// hashPair hashes two nodes together using sorted pair hashing
// (compatible with OpenZeppelin MerkleProof.sol):
//
//	hash = keccak256(abi.encodePacked(min(a,b), max(a,b)))
func hashPair(a, b [32]byte) [32]byte {
	if bytes.Compare(a[:], b[:]) <= 0 {
		return keccak256(append(a[:], b[:]...))
	}
	return keccak256(append(b[:], a[:]...))
}

// keccak256 computes the Keccak-256 hash.
func keccak256(data []byte) [32]byte {
	var hash [32]byte
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	copy(hash[:], h.Sum(nil))
	return hash
}

// EstimateGas returns a rough estimate of the gas cost for verifying a
// Merkle proof with the given number of proof elements on-chain.
// OpenZeppelin's MerkleProof.verify costs approximately:
//
//	base 600 + 700 * proofLength
func EstimateProofGas(proofLength int) uint64 {
	return 600 + 700*uint64(proofLength)
}

package web3

import (
	"encoding/hex"
	"testing"
)

func TestMerkleTree_SingleLeaf(t *testing.T) {
	items := [][]byte{[]byte("alice")}
	tree, err := NewMerkleTree(items)
	requireNoError(t, err)

	leaf := HashLeaf([]byte("alice"))
	if tree.Root() != leaf {
		t.Errorf("single-leaf root should equal the leaf hash")
	}

	proof, err := tree.Proof(0)
	requireNoError(t, err)
	if len(proof) != 0 {
		t.Errorf("single-leaf proof should be empty, got %d elements", len(proof))
	}

	if !VerifyMerkleProof(tree.Root(), leaf, proof) {
		t.Error("verification failed for single-leaf tree")
	}
}

func TestMerkleTree_TwoLeaves(t *testing.T) {
	items := [][]byte{[]byte("alice"), []byte("bob")}
	tree, err := NewMerkleTree(items)
	requireNoError(t, err)

	if tree.Root() == [32]byte{} {
		t.Error("root should not be zero")
	}

	for i := 0; i < 2; i++ {
		leaf := HashLeaf(items[i])
		proof, err := tree.Proof(i)
		requireNoError(t, err)
		if len(proof) != 1 {
			t.Errorf("two-leaf proof should have 1 element, got %d", len(proof))
		}
		if !VerifyMerkleProof(tree.Root(), leaf, proof) {
			t.Errorf("verification failed for leaf %d", i)
		}
	}
}

func TestMerkleTree_FourLeaves(t *testing.T) {
	items := [][]byte{
		[]byte("alice"),
		[]byte("bob"),
		[]byte("carol"),
		[]byte("dave"),
	}
	tree, err := NewMerkleTree(items)
	requireNoError(t, err)

	for i := 0; i < 4; i++ {
		leaf := HashLeaf(items[i])
		proof, err := tree.Proof(i)
		requireNoError(t, err)
		if len(proof) != 2 {
			t.Errorf("four-leaf proof should have 2 elements, got %d", len(proof))
		}
		if !VerifyMerkleProof(tree.Root(), leaf, proof) {
			t.Errorf("verification failed for leaf %d", i)
		}
	}
}

func TestMerkleTree_EightLeaves(t *testing.T) {
	items := [][]byte{
		[]byte("0"), []byte("1"), []byte("2"), []byte("3"),
		[]byte("4"), []byte("5"), []byte("6"), []byte("7"),
	}
	tree, err := NewMerkleTree(items)
	requireNoError(t, err)

	for i := 0; i < 8; i++ {
		leaf := HashLeaf(items[i])
		proof, err := tree.Proof(i)
		requireNoError(t, err)
		if len(proof) != 3 {
			t.Errorf("eight-leaf proof should have 3 elements, got %d", len(proof))
		}
		if !VerifyMerkleProof(tree.Root(), leaf, proof) {
			t.Errorf("verification failed for leaf %d", i)
		}
	}
}

func TestMerkleTree_OddLeaves(t *testing.T) {
	items := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	tree, err := NewMerkleTree(items)
	requireNoError(t, err)

	for i := 0; i < 3; i++ {
		leaf := HashLeaf(items[i])
		proof, err := tree.Proof(i)
		requireNoError(t, err)
		if !VerifyMerkleProof(tree.Root(), leaf, proof) {
			t.Errorf("verification failed for leaf %d", i)
		}
	}
}

func TestMerkleTree_WrongProof(t *testing.T) {
	items := [][]byte{[]byte("alice"), []byte("bob"), []byte("carol")}
	tree, err := NewMerkleTree(items)
	requireNoError(t, err)

	_ = HashLeaf([]byte("alice"))
	wrongLeaf := HashLeaf([]byte("eve"))
	proof, _ := tree.Proof(0)

	if VerifyMerkleProof(tree.Root(), wrongLeaf, proof) {
		t.Error("verification should fail with wrong leaf")
	}
}

func TestMerkleTree_EmptyProof(t *testing.T) {
	items := [][]byte{[]byte("alice"), []byte("bob")}
	tree, err := NewMerkleTree(items)
	requireNoError(t, err)

	leaf := HashLeaf([]byte("alice"))
	if VerifyMerkleProof(tree.Root(), leaf, nil) {
		t.Error("empty proof should not verify for multi-leaf tree")
	}
}

func TestMerkleTree_EmptyItems(t *testing.T) {
	_, err := NewMerkleTree(nil)
	if err == nil {
		t.Error("should reject empty items")
	}
}

func TestMerkleTree_OutOfRange(t *testing.T) {
	items := [][]byte{[]byte("alice")}
	tree, _ := NewMerkleTree(items)
	_, err := tree.Proof(1)
	if err == nil {
		t.Error("should reject out-of-range index")
	}
}

func TestMerkleTree_FromHashes(t *testing.T) {
	h1 := HashLeaf([]byte("alice"))
	h2 := HashLeaf([]byte("bob"))
	tree, err := NewMerkleTreeFromHashes([][32]byte{h1, h2})
	requireNoError(t, err)

	// Should produce same root as NewMerkleTree
	tree2, _ := NewMerkleTree([][]byte{[]byte("alice"), []byte("bob")})
	if tree.Root() != tree2.Root() {
		t.Error("NewMerkleTreeFromHashes should produce same root")
	}
}

func TestMerkleTree_RootHex(t *testing.T) {
	items := [][]byte{[]byte("alice")}
	tree, _ := NewMerkleTree(items)
	hex := tree.RootHex()
	if len(hex) != 66 { // "0x" + 64 hex chars
		t.Errorf("root hex should be 66 chars, got %d", len(hex))
	}
	if hex[:2] != "0x" {
		t.Error("root hex should be 0x-prefixed")
	}
}

func TestMerkleTree_Deterministic(t *testing.T) {
	items := [][]byte{[]byte("alice"), []byte("bob")}
	tree1, _ := NewMerkleTree(items)
	tree2, _ := NewMerkleTree(items)
	if tree1.Root() != tree2.Root() {
		t.Error("same items should produce same root")
	}
}

func TestMerkleTree_LargeTree(t *testing.T) {
	var items [][]byte
	for i := 0; i < 100; i++ {
		items = append(items, []byte(hex.EncodeToString([]byte{byte(i)})))
	}
	tree, err := NewMerkleTree(items)
	requireNoError(t, err)

	// Verify every leaf
	for i := 0; i < 100; i++ {
		leaf := HashLeaf(items[i])
		proof, err := tree.Proof(i)
		requireNoError(t, err)
		if !VerifyMerkleProof(tree.Root(), leaf, proof) {
			t.Errorf("verification failed for leaf %d", i)
		}
	}
}

func TestEstimateProofGas(t *testing.T) {
	tests := []struct {
		proofLen int
		wantMin uint64
	}{
		{0, 600},
		{1, 1300},
		{5, 4100},
		{10, 7600},
		{20, 14600},
	}
	for _, tt := range tests {
		gas := EstimateProofGas(tt.proofLen)
		if gas < tt.wantMin {
			t.Errorf("EstimateProofGas(%d) = %d, want >= %d", tt.proofLen, gas, tt.wantMin)
		}
	}
}

func TestHashLeaf_Deterministic(t *testing.T) {
	h1 := HashLeaf([]byte("test"))
	h2 := HashLeaf([]byte("test"))
	if h1 != h2 {
		t.Error("HashLeaf should be deterministic")
	}
	h3 := HashLeaf([]byte("other"))
	if h1 == h3 {
		t.Error("different inputs should produce different hashes")
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

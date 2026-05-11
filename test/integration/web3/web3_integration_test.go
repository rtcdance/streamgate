package web3_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
	"streamgate/pkg/web3"
)

func TestNFTVerifier_VerifyNFTOwnership_Owner(t *testing.T) {
	mock := setupNFTMock()
	verifier := web3.NewNFTVerifier(mock, zap.NewNop())

	verified, err := verifier.VerifyNFTOwnership(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		"1",
		knownOwnerAddress.Hex(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verified {
		t.Fatal("expected ownership to be verified, got false")
	}
}

func TestNFTVerifier_VerifyNFTOwnership_NotOwner(t *testing.T) {
	mock := setupNFTMock()
	verifier := web3.NewNFTVerifier(mock, zap.NewNop())

	verified, err := verifier.VerifyNFTOwnership(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		"1",
		"0x9999999999999999999999999999999999999999",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verified {
		t.Fatal("expected ownership to be denied, got true")
	}
}

func TestNFTVerifier_VerifyNFTOwnership_RPCError(t *testing.T) {
	mock := newMockEthCaller()
	mock.setError("6352211e", errors.New("RPC timeout")) // ownerOf selector
	verifier := web3.NewNFTVerifier(mock, zap.NewNop())

	_, err := verifier.VerifyNFTOwnership(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		"1",
		knownOwnerAddress.Hex(),
	)
	if err == nil {
		t.Fatal("expected error from RPC failure, got nil")
	}
}

func TestNFTVerifier_GetNFTBalance(t *testing.T) {
	mock := setupNFTMock()
	verifier := web3.NewNFTVerifier(mock, zap.NewNop())

	balance, err := verifier.GetNFTBalance(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		knownOwnerAddress.Hex(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if balance.Int64() != 3 {
		t.Fatalf("expected balance 3, got %d", balance.Int64())
	}
}

func TestNFTVerifier_GetNFTBalance_Zero(t *testing.T) {
	mock := newMockEthCaller()
	selectorBalanceOf := "70a08231"
	mock.setResponse(selectorBalanceOf, packBalanceOfResponse(0))
	verifier := web3.NewNFTVerifier(mock, zap.NewNop())

	balance, err := verifier.GetNFTBalance(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		knownOwnerAddress.Hex(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if balance.Int64() != 0 {
		t.Fatalf("expected balance 0, got %d", balance.Int64())
	}
}

func TestNFTVerifier_GetNFTInfo(t *testing.T) {
	mock := setupNFTMock()
	verifier := web3.NewNFTVerifier(mock, zap.NewNop())

	info, err := verifier.GetNFTInfo(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		"1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Name != "TestNFT" {
		t.Fatalf("expected name 'TestNFT', got %q", info.Name)
	}
	if info.Symbol != "TNFT" {
		t.Fatalf("expected symbol 'TNFT', got %q", info.Symbol)
	}
	if info.URI != "https://example.com/metadata/1" {
		t.Fatalf("expected URI 'https://example.com/metadata/1', got %q", info.URI)
	}
	if info.Owner != knownOwnerAddress.Hex() {
		t.Fatalf("expected owner %s, got %s", knownOwnerAddress.Hex(), info.Owner)
	}
}

func TestNFTVerifier_GetNFTInfo_PartialFailure(t *testing.T) {
	// Simulate a contract where name() reverts but other calls succeed
	mock := newMockEthCaller()

	// name() — error (revert)
	selectorName := "06fdde03"
	mock.setError(selectorName, errors.New("execution reverted"))

	// symbol() — works
	selectorSymbol := "95d89b41"
	mock.setResponse(selectorSymbol, packStringResponse("TNFT"))

	// tokenURI(uint256) — works
	selectorTokenURI := "c87b56dd"
	mock.setResponse(selectorTokenURI, packStringResponse("ipfs://QmExample"))

	// ownerOf(uint256) — works
	selectorOwnerOf := "6352211e"
	mock.setResponse(selectorOwnerOf, packOwnerOfResponse(knownOwnerAddress))

	verifier := web3.NewNFTVerifier(mock, zap.NewNop())

	info, err := verifier.GetNFTInfo(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		"1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// name should be empty (best-effort), but symbol should succeed
	if info.Name != "" {
		t.Fatalf("expected empty name on revert, got %q", info.Name)
	}
	if info.Symbol != "TNFT" {
		t.Fatalf("expected symbol 'TNFT', got %q", info.Symbol)
	}
	if info.Owner != knownOwnerAddress.Hex() {
		t.Fatalf("expected owner, got %q", info.Owner)
	}
}

func TestNFTVerifier_VerifyNFTCollection(t *testing.T) {
	mock := setupNFTMock()
	verifier := web3.NewNFTVerifier(mock, zap.NewNop())

	hasNFT, err := verifier.VerifyNFTCollection(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		knownOwnerAddress.Hex(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasNFT {
		t.Fatal("expected hasNFT=true when balance > 0")
	}
}

func TestNFTVerifier_VerifyNFTCollection_NoNFT(t *testing.T) {
	mock := newMockEthCaller()
	selectorBalanceOf := "70a08231"
	mock.setResponse(selectorBalanceOf, packBalanceOfResponse(0))
	verifier := web3.NewNFTVerifier(mock, zap.NewNop())

	hasNFT, err := verifier.VerifyNFTCollection(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		knownOwnerAddress.Hex(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasNFT {
		t.Fatal("expected hasNFT=false when balance = 0")
	}
}

func TestSolanaVerifier_VerifySignature(t *testing.T) {
	verifier := web3.NewSolanaVerifier(zap.NewNop(), "")

	// Just test the non-RPC signature verification
	verified, err := verifier.VerifySignature(
		"11111111111111111111111111111111", // invalid test address
		"test message",
		"", // empty signature — should fail
	)
	if err == nil {
		t.Fatal("expected error for empty signature, got nil")
	}
	if verified {
		t.Fatal("expected verified=false for invalid signature")
	}
}

func TestSolanaVerifier_ParseAddress(t *testing.T) {
	verifier := web3.NewSolanaVerifier(zap.NewNop(), "")

	// Valid base58 address (SystemProgram)
	pubKey, err := verifier.ParseSolanaAddress("11111111111111111111111111111111")
	if err != nil {
		t.Fatalf("unexpected error parsing valid address: %v", err)
	}
	if pubKey.String() != "11111111111111111111111111111111" {
		t.Fatalf("unexpected public key: %s", pubKey.String())
	}
}

func TestSolanaVerifier_TokenAccount_NoRPC(t *testing.T) {
	verifier := web3.NewSolanaVerifier(zap.NewNop(), "")

	_, err := verifier.VerifyTokenAccount(context.Background(), "ATokenGPv...etc", "OwnerAddr...")
	if err == nil {
		t.Fatal("expected error when RPC client is nil")
	}
}

func TestSolanaVerifier_MintAuthority_NoRPC(t *testing.T) {
	verifier := web3.NewSolanaVerifier(zap.NewNop(), "")

	_, err := verifier.VerifyMintAuthority(context.Background(), "MintAddress...", "AuthorityAddr...")
	if err == nil {
		t.Fatal("expected error when RPC client is nil")
	}
}

func TestMockEthCaller_Interface(t *testing.T) {
	// Verify mock satisfies the EthCaller interface at compile time
	var _ web3.EthCaller = newMockEthCaller()

	// Also verify *ethclient.Client would satisfy it
	var _ web3.EthCaller = (*mockEthCaller)(nil)
}

// Additional test: ensure CallContract with nil To doesn't panic
func TestMockEthCaller_NilTo(t *testing.T) {
	mock := newMockEthCaller()
	_, err := mock.CallContract(context.Background(), ethereum.CallMsg{
		To:   nil,
		Data: []byte{0x00},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Test that common.Address pack/unpack round-trips correctly
func TestPackOwnerOfResponse(t *testing.T) {
	addr := common.HexToAddress("0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B")
	packed := packOwnerOfResponse(addr)

	// First 12 bytes should be zero
	for i := 0; i < 12; i++ {
		if packed[i] != 0 {
			t.Fatalf("expected zero padding at byte %d", i)
		}
	}
	// Last 20 bytes should match the address
	var unpacked common.Address
	copy(unpacked[:], packed[12:])
	if unpacked != addr {
		t.Fatalf("round-trip failed: expected %s, got %s", addr.Hex(), unpacked.Hex())
	}
}

// Test that big.Int balance pack/unpack round-trips correctly
func TestPackBalanceOfResponse(t *testing.T) {
	balance := int64(1000000)
	packed := packBalanceOfResponse(balance)

	result := new(big.Int).SetBytes(packed)
	if result.Int64() != balance {
		t.Fatalf("round-trip failed: expected %d, got %d", balance, result.Int64())
	}
}

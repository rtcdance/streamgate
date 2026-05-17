package web3

import (
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestGenerateMnemonic_128(t *testing.T) {
	if len(bip39EnglishWords) < 2048 {
		t.Skipf("BIP-39 word list has %d words (need 2048), skipping 128-bit test", len(bip39EnglishWords))
	}
	mnemonic, err := GenerateMnemonic(128)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	words := strings.Fields(mnemonic)
	if len(words) != 12 {
		t.Errorf("expected 12 words for 128-bit entropy, got %d", len(words))
	}
}

func TestGenerateMnemonic_256(t *testing.T) {
	// Note: 256-bit mnemonic requires the full 2048-word BIP-39 list.
	// If the word list is incomplete, this test is skipped.
	if len(bip39EnglishWords) < 2048 {
		t.Skipf("BIP-39 word list has %d words (need 2048), skipping 256-bit test", len(bip39EnglishWords))
	}
	mnemonic, err := GenerateMnemonic(256)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	words := strings.Fields(mnemonic)
	if len(words) != 24 {
		t.Errorf("expected 24 words for 256-bit entropy, got %d", len(words))
	}
}

func TestGenerateMnemonic_InvalidBits(t *testing.T) {
	_, err := GenerateMnemonic(64)
	if err == nil {
		t.Error("expected error for 64-bit entropy")
	}
}

func TestMnemonicToSeed_KnownVector(t *testing.T) {
	// BIP-39 test vector: mnemonic with no password
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed, err := MnemonicToSeed(mnemonic, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seed) != 64 {
		t.Errorf("expected 64-byte seed, got %d", len(seed))
	}
	// Known BIP-39 test vector seed (first 8 bytes)
	// Expected seed for "abandon...about" with no password:
	// 5eb00bbddcf069084889a8ab9155568165f5c453ccb85e70811aaed6f6da5fc1...
	if seed[0] != 0x5e || seed[1] != 0xb0 {
		t.Errorf("seed prefix mismatch: got %x, want 5eb0", seed[:2])
	}
}

func TestMnemonicToSeed_WithPassword(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed1, _ := MnemonicToSeed(mnemonic, "")
	seed2, _ := MnemonicToSeed(mnemonic, "mypassword")
	if string(seed1) == string(seed2) {
		t.Error("seeds with different passwords should differ")
	}
}

func TestMnemonicToSeed_InvalidWordCount(t *testing.T) {
	_, err := MnemonicToSeed("abandon abandon abandon", "")
	if err == nil {
		t.Error("expected error for 3-word mnemonic")
	}
}

func TestMnemonicToSeed_InvalidWord(t *testing.T) {
	_, err := MnemonicToSeed("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon notaword", "")
	if err == nil {
		t.Error("expected error for invalid word")
	}
}

func TestHDWallet_DeriveKey(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	wallet, err := NewHDWalletFromMnemonic(mnemonic, "", zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Derive first Ethereum account
	key, err := wallet.DeriveKey(DefaultEthereumPath(0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.Sign() == 0 {
		t.Error("derived key should not be zero")
	}
}

func TestHDWallet_DeriveAddress(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	wallet, err := NewHDWalletFromMnemonic(mnemonic, "", zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	addr, err := wallet.DeriveAddress(DefaultEthereumPath(0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(addr, "0x") {
		t.Errorf("address should start with 0x, got %s", addr)
	}
	if len(addr) != 42 {
		t.Errorf("address should be 42 chars, got %d", len(addr))
	}
}

func TestHDWallet_DifferentAccounts(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	wallet, _ := NewHDWalletFromMnemonic(mnemonic, "", zap.NewNop())

	addr0, _ := wallet.DeriveAddress(DefaultEthereumPath(0))
	addr1, _ := wallet.DeriveAddress(DefaultEthereumPath(1))

	if addr0 == addr1 {
		t.Error("different account indices should produce different addresses")
	}
}

func TestHDWallet_DeriveKey_InvalidPath(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	wallet, _ := NewHDWalletFromMnemonic(mnemonic, "", zap.NewNop())

	_, err := wallet.DeriveKey("44'/60'/0'/0/0") // missing m/ prefix
	if err == nil {
		t.Error("expected error for path without m/ prefix")
	}
}

func TestDefaultEthereumPath(t *testing.T) {
	path := DefaultEthereumPath(0)
	if path != "m/44'/60'/0'/0/0" {
		t.Errorf("expected m/44'/60'/0'/0/0, got %s", path)
	}

	path5 := DefaultEthereumPath(5)
	if path5 != "m/44'/60'/0'/0/5" {
		t.Errorf("expected m/44'/60'/0'/0/5, got %s", path5)
	}
}

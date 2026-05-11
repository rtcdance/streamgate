package web3

import (
	"crypto/ecdsa"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

func TestSecurePrivateKey_UseKey(t *testing.T) {
	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	secure, err := NewSecurePrivateKey(privKey)
	if err != nil {
		t.Fatalf("failed to create secure key: %v", err)
	}

	var usedKey *ecdsa.PrivateKey
	err = secure.UseKey(func(k *ecdsa.PrivateKey) error {
		usedKey = k
		return nil
	})
	if err != nil {
		t.Fatalf("UseKey failed: %v", err)
	}

	if usedKey.D.Cmp(privKey.D) != 0 {
		t.Error("decrypted key does not match original")
	}
}

func TestSecurePrivateKey_NilKey(t *testing.T) {
	_, err := NewSecurePrivateKey(nil)
	if err == nil {
		t.Error("expected error for nil key")
	}
}

func TestSecurePrivateKey_CallbackError(t *testing.T) {
	privKey, _ := crypto.GenerateKey()
	secure, _ := NewSecurePrivateKey(privKey)

	err := secure.UseKey(func(k *ecdsa.PrivateKey) error {
		return fmt.Errorf("test error")
	})
	if err == nil {
		t.Error("expected error from callback")
	}
}

func TestZeroize(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	Zeroize(data)
	for _, b := range data {
		if b != 0 {
			t.Error("bytes not zeroed")
		}
	}
}

func TestZeroizeKey(t *testing.T) {
	privKey, _ := crypto.GenerateKey()
	ZeroizeKey(privKey)
	// After zeroing, D should be all zeros
	for _, word := range privKey.D.Bits() {
		if word != 0 {
			t.Error("key D value not zeroed")
		}
	}
}

func TestZeroizeKey_Nil(t *testing.T) {
	ZeroizeKey(nil) // should not panic
	ZeroizeKey(&ecdsa.PrivateKey{}) // D is nil, should not panic
}

func TestWalletManager_ImportFromMnemonic(t *testing.T) {
	wm := NewWalletManager(zap.NewNop())

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	wallet, err := wm.ImportFromMnemonic(mnemonic, "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wallet.Address == "" {
		t.Error("wallet address should not be empty")
	}
}

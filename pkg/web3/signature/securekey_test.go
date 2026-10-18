package signature

import (
	"crypto/ecdsa"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// TestNewSecurePrivateKeyFromHex_AcceptsTheDocumentedPrefix pins a real defect
// the durable acceptance found: the config validator, the registry signer and
// the demo minter all accept a 0x-prefixed key, so the secure-key constructor
// must not be the one consumer that refuses the value the operator was told to
// set. A 0x-prefixed key made the whole gateway refuse to start.
func TestNewSecurePrivateKeyFromHex_AcceptsTheDocumentedPrefix(t *testing.T) {
	const bare = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	want, err := crypto.HexToECDSA(bare)
	if err != nil {
		t.Fatalf("fixture key is unusable: %v", err)
	}

	// The same forms the config validator accepts. An uppercase 0X is NOT in
	// that set — the validator trims lowercase "0x" only, so it rejects "0X…"
	// on length — and this constructor stays consistent with it rather than
	// inventing a second, laxer rule.
	for _, tt := range []string{bare, "0x" + bare, "  0x" + bare + "  "} {
		sk, err := NewSecurePrivateKeyFromHex(tt)
		if err != nil {
			t.Fatalf("form %q must be accepted: %v", tt, err)
		}
		err = sk.UseKey(func(k *ecdsa.PrivateKey) error {
			if k.D.Cmp(want.D) != 0 {
				t.Errorf("form %q produced a different key", tt)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("UseKey failed for %q: %v", tt, err)
		}
	}

	if _, err := NewSecurePrivateKeyFromHex("0xnot-hex"); err == nil {
		t.Error("a non-hex key must still be refused")
	}
}

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
	for _, word := range privKey.D.Bits() {
		if word != 0 {
			t.Error("key D value not zeroed")
		}
	}
}

func TestZeroizeKey_Nil(t *testing.T) {
	ZeroizeKey(nil)
	ZeroizeKey(&ecdsa.PrivateKey{})
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

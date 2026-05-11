package web3

import (
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

func TestEncryptDecryptPrivateKey(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Encrypt with light scrypt params for test speed
	encrypted, err := EncryptPrivateKey(privateKey, "testpassword", 4096, 6)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}

	// Decrypt
	decrypted, err := DecryptPrivateKey(encrypted, "testpassword")
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}

	// Verify same key by comparing D values
	if privateKey.D.Cmp(decrypted.D) != 0 {
		t.Error("decrypted key does not match original")
	}
}

func TestDecryptWithWrongPassword(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()
	encrypted, _ := EncryptPrivateKey(privateKey, "correctpass", 4096, 6)

	_, err := DecryptPrivateKey(encrypted, "wrongpass")
	if err == nil {
		t.Error("should fail with wrong password")
	}
}

func TestValidateKeystoreJSON_Valid(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()
	encrypted, _ := EncryptPrivateKey(privateKey, "pass", 4096, 6)

	if err := ValidateKeystoreJSON(encrypted); err != nil {
		t.Errorf("valid keystore JSON should pass validation: %v", err)
	}
}

func TestValidateKeystoreJSON_InvalidJSON(t *testing.T) {
	err := ValidateKeystoreJSON([]byte("not json"))
	if err == nil {
		t.Error("should reject invalid JSON")
	}
}

func TestValidateKeystoreJSON_MissingVersion(t *testing.T) {
	err := ValidateKeystoreJSON([]byte(`{"crypto":{}}`))
	if err == nil {
		t.Error("should reject missing version field")
	}
}

func TestKeystoreManager_ImportFromKeystore(t *testing.T) {
	km := NewKeystoreManager(zap.NewNop())
	privateKey, _ := crypto.GenerateKey()
	encrypted, _ := EncryptPrivateKey(privateKey, "mypass", 4096, 6)

	decrypted, address, err := km.ImportFromKeystore(encrypted, "mypass")
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if privateKey.D.Cmp(decrypted.D) != 0 {
		t.Error("imported key does not match")
	}
	if address == "" {
		t.Error("address should not be empty")
	}
}

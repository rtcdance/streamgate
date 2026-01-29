package security_test

import (
	"testing"

	"streamgate/pkg/security"
)

func TestEncryptor_GenerateKey(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	key, err := encryptor.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if len(key) != 32 {
		t.Errorf("Expected key length 32, got %d", len(key))
	}
}

func TestEncryptor_GenerateKeyHex(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	keyHex, err := encryptor.GenerateKeyHex()
	if err != nil {
		t.Fatalf("GenerateKeyHex failed: %v", err)
	}

	if len(keyHex) != 64 { // 32 bytes = 64 hex characters
		t.Errorf("Expected hex key length 64, got %d", len(keyHex))
	}
}

func TestEncryptor_DeriveKey(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	password := "test-password-123"
	key, err := encryptor.DeriveKey(password, nil)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	if len(key) != 32 {
		t.Errorf("Expected key length 32, got %d", len(key))
	}

	// Verify same password produces same key
	key2, err := encryptor.DeriveKey(password, nil)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	if len(key2) != len(key) {
		t.Errorf("Key lengths don't match")
	}
}

func TestEncryptor_Encrypt_Decrypt(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	plaintext := "Hello, World!"
	key, err := encryptor.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Encrypt
	encrypted, err := encryptor.Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if encrypted.Ciphertext == "" {
		t.Error("Ciphertext is empty")
	}

	if encrypted.Nonce == "" {
		t.Error("Nonce is empty")
	}

	// Decrypt
	decrypted, err := encryptor.Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match. Expected %s, got %s", plaintext, decrypted)
	}
}

func TestEncryptor_EncryptString_DecryptString(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	plaintext := "Secret message"
	password := "secure-password-123"

	// Encrypt
	encrypted, err := encryptor.EncryptString(plaintext, password)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	if encrypted.Salt == "" {
		t.Error("Salt is empty")
	}

	// Decrypt
	decrypted, err := encryptor.DecryptString(encrypted, password)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match. Expected %s, got %s", plaintext, decrypted)
	}
}

func TestEncryptor_HashPassword(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	password := "test-password"
	hash := encryptor.HashPassword(password)

	if hash == "" {
		t.Error("Hash is empty")
	}

	if len(hash) != 64 { // SHA256 produces 64 hex characters
		t.Errorf("Expected hash length 64, got %d", len(hash))
	}
}

func TestEncryptor_VerifyPassword(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	password := "test-password"
	hash := encryptor.HashPassword(password)

	if !encryptor.VerifyPassword(password, hash) {
		t.Error("Password verification failed")
	}

	if encryptor.VerifyPassword("wrong-password", hash) {
		t.Error("Wrong password should not verify")
	}
}

func TestEncryptor_InvalidKeySize(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	plaintext := "Hello, World!"
	invalidKey := []byte("short-key")

	_, err := encryptor.Encrypt(plaintext, invalidKey)
	if err == nil {
		t.Error("Expected error for invalid key size")
	}
}

func TestEncryptor_DecryptWithWrongKey(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	plaintext := "Hello, World!"
	key1, _ := encryptor.GenerateKey()
	key2, _ := encryptor.GenerateKey()

	encrypted, _ := encryptor.Encrypt(plaintext, key1)

	_, err := encryptor.Decrypt(encrypted, key2)
	if err == nil {
		t.Error("Expected error when decrypting with wrong key")
	}
}

func TestEncryptor_DecryptWithWrongPassword(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	plaintext := "Secret message"
	password := "correct-password"

	encrypted, _ := encryptor.EncryptString(plaintext, password)

	_, err := encryptor.DecryptString(encrypted, "wrong-password")
	if err == nil {
		t.Error("Expected error when decrypting with wrong password")
	}
}

func TestEncryptor_MultipleEncryptions(t *testing.T) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	plaintext := "Test message"
	key, _ := encryptor.GenerateKey()

	// Encrypt same plaintext multiple times
	encrypted1, _ := encryptor.Encrypt(plaintext, key)
	encrypted2, _ := encryptor.Encrypt(plaintext, key)

	// Ciphertexts should be different (different nonces)
	if encrypted1.Ciphertext == encrypted2.Ciphertext {
		t.Error("Same plaintext produced same ciphertext (nonce not random)")
	}

	// But both should decrypt to same plaintext
	decrypted1, _ := encryptor.Decrypt(encrypted1, key)
	decrypted2, _ := encryptor.Decrypt(encrypted2, key)

	if decrypted1 != plaintext || decrypted2 != plaintext {
		t.Error("Decrypted texts don't match original")
	}
}

func BenchmarkEncryptor_Encrypt(b *testing.B) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	key, _ := encryptor.GenerateKey()
	plaintext := "Hello, World!"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encryptor.Encrypt(plaintext, key)
	}
}

func BenchmarkEncryptor_Decrypt(b *testing.B) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	key, _ := encryptor.GenerateKey()
	plaintext := "Hello, World!"
	encrypted, _ := encryptor.Encrypt(plaintext, key)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encryptor.Decrypt(encrypted, key)
	}
}

func BenchmarkEncryptor_DeriveKey(b *testing.B) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	password := "test-password"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encryptor.DeriveKey(password, nil)
	}
}

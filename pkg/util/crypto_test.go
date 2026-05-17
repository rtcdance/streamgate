package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCrypto_HashPassword(t *testing.T) {
	password := "testpassword123"

	// Hash password
	hash, err := HashPassword(password)
	require.NoError(t, err)
	require.NotEqual(t, "", hash)
	require.NotEqual(t, password, hash)
}

func TestCrypto_VerifyPassword(t *testing.T) {
	password := "testpassword123"

	// Hash password
	hash, err := HashPassword(password)
	require.NoError(t, err)

	// Verify correct password
	valid := VerifyPassword(password, hash)
	require.True(t, valid)

	// Verify wrong password
	valid = VerifyPassword("wrongpassword", hash)
	require.False(t, valid)
}

func TestCrypto_GenerateRandomString(t *testing.T) {
	// Generate random strings
	str1, err := GenerateRandomString(32)
	require.NoError(t, err)
	require.Equal(t, 32, len(str1))

	str2, err := GenerateRandomString(32)
	require.NoError(t, err)
	require.Equal(t, 32, len(str2))

	// Should be different
	require.NotEqual(t, str1, str2)
}

func TestCrypto_SHA256Hash(t *testing.T) {
	data := []byte("test data")

	// Hash data
	hash := SHA256Hash(data)
	require.NotEqual(t, "", hash)
	require.Equal(t, 64, len(hash)) // SHA256 hex is 64 chars

	// Same data should produce same hash
	hash2 := SHA256Hash(data)
	require.Equal(t, hash, hash2)

	// Different data should produce different hash
	hash3 := SHA256Hash([]byte("different data"))
	require.NotEqual(t, hash, hash3)
}

func TestCrypto_EncryptDecrypt(t *testing.T) {
	plaintext := "sensitive data"
	key := "12345678901234567890123456789012"

	// Encrypt
	ciphertext, err := Encrypt([]byte(plaintext), []byte(key))
	require.NoError(t, err)
	require.NotEqual(t, plaintext, ciphertext)

	// Decrypt
	decrypted, err := Decrypt(ciphertext, []byte(key))
	require.NoError(t, err)
	require.Equal(t, plaintext, string(decrypted))
}

func TestCrypto_EncryptDecrypt_WrongKey(t *testing.T) {
	plaintext := "sensitive data"
	key1 := "12345678901234567890123456789012"
	key2 := "98765432109876543210987654321098"

	// Encrypt with key1
	ciphertext, err := Encrypt([]byte(plaintext), []byte(key1))
	require.NoError(t, err)

	// Try to decrypt with key2
	_, err = Decrypt(ciphertext, []byte(key2))
	require.Error(t, err)
}

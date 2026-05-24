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

func TestCrypto_EncryptAES_BadKeyLength(t *testing.T) {
	_, err := EncryptAES([]byte("data"), []byte("short"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "key must be 32 bytes")
}

func TestCrypto_DecryptAES_BadKeyLength(t *testing.T) {
	_, err := DecryptAES("ciphertext", []byte("short"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "key must be 32 bytes")
}

func TestCrypto_DecryptAES_BadHex(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	_, err := DecryptAES("not-valid-hex!@#", key)
	require.Error(t, err)
}

func TestCrypto_DecryptAES_CiphertextTooShort(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	_, err := DecryptAES("ab", key)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ciphertext too short")
}

func TestCrypto_EncryptAES_DecryptAES_RoundTrip(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	plaintext := []byte("round trip test data")

	ciphertext, err := EncryptAES(plaintext, key)
	require.NoError(t, err)

	decrypted, err := DecryptAES(ciphertext, key)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)
}

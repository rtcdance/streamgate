package util_test

import (
	"testing"

	"streamgate/pkg/util"
	"streamgate/test/helpers"
)

func TestCrypto_HashPassword(t *testing.T) {
	password := "testpassword123"

	// Hash password
	hash, err := util.HashPassword(password)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, "", hash)
	helpers.AssertNotEqual(t, password, hash)
}

func TestCrypto_VerifyPassword(t *testing.T) {
	password := "testpassword123"

	// Hash password
	hash, err := util.HashPassword(password)
	helpers.AssertNoError(t, err)

	// Verify correct password
	valid := util.VerifyPassword(password, hash)
	helpers.AssertTrue(t, valid)

	// Verify wrong password
	valid = util.VerifyPassword("wrongpassword", hash)
	helpers.AssertFalse(t, valid)
}

func TestCrypto_GenerateRandomString(t *testing.T) {
	// Generate random strings
	str1, err := util.GenerateRandomString(32)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, 32, len(str1))

	str2, err := util.GenerateRandomString(32)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, 32, len(str2))

	// Should be different
	helpers.AssertNotEqual(t, str1, str2)
}

func TestCrypto_SHA256Hash(t *testing.T) {
	data := []byte("test data")

	// Hash data
	hash := util.SHA256Hash(data)
	helpers.AssertNotEqual(t, "", hash)
	helpers.AssertEqual(t, 64, len(hash)) // SHA256 hex is 64 chars

	// Same data should produce same hash
	hash2 := util.SHA256Hash(data)
	helpers.AssertEqual(t, hash, hash2)

	// Different data should produce different hash
	hash3 := util.SHA256Hash([]byte("different data"))
	helpers.AssertNotEqual(t, hash, hash3)
}

func TestCrypto_EncryptDecrypt(t *testing.T) {
	plaintext := "sensitive data"
	key := "12345678901234567890123456789012"

	// Encrypt
	ciphertext, err := util.Encrypt([]byte(plaintext), []byte(key))
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, plaintext, ciphertext)

	// Decrypt
	decrypted, err := util.Decrypt(ciphertext, []byte(key))
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, plaintext, string(decrypted))
}

func TestCrypto_EncryptDecrypt_WrongKey(t *testing.T) {
	plaintext := "sensitive data"
	key1 := "12345678901234567890123456789012"
	key2 := "98765432109876543210987654321098"

	// Encrypt with key1
	ciphertext, err := util.Encrypt([]byte(plaintext), []byte(key1))
	helpers.AssertNoError(t, err)

	// Try to decrypt with key2
	_, err = util.Decrypt(ciphertext, []byte(key2))
	helpers.AssertError(t, err)
}

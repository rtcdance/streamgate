package util

import (
	"crypto/sha256"
	"encoding/hex"
	"golang.org/x/crypto/bcrypt"
)

// SHA256Hash computes SHA256 hash of data
func SHA256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// SHA256 is an alias for SHA256Hash for compatibility
func SHA256(data []byte) string {
	return SHA256Hash(data)
}

// HashSHA256 is an alias for SHA256Hash for compatibility
func HashSHA256(data []byte) string {
	return SHA256Hash(data)
}

// VerifySHA256 verifies SHA256 hash
func VerifySHA256(data []byte, expectedHash string) bool {
	return SHA256Hash(data) == expectedHash
}

// HashString computes SHA256 hash of string
func HashString(s string) string {
	return SHA256Hash([]byte(s))
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// VerifyPassword verifies a password against a hash
func VerifyPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

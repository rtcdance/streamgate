package util

import (
	"crypto/sha256"
	"encoding/hex"
)

// SHA256Hash computes SHA256 hash of data
func SHA256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// VerifySHA256 verifies SHA256 hash
func VerifySHA256(data []byte, expectedHash string) bool {
	return SHA256Hash(data) == expectedHash
}

// HashString computes SHA256 hash of string
func HashString(s string) string {
	return SHA256Hash([]byte(s))
}

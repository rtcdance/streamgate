package util

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"html"
	"strings"
	"unicode"
)

// TrimSpace trims leading and trailing whitespace
func TrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// Trim is an alias for TrimSpace for compatibility
func Trim(s string) string {
	return strings.TrimSpace(s)
}

// ToLower converts string to lowercase
func ToLower(s string) string {
	return strings.ToLower(s)
}

// ToUpper converts string to uppercase
func ToUpper(s string) string {
	return strings.ToUpper(s)
}

// Contains checks if string contains substring
func Contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// HasPrefix checks if string has prefix
func HasPrefix(s, prefix string) bool {
	return strings.HasPrefix(s, prefix)
}

// HasSuffix checks if string has suffix
func HasSuffix(s, suffix string) bool {
	return strings.HasSuffix(s, suffix)
}

// Split splits string by separator
func Split(s, sep string) []string {
	return strings.Split(s, sep)
}

// Join joins strings with separator
func Join(strs []string, sep string) string {
	return strings.Join(strs, sep)
}

// IsAlphanumeric checks if string is alphanumeric
func IsAlphanumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// Truncate truncates string to max length
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// GenerateRandomString generates a random string of specified length
func GenerateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}

// Base64Encode encodes data to base64 string
func Base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// Base64Decode decodes base64 string to data
func Base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// HexEncode encodes data to hex string
func HexEncode(data []byte) string {
	return hex.EncodeToString(data)
}

// HexDecode decodes hex string to data
func HexDecode(s string) ([]byte, error) {
	return hex.DecodeString(s)
}

// SanitizeInput sanitizes user input by escaping HTML
func SanitizeInput(s string) string {
	return html.EscapeString(strings.TrimSpace(s))
}

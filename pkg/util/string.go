package util

import (
	"strings"
	"unicode"
)

// TrimSpace trims leading and trailing whitespace
func TrimSpace(s string) string {
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

package util

import (
	"fmt"
	"regexp"
)

// ValidateEmail validates email format
func ValidateEmail(email string) error {
	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
	matched, err := regexp.MatchString(pattern, email)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid email format")
	}
	return nil
}

// IsValidEmail checks if email is valid (returns bool)
func IsValidEmail(email string) bool {
	return ValidateEmail(email) == nil
}

// ValidateURL validates URL format
func ValidateURL(url string) error {
	pattern := `^https?://[^\s/$.?#].[^\s]*$`
	matched, err := regexp.MatchString(pattern, url)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid URL format")
	}
	return nil
}

// IsValidURL checks if URL is valid (returns bool)
func IsValidURL(url string) bool {
	return ValidateURL(url) == nil
}

// ValidateUUID validates UUID format
func ValidateUUID(uuid string) error {
	pattern := `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	matched, err := regexp.MatchString(pattern, uuid)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid UUID format")
	}
	return nil
}

// IsValidUUID checks if UUID is valid (returns bool)
func IsValidUUID(uuid string) bool {
	return ValidateUUID(uuid) == nil
}

// ValidateEthereumAddress validates Ethereum address format
func ValidateEthereumAddress(address string) error {
	pattern := `^0x[0-9a-fA-F]{40}$`
	matched, err := regexp.MatchString(pattern, address)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid Ethereum address format")
	}
	return nil
}

// IsValidAddress checks if Ethereum address is valid (returns bool)
func IsValidAddress(address string) bool {
	return ValidateEthereumAddress(address) == nil
}

// ValidateHash validates hash format (64 hex characters)
func ValidateHash(hash string) error {
	pattern := `^[0-9a-fA-F]{64}$`
	matched, err := regexp.MatchString(pattern, hash)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid hash format")
	}
	return nil
}

// IsValidHash checks if hash is valid (returns bool)
func IsValidHash(hash string) bool {
	return ValidateHash(hash) == nil
}

// IsValidJSON checks if string is valid JSON
func IsValidJSON(s string) bool {
	// Simple check - just verify it's not empty and has braces or brackets
	s = TrimSpace(s)
	if len(s) == 0 {
		return false
	}
	return (s[0] == '{' && s[len(s)-1] == '}') || (s[0] == '[' && s[len(s)-1] == ']')
}

// ValidateNotEmpty validates string is not empty
func ValidateNotEmpty(s string, fieldName string) error {
	if s == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	return nil
}

// ValidateLength validates string length
func ValidateLength(s string, minLen, maxLen int, fieldName string) error {
	if len(s) < minLen || len(s) > maxLen {
		return fmt.Errorf("%s length must be between %d and %d", fieldName, minLen, maxLen)
	}
	return nil
}

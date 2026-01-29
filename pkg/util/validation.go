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

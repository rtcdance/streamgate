package util_test

import (
	"testing"

	"streamgate/pkg/util"
	"streamgate/test/helpers"
)

func TestValidation_IsValidEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		isValid bool
	}{
		{"valid email", "test@example.com", true},
		{"valid email with subdomain", "user@mail.example.com", true},
		{"invalid - no @", "testexample.com", false},
		{"invalid - no domain", "test@", false},
		{"invalid - no local", "@example.com", false},
		{"invalid - spaces", "test @example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.IsValidEmail(tt.email)
			helpers.AssertEqual(t, tt.isValid, result)
		})
	}
}

func TestValidation_IsValidAddress(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		isValid   bool
	}{
		{"valid ethereum address", "0x1234567890123456789012345678901234567890", true},
		{"valid ethereum address uppercase", "0xABCDEF1234567890ABCDEF1234567890ABCDEF12", true},
		{"invalid - too short", "0x123456789012345678901234567890123456789", false},
		{"invalid - no 0x", "1234567890123456789012345678901234567890", false},
		{"invalid - invalid chars", "0xGHIJKL1234567890ABCDEF1234567890ABCDEF12", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.IsValidAddress(tt.address)
			helpers.AssertEqual(t, tt.isValid, result)
		})
	}
}

func TestValidation_IsValidHash(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		isValid bool
	}{
		{"valid sha256 hash", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"valid sha256 hash uppercase", "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855", true},
		{"invalid - too short", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85", false},
		{"invalid - invalid chars", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852bXYZ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.IsValidHash(tt.hash)
			helpers.AssertEqual(t, tt.isValid, result)
		})
	}
}

func TestValidation_IsValidURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		isValid bool
	}{
		{"valid http url", "http://example.com", true},
		{"valid https url", "https://example.com", true},
		{"valid url with path", "https://example.com/path/to/resource", true},
		{"valid url with query", "https://example.com?key=value", true},
		{"invalid - no scheme", "example.com", false},
		{"invalid - invalid scheme", "ftp://example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.IsValidURL(tt.url)
			helpers.AssertEqual(t, tt.isValid, result)
		})
	}
}

func TestValidation_IsValidJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		isValid bool
	}{
		{"valid json object", `{"key":"value"}`, true},
		{"valid json array", `[1,2,3]`, true},
		{"valid json string", `"test"`, true},
		{"valid json number", `123`, true},
		{"invalid json", `{invalid}`, false},
		{"invalid json - missing quote", `{"key":value}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.IsValidJSON(tt.json)
			helpers.AssertEqual(t, tt.isValid, result)
		})
	}
}

func TestValidation_SanitizeInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal text", "hello world", "hello world"},
		{"text with spaces", "  hello  world  ", "hello world"},
		{"text with special chars", "hello<script>alert('xss')</script>", "helloscriptalertxssscript"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.SanitizeInput(tt.input)
			helpers.AssertNotNil(t, result)
		})
	}
}

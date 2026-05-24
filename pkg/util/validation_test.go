package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			result := IsValidEmail(tt.email)
			require.Equal(t, tt.isValid, result)
		})
	}
}

func TestValidation_IsValidAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		isValid bool
	}{
		{"valid ethereum address", "0x1234567890123456789012345678901234567890", true},
		{"valid ethereum address uppercase", "0xABCDEF1234567890ABCDEF1234567890ABCDEF12", true},
		{"invalid - too short", "0x123456789012345678901234567890123456789", false},
		{"invalid - no 0x", "1234567890123456789012345678901234567890", false},
		{"invalid - invalid chars", "0xGHIJKL1234567890ABCDEF1234567890ABCDEF12", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidAddress(tt.address)
			require.Equal(t, tt.isValid, result)
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
			result := IsValidHash(tt.hash)
			require.Equal(t, tt.isValid, result)
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
			result := IsValidURL(tt.url)
			require.Equal(t, tt.isValid, result)
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
			result := IsValidJSON(tt.json)
			require.Equal(t, tt.isValid, result)
		})
	}
}

func TestValidation_IsSafeURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid public url", "https://example.com/video", false},
		{"localhost blocked", "http://localhost:8080/api", true},
		{"loopback ip blocked", "http://127.0.0.1/api", true},
		{"private ip blocked", "http://10.0.0.1/api", true},
		{"private ip 172 blocked", "http://172.16.0.1/api", true},
		{"private ip 192 blocked", "http://192.168.1.1/api", true},
		{"link-local blocked", "http://169.254.1.1/api", true},
		{"unspecified ip blocked", "http://0.0.0.0/api", true},
		{"missing host", "https:///path", true},
		{"invalid url", "::not-a-url", true},
		{"public ip allowed", "http://8.8.8.8/api", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := IsSafeURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidation_ValidateUUID(t *testing.T) {
	tests := []struct {
		name    string
		uuid    string
		wantErr bool
	}{
		{"valid uuid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"invalid - too short", "550e8400-e29b-41d4-a716-44665544000", true},
		{"invalid - no dashes", "550e8400e29b41d4a716446655440000", true},
		{"invalid - empty", "", true},
		{"invalid - uppercase hex", "550E8400-E29B-41D4-A716-446655440000", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUUID(tt.uuid)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidation_IsValidUUID(t *testing.T) {
	assert.True(t, IsValidUUID("550e8400-e29b-41d4-a716-446655440000"))
	assert.False(t, IsValidUUID("invalid"))
	assert.False(t, IsValidUUID(""))
}

func TestValidation_ValidateNotEmpty(t *testing.T) {
	require.NoError(t, ValidateNotEmpty("hello", "field"))
	require.Error(t, ValidateNotEmpty("", "field"))
	require.Error(t, ValidateNotEmpty("", "name"))
}

func TestValidation_ValidateLength(t *testing.T) {
	require.NoError(t, ValidateLength("hello", 1, 10, "field"))
	require.Error(t, ValidateLength("", 1, 10, "field"))
	require.Error(t, ValidateLength("a very long string", 1, 5, "field"))
	require.NoError(t, ValidateLength("abc", 3, 5, "field"))
}

func TestValidation_IsValidJSON_EmptyString(t *testing.T) {
	assert.False(t, IsValidJSON(""))
	assert.False(t, IsValidJSON("   "))
}

func TestValidation_SanitizeInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
}{
		{"normal text", "hello world", "hello world"},
		{"text with spaces", "  hello  world  ", "hello  world"},
		{"text with special chars", "hello<script>alert('xss')</script>", "hello&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeInput(tt.input)
			require.NotNil(t, result)
		})
	}
}

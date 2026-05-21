package signature

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSIWEMessage_Full(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	expires := now.Add(5 * time.Minute)

	msg := NewSIWEMessage("streamgate.io", "0x71C7656EC7ab88b098defB751B7401B5f6d8976F", "https://streamgate.io/login", 1, "abc123", now, WithSIWEExpirationTime(expires))

	result := BuildSIWEMessage(msg)

	assert.Contains(t, result, "streamgate.io wants you to sign in with your Ethereum account:")
	assert.Contains(t, result, "0x71C7656EC7ab88b098defB751B7401B5f6d8976F")
	assert.Contains(t, result, "URI: https://streamgate.io/login")
	assert.Contains(t, result, "Version: 1")
	assert.Contains(t, result, "Chain ID: 1")
	assert.Contains(t, result, "Nonce: abc123")
	assert.Contains(t, result, "Issued At: 2026-05-07T12:00:00Z")
	assert.Contains(t, result, "Expiration Time: 2026-05-07T12:05:00Z")
	assert.Contains(t, result, "- https://streamgate.io/login")
}

func TestBuildSIWEMessage_Minimal(t *testing.T) {
	msg := &SIWEMessage{
		Domain:   "example.com",
		Address:  "0x1234567890123456789012345678901234567890",
		URI:      "https://example.com",
		Version:  "1",
		ChainID:  1,
		Nonce:    "test-nonce",
		IssuedAt: "2026-01-01T00:00:00Z",
	}

	result := BuildSIWEMessage(msg)

	assert.Contains(t, result, "example.com wants you to sign in with your Ethereum account:")
	assert.Contains(t, result, "Version: 1")
	assert.NotContains(t, result, "Expiration Time:")
	assert.NotContains(t, result, "Not Before:")
	assert.NotContains(t, result, "Resources:")
}

func TestParseSIWEMessage_Full(t *testing.T) {
	raw := `streamgate.io wants you to sign in with your Ethereum account:
0x71C7656EC7ab88b098defB751B7401B5f6d8976F

Sign in to StreamGate

URI: https://streamgate.io/login
Version: 1
Chain ID: 1
Nonce: abc123
Issued At: 2026-05-07T12:00:00Z
Expiration Time: 2026-05-07T12:05:00Z
Resources:
- https://streamgate.io/login`

	msg, err := ParseSIWEMessage(raw)
	require.NoError(t, err)

	assert.Equal(t, "streamgate.io", msg.Domain)
	assert.Equal(t, "0x71C7656EC7ab88b098defB751B7401B5f6d8976F", msg.Address)
	assert.Equal(t, "https://streamgate.io/login", msg.URI)
	assert.Equal(t, "1", msg.Version)
	assert.Equal(t, int64(1), msg.ChainID)
	assert.Equal(t, "abc123", msg.Nonce)
	assert.Equal(t, "2026-05-07T12:00:00Z", msg.IssuedAt)
	assert.Equal(t, "2026-05-07T12:05:00Z", msg.ExpirationTime)
	assert.Len(t, msg.Resources, 1)
	assert.Equal(t, "https://streamgate.io/login", msg.Resources[0])
}

func TestParseSIWEMessage_Minimal(t *testing.T) {
	raw := `example.com wants you to sign in with your Ethereum account:
0x1234567890123456789012345678901234567890


URI: https://example.com
Version: 1
Chain ID: 137
Nonce: xyz789
Issued At: 2026-01-01T00:00:00Z`

	msg, err := ParseSIWEMessage(raw)
	require.NoError(t, err)

	assert.Equal(t, "example.com", msg.Domain)
	assert.Equal(t, "1", msg.Version)
	assert.Equal(t, int64(137), msg.ChainID)
	assert.Equal(t, "xyz789", msg.Nonce)
	assert.Empty(t, msg.ExpirationTime)
	assert.Empty(t, msg.Resources)
}

func TestParseSIWEMessage_InvalidTooShort(t *testing.T) {
	_, err := ParseSIWEMessage("short")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too few lines")
}

func TestParseSIWEMessage_InvalidHeader(t *testing.T) {
	raw := strings.Repeat("line\n", 10)
	_, err := ParseSIWEMessage(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing domain header")
}

func TestBuildAndParseRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	expires := now.Add(5 * time.Minute)

	original := NewSIWEMessage("streamgate.io", "0x71C7656EC7ab88b098defB751B7401B5f6d8976F", "https://streamgate.io/login", 1, "abc123", now, WithSIWEExpirationTime(expires))

	built := BuildSIWEMessage(original)
	parsed, err := ParseSIWEMessage(built)
	require.NoError(t, err)

	assert.Equal(t, original.Domain, parsed.Domain)
	assert.Equal(t, original.Address, parsed.Address)
	assert.Equal(t, original.URI, parsed.URI)
	assert.Equal(t, original.Version, parsed.Version)
	assert.Equal(t, original.ChainID, parsed.ChainID)
	assert.Equal(t, original.Nonce, parsed.Nonce)
	assert.Equal(t, original.IssuedAt, parsed.IssuedAt)
	assert.Equal(t, original.ExpirationTime, parsed.ExpirationTime)
	assert.Equal(t, original.Resources, parsed.Resources)
}

func TestNewSIWEMessage_Defaults(t *testing.T) {
	now := time.Now()
	expires := now.Add(5 * time.Minute)

	msg := NewSIWEMessage("test.com", "0xabc", "https://test.com", 1, "nonce123", now, WithSIWEExpirationTime(expires))

	assert.Equal(t, "1", msg.Version)
	assert.Equal(t, int64(1), msg.ChainID)
	assert.Equal(t, "nonce123", msg.Nonce)
	assert.Len(t, msg.Resources, 1)
	assert.Equal(t, "https://test.com", msg.Resources[0])
}

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrim(t *testing.T) {
	assert.Equal(t, "hello", Trim("  hello  "))
	assert.Equal(t, "", Trim("   "))
	assert.Equal(t, "hello world", Trim("  hello world  "))
}

func TestToLower(t *testing.T) {
	assert.Equal(t, "hello", ToLower("HELLO"))
	assert.Equal(t, "hello world", ToLower("HELLO WORLD"))
	assert.Equal(t, "abc123", ToLower("ABC123"))
}

func TestToUpper(t *testing.T) {
	assert.Equal(t, "HELLO", ToUpper("hello"))
	assert.Equal(t, "HELLO WORLD", ToUpper("hello world"))
	assert.Equal(t, "ABC123", ToUpper("abc123"))
}

func TestContains(t *testing.T) {
	assert.True(t, Contains("hello world", "world"))
	assert.True(t, Contains("hello", "hello"))
	assert.False(t, Contains("hello", "xyz"))
	assert.True(t, Contains("hello", ""))
}

func TestHasPrefix(t *testing.T) {
	assert.True(t, HasPrefix("hello world", "hello"))
	assert.False(t, HasPrefix("hello world", "world"))
	assert.True(t, HasPrefix("hello", ""))
}

func TestHasSuffix(t *testing.T) {
	assert.True(t, HasSuffix("hello world", "world"))
	assert.False(t, HasSuffix("hello world", "hello"))
	assert.True(t, HasSuffix("hello", ""))
}

func TestSplit(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, Split("a,b,c", ","))
	assert.Equal(t, []string{"hello"}, Split("hello", ","))
	assert.Equal(t, []string{"", ""}, Split(",", ","))
}

func TestJoin(t *testing.T) {
	assert.Equal(t, "a,b,c", Join([]string{"a", "b", "c"}, ","))
	assert.Equal(t, "", Join([]string{}, ","))
	assert.Equal(t, "a", Join([]string{"a"}, ","))
}

func TestIsAlphanumeric(t *testing.T) {
	assert.True(t, IsAlphanumeric("abc123"))
	assert.True(t, IsAlphanumeric("ABC"))
	assert.True(t, IsAlphanumeric(""))
	assert.False(t, IsAlphanumeric("abc-123"))
	assert.False(t, IsAlphanumeric("hello world"))
	assert.False(t, IsAlphanumeric("test!"))
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", Truncate("hello", 10))
	assert.Equal(t, "hel", Truncate("hello", 3))
	assert.Equal(t, "", Truncate("", 5))
	assert.Equal(t, "hello", Truncate("hello", 5))
}

func TestGenerateRandomString(t *testing.T) {
	s1, err := GenerateRandomString(16)
	require.NoError(t, err)
	assert.Len(t, s1, 16)

	s2, err := GenerateRandomString(16)
	require.NoError(t, err)
	assert.NotEqual(t, s1, s2)

	s3, err := GenerateRandomString(1)
	require.NoError(t, err)
	assert.Len(t, s3, 1)
}

func TestBase64Encode(t *testing.T) {
	assert.Equal(t, "aGVsbG8=", Base64Encode([]byte("hello")))
	assert.Equal(t, "", Base64Encode([]byte{}))
	assert.Equal(t, "dGVzdCBkYXRh", Base64Encode([]byte("test data")))
}

func TestBase64Decode(t *testing.T) {
	decoded, err := Base64Decode("aGVsbG8=")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), decoded)

	_, err = Base64Decode("invalid!base64")
	assert.Error(t, err)
}

func TestHexEncode(t *testing.T) {
	assert.Equal(t, "68656c6c6f", HexEncode([]byte("hello")))
	assert.Equal(t, "", HexEncode([]byte{}))
}

func TestHexDecode(t *testing.T) {
	decoded, err := HexDecode("68656c6c6f")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), decoded)

	_, err = HexDecode("invalidhex!")
	assert.Error(t, err)
}
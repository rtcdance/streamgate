package e2e_test

import (
	"testing"

	"github.com/rtcdance/streamgate/pkg/util"
	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/require"
)

func TestE2E_CryptoOperations(t *testing.T) {
	// Test hash generation
	data := []byte("test data")
	hash := util.SHA256(data)
	require.NotEmpty(t, hash)

	// Test hash verification
	hash2 := util.SHA256(data)
	require.Equal(t, hash, hash2)

	// Test different data produces different hash
	hash3 := util.SHA256([]byte("different data"))
	require.NotEqual(t, hash, hash3)
}

func TestE2E_StringOperations(t *testing.T) {
	// Test string utilities
	str := "test string"

	// Test contains
	require.True(t, util.Contains(str, "test"))
	require.False(t, util.Contains(str, "notfound"))

	// Test trim
	trimmed := util.Trim("  test  ")
	require.Equal(t, "test", trimmed)

	// Test split
	parts := util.Split("a,b,c", ",")
	require.Equal(t, 3, len(parts))
}

func TestE2E_ValidationOperations(t *testing.T) {
	// Test email validation
	require.True(t, util.IsValidEmail("test@example.com"))
	require.False(t, util.IsValidEmail("invalid-email"))

	// Test URL validation
	require.True(t, util.IsValidURL("https://example.com"))
	require.False(t, util.IsValidURL("not-a-url"))

	// Test UUID validation
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	require.True(t, util.IsValidUUID(uuid))
	require.False(t, util.IsValidUUID("not-a-uuid"))
}

func TestE2E_TimeOperations(t *testing.T) {
	// Test time utilities
	now := util.Now()
	require.NotNil(t, now)

	// Test time formatting
	formatted := util.FormatTime(now, "2006-01-02")
	require.NotEmpty(t, formatted)

	// Test time parsing
	parsed, err := util.ParseTime("2024-01-01", "2006-01-02")
	require.NoError(t, err)
	require.NotNil(t, parsed)
}

func TestE2E_FileOperations(t *testing.T) {
	// Create temp file
	tmpFile := helpers.CreateTempFile(t, []byte("test content"))
	require.NotEmpty(t, tmpFile)

	// Test file exists
	require.True(t, util.FileExists(tmpFile))

	// Test read file
	content, err := util.ReadFile(tmpFile)
	require.NoError(t, err)
	require.Equal(t, "test content", string(content))

	// Test file size
	size, err := util.FileSize(tmpFile)
	require.NoError(t, err)
	require.Equal(t, int64(12), size)
}

func TestE2E_JSONOperations(t *testing.T) {
	// Test JSON marshaling
	data := map[string]interface{}{
		"name": "test",
		"age":  30,
	}

	jsonStr, err := util.ToJSON(data)
	require.NoError(t, err)
	require.NotEmpty(t, jsonStr)

	// Test JSON unmarshaling
	var result map[string]interface{}
	err = util.FromJSON(jsonStr, &result)
	require.NoError(t, err)
	require.Equal(t, "test", result["name"])
}

func TestE2E_EncodingOperations(t *testing.T) {
	// Test base64 encoding
	data := []byte("test data")
	encoded := util.Base64Encode(data)
	require.NotEmpty(t, encoded)

	// Test base64 decoding
	decoded, err := util.Base64Decode(encoded)
	require.NoError(t, err)
	require.Equal(t, data, decoded)

	// Test hex encoding
	hexEncoded := util.HexEncode(data)
	require.NotEmpty(t, hexEncoded)

	// Test hex decoding
	hexDecoded, err := util.HexDecode(hexEncoded)
	require.NoError(t, err)
	require.Equal(t, data, hexDecoded)
}

func TestE2E_CompressionOperations(t *testing.T) {
	// Test gzip compression
	data := []byte("test data for compression")
	compressed, err := util.GzipCompress(data)
	require.NoError(t, err)
	require.True(t, len(compressed) > 0)

	// Test gzip decompression
	decompressed, err := util.GzipDecompress(compressed)
	require.NoError(t, err)
	require.Equal(t, data, decompressed)
}

func TestE2E_SliceOperations(t *testing.T) {
	// Test slice contains
	slice := []string{"a", "b", "c"}
	require.True(t, util.SliceContains(slice, "b"))
	require.False(t, util.SliceContains(slice, "d"))

	// Test slice index
	index := util.SliceIndex(slice, "b")
	require.Equal(t, 1, index)

	// Test slice remove
	result := util.SliceRemove(slice, "b")
	require.Equal(t, 2, len(result))
}

package e2e_test

import (
	"testing"

	"streamgate/pkg/util"
	"streamgate/test/helpers"
)

func TestE2E_CryptoOperations(t *testing.T) {
	// Test hash generation
	data := []byte("test data")
	hash := util.SHA256(data)
	helpers.AssertNotEmpty(t, hash)

	// Test hash verification
	hash2 := util.SHA256(data)
	helpers.AssertEqual(t, hash, hash2)

	// Test different data produces different hash
	hash3 := util.SHA256([]byte("different data"))
	helpers.AssertNotEqual(t, hash, hash3)
}

func TestE2E_StringOperations(t *testing.T) {
	// Test string utilities
	str := "test string"

	// Test contains
	helpers.AssertTrue(t, util.Contains(str, "test"))
	helpers.AssertFalse(t, util.Contains(str, "notfound"))

	// Test trim
	trimmed := util.Trim("  test  ")
	helpers.AssertEqual(t, "test", trimmed)

	// Test split
	parts := util.Split("a,b,c", ",")
	helpers.AssertEqual(t, 3, len(parts))
}

func TestE2E_ValidationOperations(t *testing.T) {
	// Test email validation
	helpers.AssertTrue(t, util.IsValidEmail("test@example.com"))
	helpers.AssertFalse(t, util.IsValidEmail("invalid-email"))

	// Test URL validation
	helpers.AssertTrue(t, util.IsValidURL("https://example.com"))
	helpers.AssertFalse(t, util.IsValidURL("not-a-url"))

	// Test UUID validation
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	helpers.AssertTrue(t, util.IsValidUUID(uuid))
	helpers.AssertFalse(t, util.IsValidUUID("not-a-uuid"))
}

func TestE2E_TimeOperations(t *testing.T) {
	// Test time utilities
	now := util.Now()
	helpers.AssertNotNil(t, now)

	// Test time formatting
	formatted := util.FormatTime(now, "2006-01-02")
	helpers.AssertNotEmpty(t, formatted)

	// Test time parsing
	parsed, err := util.ParseTime("2024-01-01", "2006-01-02")
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, parsed)
}

func TestE2E_FileOperations(t *testing.T) {
	// Create temp file
	tmpFile := helpers.CreateTempFile(t, []byte("test content"))
	helpers.AssertNotEmpty(t, tmpFile)

	// Test file exists
	helpers.AssertTrue(t, util.FileExists(tmpFile))

	// Test read file
	content, err := util.ReadFile(tmpFile)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "test content", string(content))

	// Test file size
	size := util.FileSize(tmpFile)
	helpers.AssertEqual(t, int64(12), size)
}

func TestE2E_JSONOperations(t *testing.T) {
	// Test JSON marshaling
	data := map[string]interface{}{
		"name": "test",
		"age":  30,
	}

	jsonStr, err := util.ToJSON(data)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, jsonStr)

	// Test JSON unmarshaling
	var result map[string]interface{}
	err = util.FromJSON(jsonStr, &result)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "test", result["name"])
}

func TestE2E_EncodingOperations(t *testing.T) {
	// Test base64 encoding
	data := []byte("test data")
	encoded := util.Base64Encode(data)
	helpers.AssertNotEmpty(t, encoded)

	// Test base64 decoding
	decoded, err := util.Base64Decode(encoded)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, data, decoded)

	// Test hex encoding
	hexEncoded := util.HexEncode(data)
	helpers.AssertNotEmpty(t, hexEncoded)

	// Test hex decoding
	hexDecoded, err := util.HexDecode(hexEncoded)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, data, hexDecoded)
}

func TestE2E_CompressionOperations(t *testing.T) {
	// Test gzip compression
	data := []byte("test data for compression")
	compressed, err := util.GzipCompress(data)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, len(compressed) > 0)

	// Test gzip decompression
	decompressed, err := util.GzipDecompress(compressed)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, data, decompressed)
}

func TestE2E_SliceOperations(t *testing.T) {
	// Test slice contains
	slice := []string{"a", "b", "c"}
	helpers.AssertTrue(t, util.SliceContains(slice, "b"))
	helpers.AssertFalse(t, util.SliceContains(slice, "d"))

	// Test slice index
	index := util.SliceIndex(slice, "b")
	helpers.AssertEqual(t, 1, index)

	// Test slice remove
	result := util.SliceRemove(slice, "b")
	helpers.AssertEqual(t, 2, len(result))
}

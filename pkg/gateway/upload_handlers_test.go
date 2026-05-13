package gateway

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectVideoFormat_ValidMP4(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		want   string
	}{
		{"ftyp isom", []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D}, "mp4"},
		{"ftyp mp42", []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x6D, 0x70, 0x34, 0x32}, "mp4"},
		{"ftyp MSNV", []byte{0x00, 0x00, 0x00, 0x1C, 0x66, 0x74, 0x79, 0x70, 0x4D, 0x53, 0x4E, 0x56}, "mp4"},
		{"ftyp at offset 4", []byte{0x00, 0x00, 0x00, 0x14, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D}, "mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectVideoFormat(bytes.NewReader(tt.header))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectVideoFormat_ValidWebM(t *testing.T) {
	got := detectVideoFormat(bytes.NewReader([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x00, 0x00, 0x00}))
	assert.Equal(t, "webm", got)
}

func TestDetectVideoFormat_ValidAVI(t *testing.T) {
	got := detectVideoFormat(bytes.NewReader([]byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x41, 0x56, 0x49, 0x20}))
	assert.Equal(t, "avi", got)
}

func TestDetectVideoFormat_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
	}{
		{"empty", []byte{}},
		{"text file", []byte("Hello World")},
		{"JSON", []byte(`{"key":"value"}`)},
		{"PNG image", []byte{0x89, 0x50, 0x4E, 0x47}},
		{"JPG image", []byte{0xFF, 0xD8, 0xFF, 0xE0}},
		{"PDF", []byte{0x25, 0x50, 0x44, 0x46}},
		{"ZIP", []byte{0x50, 0x4B, 0x03, 0x04}},
		{"single byte", []byte{0x00}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectVideoFormat(bytes.NewReader(tt.header))
			assert.Empty(t, got, "expected no format match for %s", tt.name)
		})
	}
}

func TestReadFileHeader_ReturnsCombinedContent(t *testing.T) {
	ftyp := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D, 0x00, 0x00, 0x00, 0x01, 0x61, 0x62, 0x63, 0x64}
	r := bytes.NewReader(ftyp)
	format, combined, err := readFileHeader(r)
	require.NoError(t, err)
	assert.Equal(t, "mp4", format)
	all, err := io.ReadAll(combined)
	require.NoError(t, err)
	assert.Equal(t, ftyp, all)
}

func TestReadFileHeader_EmptyFile(t *testing.T) {
	format, combined, err := readFileHeader(bytes.NewReader([]byte{}))
	require.NoError(t, err)
	assert.Empty(t, format)
	all, err := io.ReadAll(combined)
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestReadFileHeader_TextFileNoFormatDetected(t *testing.T) {
	body := []byte("this is not a video file")
	format, combined, err := readFileHeader(bytes.NewReader(body))
	require.NoError(t, err)
	assert.Empty(t, format)
	all, err := io.ReadAll(combined)
	require.NoError(t, err)
	assert.Equal(t, body, all)
}

func TestUploadFlow_ValidMP4RoundTrip(t *testing.T) {
	mp4Header := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D, 0x00, 0x00, 0x00, 0x01}
	body := append(mp4Header, []byte("fake payload...")...)
	format, combined, err := readFileHeader(bytes.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, "mp4", format)
	all, err := io.ReadAll(combined)
	require.NoError(t, err)
	assert.Equal(t, body, all)
}

func TestUploadFlow_TextFileRejected(t *testing.T) {
	format, _, err := readFileHeader(bytes.NewReader([]byte("plain text")))
	require.NoError(t, err)
	assert.Empty(t, format)
}

func TestSanitizeObjectKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantKey string
		wantOK  bool
	}{
		{"valid name", "video-123", "video-123", true},
		{"valid extension", "file.mp4", "file.mp4", true},
		{"empty", "", "", true},
		{"double dot", "video..secret", "", false},
		{"dot dot slash", "../etc/passwd", "", false},
		{"forward slash", "dir/file", "", false},
		{"backslash", "dir\\file", "", false},
		{"embedded dot dot", "a/../b", "", false},
		{"mixed slashes", "a/b\\c", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := sanitizeObjectKey(tt.input)
			assert.Equal(t, tt.wantKey, key)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestVideoMagicBytes_SelfDetect(t *testing.T) {
	for i, entry := range videoMagicBytes {
		t.Run(fmt.Sprintf("entry_%d_%s", i, entry.format), func(t *testing.T) {
			assert.NotEmpty(t, entry.format)
			assert.Greater(t, len(entry.bytes), 0)
			header := make([]byte, entry.offset+len(entry.bytes))
			copy(header[entry.offset:], entry.bytes)
			got := detectVideoFormat(bytes.NewReader(header))
			assert.Equal(t, entry.format, got)
		})
	}
}

package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeObjectKey(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantKey   string
		wantOK    bool
	}{
		{"valid simple name", "video-123", "video-123", true},
		{"valid with extension", "file.mp4", "file.mp4", true},
		{"empty string", "", "", true},
		{"path traversal double dot", "video..secret", "", false},
		{"path traversal dot dot slash", "../etc/passwd", "", false},
		{"forward slash", "dir/file", "", false},
		{"backslash", "dir\\file", "", false},
		{"leading traversal", "../secret", "", false},
		{"embedded traversal", "a/../b", "", false},
		{"mixed slashes", "a/b\\c", "", false},
		{"trailing slash", "file/", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := sanitizeObjectKey(tt.input)
			assert.Equal(t, tt.wantKey, key)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

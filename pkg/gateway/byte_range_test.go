package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseByteRange(t *testing.T) {
	const size = 1000
	cases := []struct {
		name      string
		header    string
		wantStart int64
		wantEnd   int64
		wantOK    bool
	}{
		{"first bytes", "bytes=0-99", 0, 99, true},
		{"middle", "bytes=100-199", 100, 199, true},
		{"single byte", "bytes=0-0", 0, 0, true},
		{"open ended", "bytes=900-", 900, 999, true},
		{"whole object", "bytes=0-999", 0, 999, true},
		{"suffix", "bytes=-100", 900, 999, true},
		{"suffix larger than the object", "bytes=-5000", 0, 999, true},
		{"end past the object clamps", "bytes=900-9999", 900, 999, true},
		{"whitespace tolerated", "bytes= 10 - 19 ", 10, 19, true},

		// Unsatisfiable or unsupported requests must be refused, not answered
		// with bytes the client did not ask for.
		{"start at the size", "bytes=1000-", 0, 0, false},
		{"start past the size", "bytes=1500-1600", 0, 0, false},
		{"end before start", "bytes=200-100", 0, 0, false},
		{"multi-range refused", "bytes=0-10,20-30", 0, 0, false},
		{"missing unit", "0-10", 0, 0, false},
		{"empty spec", "bytes=", 0, 0, false},
		{"no dash", "bytes=100", 0, 0, false},
		{"zero suffix", "bytes=-0", 0, 0, false},
		{"negative start", "bytes=-5-10", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end, ok := parseByteRange(tc.header, size)
			assert.Equal(t, tc.wantOK, ok, "ok for %q", tc.header)
			if tc.wantOK {
				assert.Equal(t, tc.wantStart, start)
				assert.Equal(t, tc.wantEnd, end)
			}
		})
	}
}

// An empty object has no satisfiable range.
func TestParseByteRange_EmptyObject(t *testing.T) {
	_, _, ok := parseByteRange("bytes=0-10", 0)
	assert.False(t, ok)
}

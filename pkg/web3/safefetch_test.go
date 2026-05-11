package web3

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"169.254.1.1", true},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip)
			assert.Equal(t, tt.expected, isPrivateIP(ip))
		})
	}
}

func TestRewriteURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{"ipfs scheme", "ipfs://QmTest", DefaultIPFSGateway + "QmTest", false},
		{"ar scheme", "ar://abc123", DefaultArweaveGateway + "abc123", false},
		{"https scheme", "https://example.com/metadata.json", "https://example.com/metadata.json", false},
		{"http scheme", "http://example.com/metadata.json", "http://example.com/metadata.json", false},
		{"ftp scheme", "ftp://evil.com/file", "", true},
		{"file scheme", "file:///etc/passwd", "", true},
		{"no scheme", "just-a-string", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rewriteURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestSafeFetchURI_BlocksPrivateIPs(t *testing.T) {
	// Start a local server on 127.0.0.1 which is private
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"name": "leaked"})
	}))
	defer server.Close()

	var result map[string]string
	err := safeFetchURI(context.Background(), server.URL, &result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blocked private")
}

func TestSafeFetchURI_DataURI(t *testing.T) {
	var result map[string]string
	err := safeFetchURI(context.Background(),
		`data:application/json,{"name":"test"}`,
		&result)
	require.NoError(t, err)
	assert.Equal(t, "test", result["name"])
}

func TestSafeFetchURI_UnsupportedScheme(t *testing.T) {
	var result map[string]string
	err := safeFetchURI(context.Background(), "ftp://evil.com/file", &result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported URI scheme")
}

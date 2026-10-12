package gateway

import (
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"

	"github.com/stretchr/testify/assert"
)

// The allowlist is derived from the configured object store, in both "host:port"
// and bare "host" form, because a presigned URL may carry the port and an
// internal one may not.
func TestTranscodeInputAllowlist_DerivesFromTheStorageEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     []string
	}{
		{"host and port", "minio:9000", []string{"minio:9000", "minio"}},
		{"host only", "minio.internal", []string{"minio.internal"}},
		{"with a scheme", "http://minio:9000", []string{"minio:9000", "minio"}},
		{"with a trailing slash", "minio:9000/", []string{"minio:9000", "minio"}},
		{"scheme and trailing slash", "https://minio:9000/", []string{"minio:9000", "minio"}},
		{"empty", "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Storage.Endpoint = tc.endpoint
			assert.Equal(t, tc.want, transcodeInputAllowlist(cfg))
		})
	}
}

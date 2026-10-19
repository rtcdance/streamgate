package load

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func mediaHashesDigest(hashes map[string]string) (string, error) {
	if len(hashes) == 0 {
		return "", nil
	}
	for path, digest := range hashes {
		if path != "*" {
			parsed, err := url.ParseRequestURI(path)
			if err != nil || !strings.HasPrefix(path, "/") || parsed.Path != path || parsed.RawQuery != "" || parsed.Fragment != "" {
				return "", fmt.Errorf("media hash keys must be URL paths without credentials or query parameters")
			}
		}
		raw, err := hex.DecodeString(digest)
		if err != nil || len(raw) != sha256.Size {
			return "", fmt.Errorf("media hashes must contain valid SHA256 digests")
		}
	}
	raw, err := json.Marshal(hashes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func verifyMediaHash(rawURL string, body []byte, hashes map[string]string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid media URL")
	}
	expected, ok := hashes[parsed.Path]
	if !ok {
		expected, ok = hashes["*"]
	}
	if !ok {
		return fmt.Errorf("media segment is absent from fixture hash list")
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), expected) {
		return fmt.Errorf("media segment SHA256 mismatch")
	}
	return nil
}

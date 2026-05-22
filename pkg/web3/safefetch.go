package web3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rtcdance/streamgate/pkg/web3/solana"
)

// DefaultIPFSGateway is the default gateway for ipfs:// URI resolution.
var DefaultIPFSGateway = "https://ipfs.io/ipfs/"

// DefaultArweaveGateway is the default gateway for ar:// URI resolution.
var DefaultArweaveGateway = "https://arweave.net/"

// fetchSemaphore limits concurrent safeFetchURI calls to prevent resource exhaustion.
var fetchSemaphore = make(chan struct{}, 20) // max 20 concurrent fetches

// activeFetches tracks active goroutines for monitoring
var activeFetches atomic.Int64

// safeHTTPClient is an HTTP client with a custom dialer that blocks private IPs.
var safeHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q: %w", addr, err)
			}
			ip := net.ParseIP(host)
			if ip == nil {
				// Resolve hostname
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve %q: %w", host, err)
				}
				if len(ips) == 0 {
					return nil, fmt.Errorf("no IPs found for %q", host)
				}
				ip = ips[0].IP
			}
			if isPrivateIP(ip) {
				return nil, fmt.Errorf("blocked private/reserved IP: %s", ip)
			}
			dialer := &net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, network, addr)
		},
	},
}

// GetActiveFetches returns the number of currently active fetch operations
func GetActiveFetches() int64 {
	return activeFetches.Load()
}

// SetFetchConcurrency sets the maximum number of concurrent fetch operations
func SetFetchConcurrency(max int) {
	fetchSemaphore = make(chan struct{}, max)
}

// CloseSafeHTTPClient closes idle connections on the shared safe HTTP client.
// Call this during application shutdown.
func CloseSafeHTTPClient() {
	if t, ok := safeHTTPClient.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}

// isPrivateIP checks if an IP is in a private, loopback, or link-local range.
func isPrivateIP(ip net.IP) bool {
	privateRanges := []struct {
		network *net.IPNet
	}{
		{mustParseCIDR("10.0.0.0/8")},
		{mustParseCIDR("172.16.0.0/12")},
		{mustParseCIDR("192.168.0.0/16")},
		{mustParseCIDR("127.0.0.0/8")},
		{mustParseCIDR("169.254.0.0/16")},
		{mustParseCIDR("::1/128")},
		{mustParseCIDR("fc00::/7")},
		{mustParseCIDR("fe80::/10")},
	}
	for _, r := range privateRanges {
		if r.network.Contains(ip) {
			return true
		}
	}
	return false
}

func mustParseCIDR(s string) *net.IPNet {
	_, network, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return network
}

// rewriteURI rewrites ipfs:// and ar:// URIs to HTTPS gateway URLs.
// Returns an error for unsupported schemes (non-https, non-ipfs, non-ar).
func rewriteURI(uri string) (string, error) {
	switch {
	case strings.HasPrefix(uri, "ipfs://"):
		return DefaultIPFSGateway + strings.TrimPrefix(uri, "ipfs://"), nil
	case strings.HasPrefix(uri, "ar://"):
		return DefaultArweaveGateway + strings.TrimPrefix(uri, "ar://"), nil
	case strings.HasPrefix(uri, "https://"):
		return uri, nil
	case strings.HasPrefix(uri, "http://"):
		// Allow http:// for dev/test but warn it's not production-safe
		return uri, nil
	default:
		return "", fmt.Errorf("unsupported URI scheme in %q (allowed: https, ipfs, ar)", uri)
	}
}

// safeFetchURI fetches metadata from a URI with SSRF protection.
// It validates the scheme, blocks private IPs, and rewrites ipfs:// and ar://
// URIs through controlled gateways.
func safeFetchURI(ctx context.Context, uri string, result interface{}) error {
	// Acquire semaphore slot (blocks if limit reached)
	select {
	case fetchSemaphore <- struct{}{}:
		defer func() { <-fetchSemaphore }()
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for fetch slot: %w", ctx.Err())
	}

	activeFetches.Add(1)
	defer func() {
		activeFetches.Add(-1)
	}()

	// Handle data: URIs inline — no HTTP request needed
	if strings.HasPrefix(uri, "data:application/json") {
		// Format: data:application/json;utf8,{...} or data:application/json;base64,...
		parts := strings.SplitN(uri, ",", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid data URI format")
		}
		payload := parts[1]
		// Check for base64 encoding
		if strings.Contains(parts[0], "base64") {
			return fmt.Errorf("base64 data URIs not supported")
		}
		// URL-decode percent-encoded characters
		payload = strings.ReplaceAll(payload, "%22", `"`)
		payload = strings.ReplaceAll(payload, "%7B", "{")
		payload = strings.ReplaceAll(payload, "%7D", "}")
		if err := json.Unmarshal([]byte(payload), result); err != nil {
			return fmt.Errorf("failed to parse data URI payload: %w", err)
		}
		return nil
	}

	// Rewrite ipfs:// and ar:// URIs, validate scheme
	resolvedURI, err := rewriteURI(uri)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", resolvedURI, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := safeHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata request failed with status: %d", resp.StatusCode)
	}

	// Limit response body to 1MB to prevent OOM from malicious NFT metadata servers.
	const maxMetadataSize = 1 << 20 // 1MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataSize))
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	return nil
}

// SafeFetchURI fetches JSON metadata from a URI with SSRF protection.
// Supports https://, ipfs://, ar://, and data:application/json URIs.
// Private IPs are blocked. Concurrency is limited by the fetch semaphore.
func SafeFetchURI(ctx context.Context, uri string, result interface{}) error {
	return safeFetchURI(ctx, uri, result)
}

func init() {
	solana.SafeURIFetch = safeFetchURI
}

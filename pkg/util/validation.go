package util

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// ValidateEmail validates email format
func ValidateEmail(email string) error {
	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
	matched, err := regexp.MatchString(pattern, email)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid email format")
	}
	return nil
}

// IsValidEmail checks if email is valid (returns bool)
func IsValidEmail(email string) bool {
	return ValidateEmail(email) == nil
}

// ValidateURL validates URL format
func ValidateURL(url string) error {
	pattern := `^https?://[^\s/$.?#].[^\s]*$`
	matched, err := regexp.MatchString(pattern, url)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid URL format")
	}
	return nil
}

// IsValidURL checks if URL is valid (returns bool)
func IsValidURL(rawURL string) bool {
	return ValidateURL(rawURL) == nil
}

// IsSafeURL checks that a URL is a valid http/https URL whose host resolves
// ONLY to public addresses (no RFC 1918, loopback, link-local, CGNAT or
// unspecified address). It is the guard for endpoints that fetch a remote
// resource on a caller's behalf.
//
// The host is RESOLVED, not just pattern-matched: a name that resolves to
// 127.0.0.1 or to the cloud metadata address passes any string check and is the
// ordinary way this class of guard is defeated.
func IsSafeURL(rawURL string) error {
	return ValidateFetchTarget(rawURL, FetchTargetOptions{AllowExternal: true})
}

// FetchTargetOptions is the policy for a URL the server will fetch itself.
type FetchTargetOptions struct {
	// AllowedHosts is an exact host or host:port allowlist. A match is permitted
	// WITHOUT relaxing anything for other hosts — this is how a configured
	// internal object store is allowed without opening every private address to
	// a caller-supplied URL.
	AllowedHosts []string
	// AllowExternal permits public internet URLs. Off by default: the API accepts
	// uploaded objects and internal object-store URLs, not arbitrary external
	// input.
	AllowExternal bool
	// Resolve replaces the DNS resolver. Tests inject it; production leaves it
	// nil so the hostname is genuinely resolved.
	Resolve func(host string) ([]net.IP, error)
}

// ValidateFetchTarget applies the fetch policy for a server-side fetch.
//
// Order matters: the allowlist is consulted first (so the object store works
// without loosening anything else), then external input is refused unless the
// deployment opted in, and finally every resolved address is checked. A name
// that fails to resolve is refused rather than assumed safe.
func ValidateFetchTarget(rawURL string, opts FetchTargetOptions) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("only http and https URLs are allowed, got %q", parsed.Scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL missing host")
	}

	if hostAllowed(host, parsed.Port(), opts.AllowedHosts) {
		return nil
	}

	if !opts.AllowExternal {
		return fmt.Errorf("external input URLs are not accepted")
	}

	addresses, err := resolveHost(host, opts.Resolve)
	if err != nil {
		return err
	}
	for _, ip := range addresses {
		if err := checkPublicAddress(ip); err != nil {
			return fmt.Errorf("host %q: %w", host, err)
		}
	}
	return nil
}

// hostAllowed reports whether host (or host:port) is on the exact allowlist.
// Comparison is case-insensitive on the host; the port is only compared when the
// entry names one.
func hostAllowed(host, port string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	withPort := host
	if port != "" {
		withPort = host + ":" + port
	}
	for _, entry := range allowed {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == withPort || entry == host {
			return true
		}
	}
	return false
}

// resolveHost turns a host into the addresses it resolves to. An IP literal is
// returned as-is; anything else goes through the resolver, and a failure is
// reported rather than treated as "no addresses, therefore fine".
func resolveHost(host string, resolve func(string) ([]net.IP, error)) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if resolve == nil {
		resolve = net.LookupIP
	}
	addrs, err := resolve(host)
	if err != nil {
		return nil, fmt.Errorf("host %q could not be resolved: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("host %q resolved to no addresses", host)
	}
	return addrs, nil
}

// cgnat is 100.64.0.0/10 (RFC 6598): carrier-grade NAT space, which is not
// "private" by net.IP.IsPrivate but is still not public.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// checkPublicAddress rejects anything that is not a routable public address.
// Every address a name resolves to is checked, so a host with one public and one
// internal record is refused rather than half-trusted.
//
// The offending address is named: this text goes to the server log, not to the
// caller (the handler returns a generic message), and "which address was
// refused" is the first thing an operator needs.
func checkPublicAddress(ip net.IP) error {
	// Normalise IPv4-mapped IPv6 (::ffff:127.0.0.1) so the v4 checks apply.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsLoopback():
		return fmt.Errorf("loopback address %s is not allowed", ip)
	case ip.IsPrivate():
		return fmt.Errorf("private address %s is not allowed", ip)
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		// Includes 169.254.169.254, the cloud instance-metadata address.
		return fmt.Errorf("link-local address %s is not allowed", ip)
	case ip.IsUnspecified():
		return fmt.Errorf("unspecified address %s is not allowed", ip)
	case ip.IsMulticast():
		return fmt.Errorf("multicast address %s is not allowed", ip)
	case cgnat.Contains(ip):
		return fmt.Errorf("carrier-grade NAT address %s is not allowed", ip)
	}
	return nil
}

// ValidateUUID validates UUID format
func ValidateUUID(uuid string) error {
	pattern := `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	matched, err := regexp.MatchString(pattern, uuid)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid UUID format")
	}
	return nil
}

// IsValidUUID checks if UUID is valid (returns bool)
func IsValidUUID(uuid string) bool {
	return ValidateUUID(uuid) == nil
}

// ValidateEthereumAddress validates Ethereum address format
func ValidateEthereumAddress(address string) error {
	pattern := `^0x[0-9a-fA-F]{40}$`
	matched, err := regexp.MatchString(pattern, address)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid Ethereum address format")
	}
	return nil
}

// IsValidAddress checks if Ethereum address is valid (returns bool)
func IsValidAddress(address string) bool {
	return ValidateEthereumAddress(address) == nil
}

// ValidateHash validates hash format (64 hex characters)
func ValidateHash(hash string) error {
	pattern := `^[0-9a-fA-F]{64}$`
	matched, err := regexp.MatchString(pattern, hash)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid hash format")
	}
	return nil
}

// IsValidHash checks if hash is valid (returns bool)
func IsValidHash(hash string) bool {
	return ValidateHash(hash) == nil
}

// IsValidJSON checks if string is valid JSON
func IsValidJSON(s string) bool {
	s = TrimSpace(s)
	if s == "" {
		return false
	}

	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

// ValidateNotEmpty validates string is not empty
func ValidateNotEmpty(s, fieldName string) error {
	if s == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	return nil
}

// ValidateLength validates string length
func ValidateLength(s string, minLen, maxLen int, fieldName string) error {
	if len(s) < minLen || len(s) > maxLen {
		return fmt.Errorf("%s length must be between %d and %d", fieldName, minLen, maxLen)
	}
	return nil
}

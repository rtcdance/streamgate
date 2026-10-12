package util

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolverFor answers every name with the given addresses, so the address checks
// can be exercised without depending on real DNS.
func resolverFor(addrs ...string) func(string) ([]net.IP, error) {
	return func(string) ([]net.IP, error) {
		ips := make([]net.IP, 0, len(addrs))
		for _, a := range addrs {
			ip := net.ParseIP(a)
			if ip == nil {
				return nil, fmt.Errorf("bad test address %q", a)
			}
			ips = append(ips, ip)
		}
		return ips, nil
	}
}

func failingResolver() func(string) ([]net.IP, error) {
	return func(string) ([]net.IP, error) { return nil, fmt.Errorf("no such host") }
}

// A name that resolves to an internal address must be refused. This is the whole
// point of resolving rather than pattern-matching: the string looks public.
func TestValidateFetchTarget_ResolvesAndRefusesInternalTargets(t *testing.T) {
	cases := []struct {
		name string
		addr string
	}{
		{"loopback", "127.0.0.1"},
		{"loopback elsewhere in the block", "127.9.9.9"},
		{"cloud metadata", "169.254.169.254"},
		{"RFC1918 10/8", "10.1.2.3"},
		{"RFC1918 192.168/16", "192.168.1.1"},
		{"RFC1918 172.16/12", "172.16.5.5"},
		{"carrier-grade NAT", "100.64.0.1"},
		{"unspecified", "0.0.0.0"},
		{"IPv6 loopback", "::1"},
		{"IPv6 link-local", "fe80::1"},
		{"IPv6 unique-local", "fd00::1"},
		{"IPv4-mapped loopback", "::ffff:127.0.0.1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateFetchTarget("https://cdn.example.com/video.mp4", FetchTargetOptions{
				AllowExternal: true,
				Resolve:       resolverFor(tc.addr),
			})
			assert.Error(t, err, "a name resolving to %s must be refused", tc.addr)
		})
	}
}

// A name with BOTH a public and an internal record is refused: every resolved
// address is checked, so a half-public answer cannot be half-trusted.
func TestValidateFetchTarget_RefusesAMixedAnswer(t *testing.T) {
	err := ValidateFetchTarget("https://cdn.example.com/video.mp4", FetchTargetOptions{
		AllowExternal: true,
		Resolve:       resolverFor("93.184.216.34", "10.0.0.5"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "10.0.0.5")
}

func TestValidateFetchTarget_AllowsAPublicAddress(t *testing.T) {
	err := ValidateFetchTarget("https://cdn.example.com/video.mp4", FetchTargetOptions{
		AllowExternal: true,
		Resolve:       resolverFor("93.184.216.34"),
	})
	assert.NoError(t, err)
}

// A name that cannot be resolved is refused rather than assumed safe.
func TestValidateFetchTarget_RefusesAnUnresolvableName(t *testing.T) {
	err := ValidateFetchTarget("https://nothing.example.invalid/v.mp4", FetchTargetOptions{
		AllowExternal: true,
		Resolve:       failingResolver(),
	})
	assert.Error(t, err)
}

// External input is refused BY DEFAULT. The API takes uploaded objects and
// internal object-store URLs, not a URL of the caller's choosing.
func TestValidateFetchTarget_RefusesExternalByDefault(t *testing.T) {
	err := ValidateFetchTarget("https://cdn.example.com/video.mp4", FetchTargetOptions{
		Resolve: resolverFor("93.184.216.34"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "external input URLs are not accepted")
}

// The allowlist is how the object store is reachable without opening anything
// else — the mistake the old option made, where "allow localhost" also allowed
// every private address.
func TestValidateFetchTarget_AllowlistAdmitsTheObjectStoreOnly(t *testing.T) {
	opts := FetchTargetOptions{
		AllowedHosts: []string{"minio:9000"},
		Resolve:      resolverFor("10.0.0.7"),
	}

	assert.NoError(t, ValidateFetchTarget("http://minio:9000/streamgate/in.mp4?sig=x", opts),
		"the configured object store must be reachable")
	assert.Error(t, ValidateFetchTarget("http://other-internal:9000/streamgate/in.mp4", opts),
		"a different internal host must NOT be admitted by the same policy")
	assert.Error(t, ValidateFetchTarget("http://10.0.0.7/streamgate/in.mp4", opts),
		"the address behind the allowlisted name is not itself allowlisted")
}

// An allowlist entry without a port matches the host on any port, and an entry
// with a port matches only that port.
func TestValidateFetchTarget_AllowlistMatching(t *testing.T) {
	byHost := FetchTargetOptions{AllowedHosts: []string{"minio"}, Resolve: resolverFor("10.0.0.7")}
	assert.NoError(t, ValidateFetchTarget("http://minio:9000/x.mp4", byHost))
	assert.NoError(t, ValidateFetchTarget("http://minio/x.mp4", byHost))

	byPort := FetchTargetOptions{AllowedHosts: []string{"minio:9000"}, Resolve: resolverFor("10.0.0.7")}
	assert.NoError(t, ValidateFetchTarget("http://minio:9000/x.mp4", byPort))
	assert.Error(t, ValidateFetchTarget("http://minio:8080/x.mp4", byPort),
		"an entry naming a port must not admit a different one")
}

// Only http and https are fetchable. file/data/ftp are local-resource or
// non-HTTP schemes and must never be accepted.
func TestValidateFetchTarget_RefusesNonHTTPSchemes(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"data:text/plain,hello",
		"ftp://example.com/v.mp4",
		"gopher://example.com/",
		"javascript:alert(1)",
	} {
		err := ValidateFetchTarget(raw, FetchTargetOptions{AllowExternal: true, Resolve: resolverFor("93.184.216.34")})
		assert.Error(t, err, "%s must be refused", raw)
	}
}

// A literal internal address is refused even when external input is enabled.
func TestValidateFetchTarget_RefusesALiteralInternalAddress(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/v.mp4",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/v.mp4",
		"http://0.0.0.0/v.mp4",
	} {
		err := ValidateFetchTarget(raw, FetchTargetOptions{AllowExternal: true})
		assert.Error(t, err, "%s must be refused", raw)
	}
}

// The URL must name a host at all.
func TestValidateFetchTarget_RequiresAHost(t *testing.T) {
	assert.Error(t, ValidateFetchTarget("http:///v.mp4", FetchTargetOptions{AllowExternal: true}))
	assert.Error(t, ValidateFetchTarget("not a url", FetchTargetOptions{AllowExternal: true}))
}

// IsSafeURL is the strict form: public addresses only.
func TestIsSafeURL_RejectsInternalTargets(t *testing.T) {
	assert.NoError(t, IsSafeURL("https://93.184.216.34/v.mp4"))
	assert.Error(t, IsSafeURL("http://127.0.0.1/v.mp4"))
	assert.Error(t, IsSafeURL("http://10.0.0.1/v.mp4"))
	assert.Error(t, IsSafeURL("ftp://example.com/v.mp4"))
}

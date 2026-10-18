// Command grpcparity is the Docker-level parity probe for the HTTP and gRPC
// playback entry points: it talks to the REAL stack over gRPC and asserts the
// authorization decision the two entry points must share. It is run by
// scripts/fullchain-acceptance.sh via `go run ./test/grpcparity`.
//
// Exit 0 only when the asserted outcome holds; otherwise one clear line is
// printed to stderr and the process exits 1.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type options struct {
	target          string
	content         string
	token           string
	mode            string
	capability      string
	httpBase        string
	expectExpSecond int64
}

func main() {
	o := parseFlags()
	if err := run(o); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.target, "target", "", "host:port of the gRPC server (e.g. monolith:9090)")
	flag.StringVar(&o.content, "content", "", "content id whose manifest is authorized")
	flag.StringVar(&o.token, "token", "", "the caller's real Bearer access token")
	flag.StringVar(&o.mode, "mode", "", "assertion mode: holder-ok | non-holder-denied | threshold-denied | conflict-denied")
	flag.StringVar(&o.capability, "capability", "", "a capability that does NOT belong to --token's caller (conflict-denied only)")
	flag.StringVar(&o.httpBase, "http-base", "", "HTTP base of the same deployment mode, used to fetch a real segment (holder-ok only)")
	flag.Int64Var(&o.expectExpSecond, "expect-exp-seconds", 60, "expected capability lifetime in seconds")
	flag.Parse()

	var missing []string
	if o.target == "" {
		missing = append(missing, "--target")
	}
	if o.content == "" {
		missing = append(missing, "--content")
	}
	if o.token == "" {
		missing = append(missing, "--token")
	}
	if o.mode == "" {
		missing = append(missing, "--mode")
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: missing required flags: %s\n", strings.Join(missing, ", "))
		flag.Usage()
		os.Exit(1)
	}

	switch o.mode {
	case "holder-ok":
		if o.httpBase == "" {
			fmt.Fprintln(os.Stderr, "FAIL: --mode holder-ok requires --http-base")
			os.Exit(1)
		}
	case "non-holder-denied", "threshold-denied":
	case "conflict-denied":
		if o.capability == "" {
			fmt.Fprintln(os.Stderr, "FAIL: --mode conflict-denied requires --capability")
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "FAIL: unknown --mode %q\n", o.mode)
		os.Exit(1)
	}
	return o
}

func run(o options) error {
	conn, err := grpc.NewClient(o.target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("could not create a gRPC client for %s: %w", o.target, err)
	}
	defer conn.Close()

	client := streamingv1.NewStreamingServiceClient(conn)
	switch o.mode {
	case "holder-ok":
		return holderOK(client, o)
	case "non-holder-denied":
		return denied(client, o, "non-holder")
	case "threshold-denied":
		return denied(client, o, "threshold")
	case "conflict-denied":
		return conflictDenied(client, o)
	}
	return fmt.Errorf("unknown --mode %q", o.mode)
}

func callContext(token string, extra ...string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	pairs := append([]string{"authorization", "Bearer " + token}, extra...)
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(pairs...)), cancel
}

// holderOK: the legitimate holder must receive a manifest whose segments carry
// a capability on the shared 60s policy, and must actually fetch one of those
// segment URLs over HTTP with NO Authorization header.
func holderOK(client streamingv1.StreamingServiceClient, o options) error {
	ctx, cancel := callContext(o.token)
	defer cancel()

	resp, err := client.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: o.content})
	if err != nil {
		return fmt.Errorf("GetManifest (holder) failed: %w", err)
	}
	segments := resp.GetSegments()
	if len(segments) == 0 {
		return fmt.Errorf("GetManifest (holder) succeeded but returned no segments")
	}

	firstURL := ""
	for _, seg := range segments {
		if seg.GetUrl() == "" {
			return fmt.Errorf("a segment carries no URL")
		}
		if !strings.Contains(seg.GetUrl(), "playback_token=") {
			return fmt.Errorf("segment URL %q carries no playback_token query parameter", seg.GetUrl())
		}
		if firstURL == "" {
			firstURL = seg.GetUrl()
		}
	}

	capability := playbackTokenFromURL(firstURL)
	if capability == "" {
		return fmt.Errorf("segment URL %q carries an empty playback_token", firstURL)
	}

	exp, err := jwtExpiry(capability)
	if err != nil {
		return err
	}
	remaining := time.Until(time.Unix(exp, 0))
	lo := time.Duration(o.expectExpSecond-5) * time.Second
	hi := time.Duration(o.expectExpSecond+5) * time.Second
	if remaining < lo || remaining > hi {
		return fmt.Errorf("capability expires in %s, outside the %ds shared policy window [%s..%s]",
			remaining.Round(time.Second), o.expectExpSecond, lo, hi)
	}

	// No Authorization header: the capability in the query string is the only
	// credential a media request can carry.
	body, code, err := fetchSegment(o.httpBase, firstURL)
	if err != nil {
		return fmt.Errorf("fetching a segment over HTTP failed: %w", err)
	}
	if code != http.StatusOK {
		return fmt.Errorf("segment fetch returned %d, expected 200", code)
	}
	if len(body) == 0 {
		return fmt.Errorf("segment fetch returned an empty body")
	}
	return nil
}

// denied: the manifest must fail with PermissionDenied. Used for a caller who
// holds nothing (non-holder-denied) and for a holder below the rule threshold
// (threshold-denied).
func denied(client streamingv1.StreamingServiceClient, o options, label string) error {
	ctx, cancel := callContext(o.token)
	defer cancel()

	resp, err := client.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: o.content})
	if err == nil {
		return fmt.Errorf("GetManifest (%s) succeeded with %d segment(s); expected PermissionDenied",
			label, len(resp.GetSegments()))
	}
	if code := status.Code(err); code != codes.PermissionDenied {
		return fmt.Errorf("GetManifest (%s) failed with %s (%v); expected PermissionDenied", label, code, err)
	}
	return nil
}

// conflictDenied: carrying BOTH an access token (wallet B) and a capability
// that belongs to ANOTHER wallet (A) must be refused.
func conflictDenied(client streamingv1.StreamingServiceClient, o options) error {
	ctx, cancel := callContext(o.token, "playback-token", o.capability)
	defer cancel()

	resp, err := client.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: o.content})
	if err == nil {
		return fmt.Errorf("GetManifest with conflicting dual credentials succeeded with %d segment(s)", len(resp.GetSegments()))
	}
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated:
		return nil
	default:
		return fmt.Errorf("GetManifest with conflicting dual credentials failed with %s (%v); expected PermissionDenied or Unauthenticated",
			status.Code(err), err)
	}
}

func playbackTokenFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Query().Get("playback_token")
}

// jwtExpiry decodes the exp claim WITHOUT verifying the signature: this asserts
// which policy the issuer applied (the shared 60s window), not authenticity.
func jwtExpiry(token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, fmt.Errorf("capability is not a three-part JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, fmt.Errorf("capability payload is not base64url: %w", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0, fmt.Errorf("capability payload is not JSON: %w", err)
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		return 0, fmt.Errorf("capability carries no numeric exp claim")
	}
	return int64(exp), nil
}

func fetchSegment(base, segPath string) ([]byte, int, error) {
	target := base + segPath
	if !strings.HasPrefix(segPath, "/") {
		target = strings.TrimRight(base, "/") + "/" + segPath
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

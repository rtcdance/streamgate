// Command grpcparity is the Docker-level parity probe for the HTTP and gRPC
// playback entry points: it talks to the REAL stack over gRPC and asserts the
// authorization decision the two entry points must share. It is run by
// scripts/fullchain-acceptance.sh via `go run ./test/grpcparity`.
//
// Exit 0 only when the asserted outcome holds; otherwise one clear line is
// printed to stderr and the process exits 1.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	authv1 "github.com/rtcdance/streamgate/pkg/api/v1/auth"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type options struct {
	target           string
	content          string
	token            string
	tokenFile        string
	walletSubject    string
	walletChain      int64
	mode             string
	capability       string
	httpBase         string
	expectExpSecond  int64
	evidenceFile     string
	segmentID        string
	deadlineSeconds  float64
	passMarker       string
	elapsedAtStartMS int64
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
	flag.StringVar(&o.tokenFile, "token-file", "", "private atomically updated CW token file; holder-ok or NFT capability-wait")
	flag.StringVar(&o.walletSubject, "wallet-subject", "", "expected contract-wallet subject for --token-file")
	flag.Int64Var(&o.walletChain, "wallet-chain", 0, "expected contract-wallet chain for --token-file")
	flag.StringVar(&o.mode, "mode", "", "assertion mode: holder-ok | capability-wait | non-holder-denied | threshold-denied | threshold-denied-bounded | conflict-denied")
	flag.StringVar(&o.capability, "capability", "", "a capability that does NOT belong to --token's caller (conflict-denied only)")
	flag.StringVar(&o.httpBase, "http-base", "", "HTTP base of the same deployment mode, used to fetch a real segment (holder-ok only)")
	flag.Int64Var(&o.expectExpSecond, "expect-exp-seconds", 60, "expected capability lifetime in seconds")
	flag.StringVar(&o.evidenceFile, "evidence-file", "", "write private holder-ok capability evidence here (never stdout)")
	flag.StringVar(&o.segmentID, "segment-id", "", "segment to probe with capability-wait")
	flag.Float64Var(&o.deadlineSeconds, "deadline-seconds", 70, "capability-wait deadline")
	flag.StringVar(&o.passMarker, "pass-marker", "M3_GRPC_TRANSFER_70S_PASS", "capability-wait success marker")
	flag.Int64Var(&o.elapsedAtStartMS, "elapsed-at-start-ms", 0, "measured monotonic delay since transfer safe-view confirmation")
	flag.Parse()

	var missing []string
	if o.target == "" {
		missing = append(missing, "--target")
	}
	if o.content == "" {
		missing = append(missing, "--content")
	}
	if o.token == "" && o.tokenFile == "" {
		missing = append(missing, "--token or --token-file")
	}
	if o.mode == "" {
		missing = append(missing, "--mode")
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: missing required flags: %s\n", strings.Join(missing, ", "))
		flag.Usage()
		os.Exit(1)
	}
	if o.tokenFile != "" && (o.token != "" || o.walletSubject == "" || o.walletChain <= 0 ||
		(o.mode != "holder-ok" && o.mode != "capability-wait")) {
		fmt.Fprintln(os.Stderr, "FAIL: CW token-file requires an exclusive credential, expected subject/chain and holder-ok or NFT capability-wait")
		os.Exit(1)
	}

	switch o.mode {
	case "holder-ok":
		if o.httpBase == "" {
			fmt.Fprintln(os.Stderr, "FAIL: --mode holder-ok requires --http-base")
			os.Exit(1)
		}
	case "capability-wait":
		if o.evidenceFile == "" {
			fmt.Fprintln(os.Stderr, "FAIL: capability-wait requires a private --evidence-file")
			os.Exit(1)
		}
	case "non-holder-denied", "threshold-denied", "threshold-denied-bounded":
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
	if o.tokenFile != "" {
		var err error
		o.token, err = currentCWToken(o)
		if err != nil {
			return err
		}
	}
	conn, err := grpc.NewClient(o.target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("could not create a gRPC client for %s: %w", o.target, err)
	}
	defer conn.Close()

	client := streamingv1.NewStreamingServiceClient(conn)
	switch o.mode {
	case "holder-ok":
		return holderOK(client, o)
	case "capability-wait":
		return capabilityWait(client, authv1.NewAuthServiceClient(conn), o)
	case "non-holder-denied":
		return denied(client, o, "non-holder")
	case "threshold-denied":
		return denied(client, o, "threshold")
	case "threshold-denied-bounded":
		return deniedWithinBound(client, o, "threshold")
	case "conflict-denied":
		return conflictDenied(client, o)
	}
	return fmt.Errorf("unknown --mode %q", o.mode)
}

func callContext(token string, extra ...string) (context.Context, context.CancelFunc) {
	return callContextTimeout(token, 30*time.Second, extra...)
}

func callContextTimeout(token string, timeout time.Duration, extra ...string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	pairs := append([]string{"authorization", "Bearer " + token}, extra...)
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(pairs...)), cancel
}

// holderOK: the legitimate holder must receive a manifest whose segments carry
// a capability on the shared 60s policy, and must actually fetch one of those
// segment URLs over HTTP with NO Authorization header, then fetch the same
// published bytes through the authenticated GetSegment RPC.
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
			return fmt.Errorf("segment URL carries no playback_token query parameter")
		}
		if firstURL == "" {
			firstURL = seg.GetUrl()
		}
	}

	capability := playbackTokenFromURL(firstURL)
	if capability == "" {
		return fmt.Errorf("segment URL carries an empty playback_token")
	}

	exp, err := jwtExpiry(capability)
	if err != nil {
		return err
	}
	remaining := time.Until(time.Unix(exp, 0))
	lo := time.Duration(o.expectExpSecond-5) * time.Second
	hi := time.Duration(o.expectExpSecond+5) * time.Second
	if o.tokenFile != "" {
		if err := validateCWCapability(o.token, capability, o); err != nil || remaining <= 0 {
			return fmt.Errorf("CW capability is outside the presented credential's identity or lifetime")
		}
	} else if remaining < lo || remaining > hi {
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
	if o.evidenceFile != "" {
		if err := writePrivateEvidence(o.evidenceFile, map[string]string{
			"capability": capability, "segment_id": segments[0].GetSegmentId(),
		}); err != nil {
			return fmt.Errorf("write private gRPC capability evidence: %w", err)
		}
	}
	rpcCtx, rpcCancel := callContext(o.token, "playback-token", capability)
	defer rpcCancel()
	segment, err := client.GetSegment(rpcCtx, &streamingv1.GetSegmentRequest{
		ContentId: o.content, SegmentId: segments[0].GetSegmentId(),
	})
	if err != nil {
		return fmt.Errorf("GetSegment (holder) failed: %w", err)
	}
	if !bytes.Equal(segment.GetData(), body) || segment.GetSize() != int64(len(body)) {
		return fmt.Errorf("gRPC segment bytes/size differ from the protected HTTP media")
	}
	return nil
}

func writePrivateEvidence(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// capabilityWait probes an already-issued capability on real gRPC media. It
// intentionally reports only status and elapsed time; bearer credentials are
// never included in output or acceptance logs.
func capabilityWait(client streamingv1.StreamingServiceClient, auth authv1.AuthServiceClient, o options) error {
	data, err := os.ReadFile(o.evidenceFile)
	if err != nil {
		return fmt.Errorf("read private capability evidence: %w", err)
	}
	if err := os.Remove(o.evidenceFile); err != nil {
		return fmt.Errorf("remove private capability evidence: %w", err)
	}
	var evidence struct {
		Capability string `json:"capability"`
		SegmentID  string `json:"segment_id"`
	}
	if err := json.Unmarshal(data, &evidence); err != nil || evidence.Capability == "" || evidence.SegmentID == "" {
		return fmt.Errorf("private capability evidence is malformed")
	}
	o.capability, o.segmentID = evidence.Capability, evidence.SegmentID
	if o.deadlineSeconds <= 0 || o.deadlineSeconds > 70 || !strings.HasPrefix(o.passMarker, "M3_GRPC_") {
		return fmt.Errorf("capability-wait bound or success marker is invalid")
	}
	if o.elapsedAtStartMS < 0 || time.Duration(o.elapsedAtStartMS)*time.Millisecond >= time.Duration(o.deadlineSeconds*float64(time.Second)) {
		return fmt.Errorf("capability-wait started after its measured deadline")
	}
	start := time.Now().Add(-time.Duration(o.elapsedAtStartMS) * time.Millisecond)
	deadline := start.Add(time.Duration(o.deadlineSeconds * float64(time.Second)))
	if o.tokenFile != "" {
		exp, err := jwtExpiry(o.capability)
		if err != nil || !time.Unix(exp, 0).After(start) {
			return fmt.Errorf("CW capability was already expired at the revocation observation")
		}
	}
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("gRPC capability remained accepted for %.0f seconds after revocation event", o.deadlineSeconds)
		}
		if o.tokenFile != "" {
			denied, err := guardedCWMediaProbe(client, auth, o, remaining)
			if err == nil && denied {
				elapsed := time.Since(start)
				if elapsed > time.Duration(o.deadlineSeconds*float64(time.Second)) {
					return fmt.Errorf("CW capability denial arrived after the measured deadline")
				}
				fmt.Printf("%s elapsed=%s identity_control=valid\n", o.passMarker, elapsed.Round(time.Millisecond))
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("CW media denial was not confirmed with a valid current identity within the deadline")
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}
		ctx, cancel := callContextTimeout(o.token, remaining, "playback-token", o.capability)
		_, err := client.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: o.content, SegmentId: o.segmentID})
		cancel()
		if err != nil {
			code := status.Code(err)
			if code == codes.PermissionDenied || code == codes.Unauthenticated {
				elapsed := time.Since(start)
				if elapsed > time.Duration(o.deadlineSeconds*float64(time.Second)) {
					return fmt.Errorf("gRPC capability was revoked after the deadline (%s)", elapsed.Round(time.Millisecond))
				}
				fmt.Printf("%s elapsed=%s\n", o.passMarker, elapsed.Round(time.Millisecond))
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("gRPC capability remained accepted for %.0f seconds after revocation event", o.deadlineSeconds)
		}
		time.Sleep(250 * time.Millisecond)
	}
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

// deniedWithinBound proves that a changed policy reaches the independent gRPC
// authorization entry point within the stated wall-clock budget. The caller's
// timestamp is captured before submitting the rule update, making the measured
// interval conservative; an HTTP poll cannot consume this deadline first.
func deniedWithinBound(client streamingv1.StreamingServiceClient, o options, label string) error {
	if o.deadlineSeconds <= 0 || o.deadlineSeconds > 5 || !strings.HasPrefix(o.passMarker, "M3_GRPC_") {
		return fmt.Errorf("threshold denial bound or success marker is invalid")
	}
	if o.elapsedAtStartMS < 0 || time.Duration(o.elapsedAtStartMS)*time.Millisecond >= time.Duration(o.deadlineSeconds*float64(time.Second)) {
		return fmt.Errorf("threshold denial probe started after its measured deadline")
	}
	start := time.Now().Add(-time.Duration(o.elapsedAtStartMS) * time.Millisecond)
	deadline := start.Add(time.Duration(o.deadlineSeconds * float64(time.Second)))
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("gRPC manifest remained authorized %.0f seconds after policy update", o.deadlineSeconds)
		}
		ctx, cancel := callContextTimeout(o.token, remaining)
		_, err := client.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: o.content})
		cancel()
		if err != nil && status.Code(err) == codes.PermissionDenied {
			elapsed := time.Since(start)
			if elapsed > time.Duration(o.deadlineSeconds*float64(time.Second)) {
				return fmt.Errorf("gRPC threshold denial arrived after the deadline (%s)", elapsed.Round(time.Millisecond))
			}
			fmt.Printf("%s elapsed=%s\n", o.passMarker, elapsed.Round(time.Millisecond))
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("gRPC threshold check failed without denial: %s", status.Code(err))
			}
			return fmt.Errorf("gRPC manifest remained authorized %.0f seconds after policy update", o.deadlineSeconds)
		}
		time.Sleep(100 * time.Millisecond)
	}
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

func fetchSegment(base, segPath string) (body []byte, status int, err error) {
	target := base + segPath
	if !strings.HasPrefix(segPath, "/") {
		target = strings.TrimRight(base, "/") + "/" + segPath
	}
	req, err := http.NewRequest(http.MethodGet, target, http.NoBody)
	if err != nil {
		return nil, 0, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

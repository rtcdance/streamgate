package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	authv1 "github.com/rtcdance/streamgate/pkg/api/v1/auth"
	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cwClaims struct {
	Subject  string `json:"sub"`
	Wallet   string `json:"wallet_address"`
	Chain    int64  `json:"chain_id"`
	Kind     string `json:"account_kind"`
	Session  string `json:"session_id"`
	Proof    string `json:"wallet_proof_id"`
	Absolute int64  `json:"wallet_session_exp"`
	Issued   int64  `json:"iat"`
	Expires  int64  `json:"exp"`
	Type     string `json:"token_type"`
}

func decodeCWClaims(token string) (cwClaims, error) {
	var claims cwClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, fmt.Errorf("credential is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(payload, &claims) != nil {
		return claims, fmt.Errorf("credential claims are malformed")
	}
	return claims, nil
}

func currentCWToken(o options) (string, error) {
	info, err := os.Stat(o.tokenFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 20*1024 {
		return "", fmt.Errorf("CW token file must be a private regular file within the credential size budget")
	}
	file, err := os.Open(o.tokenFile)
	if err != nil {
		return "", fmt.Errorf("CW token file could not be opened")
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, 20*1024+1))
	if err != nil || len(raw) > 20*1024 {
		return "", fmt.Errorf("CW token file could not be read within its size budget")
	}
	token := strings.TrimSpace(string(raw))
	c, err := decodeCWClaims(token)
	if err != nil || !strings.EqualFold(c.Wallet, o.walletSubject) || !strings.EqualFold(c.Subject, o.walletSubject) ||
		c.Chain != o.walletChain || c.Kind != "erc1271" || !strings.HasPrefix(c.Session, "cw:") || c.Proof == "" ||
		c.Expires <= time.Now().Unix() || c.Issued > time.Now().Unix()+5 || c.Expires <= c.Issued || c.Expires-c.Issued > 60 ||
		c.Absolute < c.Expires || c.Absolute > c.Issued+7200 {
		return "", fmt.Errorf("current CW identity is absent, expired, foreign or outside its lease policy")
	}
	return token, nil
}

// Decoding is a measurement hint; the real AuthService verifies each exact
// credential before AND after media denial. A rotated/revoked JTI alone must
// never count as proof that an NFT transfer revoked a playback capability.
func validateCWCapability(token, capability string, o options) error {
	identity, err := decodeCWClaims(token)
	if err != nil {
		return err
	}
	media, err := decodeCWClaims(capability)
	if err != nil || media.Type != "playback" || media.Kind != "erc1271" || media.Chain != o.walletChain ||
		!strings.EqualFold(media.Wallet, o.walletSubject) || media.Session != identity.Session || media.Proof != identity.Proof ||
		media.Absolute != identity.Absolute || media.Expires > identity.Absolute || media.Expires <= media.Issued || media.Expires-media.Issued > 60 {
		return fmt.Errorf("CW media capability has a different identity, admission or lifetime")
	}
	// Renewals may extend a NEW capability, never one issued under an older lease.
	if media.Issued >= identity.Issued && media.Expires > identity.Expires {
		return fmt.Errorf("CW capability outlives its presented credential")
	}
	return nil
}

func guardedCWMediaProbe(client streamingv1.StreamingServiceClient, auth authv1.AuthServiceClient, o options, timeout time.Duration) (bool, error) {
	token, err := currentCWToken(o)
	if err != nil {
		return false, err
	}
	if err := validateCWCapability(token, o.capability, o); err != nil {
		return false, err
	}
	ctx, cancel := callContextTimeout(token, timeout, "playback-token", o.capability)
	defer cancel()
	before, err := auth.VerifyToken(ctx, &authv1.VerifyTokenRequest{Token: token})
	if err != nil || !before.GetValid() {
		return false, fmt.Errorf("current CW identity control failed before media probe")
	}
	_, err = client.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: o.content, SegmentId: o.segmentID})
	if err == nil {
		return false, nil
	}
	if code := status.Code(err); code != codes.PermissionDenied && code != codes.Unauthenticated {
		return false, fmt.Errorf("media probe failed without an authorization denial: %s", code)
	}
	after, err := auth.VerifyToken(ctx, &authv1.VerifyTokenRequest{Token: token})
	if err != nil || !after.GetValid() {
		return false, fmt.Errorf("CW identity expired or rotated during media probe; this is not transfer evidence")
	}
	return true, nil
}

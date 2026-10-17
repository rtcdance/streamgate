package middleware

import (
	"context"
	"errors"

	"github.com/golang-jwt/jwt/v4"
)

// AccessDecision is the outcome of applying the access-token policy to verified
// claims: the identity, session and epoch the caller is acting under.
type AccessDecision struct {
	Wallet    string
	SessionID string
	AuthEpoch int64
}

// AccessPolicy is the shared access-token policy. Both transports apply it to
// already-verified claims, so HTTP and gRPC cannot drift apart.
type AccessPolicy struct {
	// Issuer, when set, must match the token's `iss` exactly.
	Issuer string
	// Audience, when set, must be present in the token's `aud`.
	Audience string
	// SessionValidator checks the session POSITIVELY: it must exist and its
	// epoch must be current. A nil validator still requires the session fields
	// to be present, so a credential predating the scheme is refused.
	SessionValidator SessionValidator
	// Blacklist, when set, refuses a token whose jti has been revoked.
	Blacklist TokenBlacklistChecker
}

// The typed policy errors. They carry the exact message and code the HTTP
// wrapper previously returned, so a caller maps them back without duplicating
// the strings.
type accessPolicyError struct {
	msg  string
	code string
}

func (e *accessPolicyError) Error() string { return e.msg }

var (
	ErrAccessNotAccessToken   = &accessPolicyError{msg: "token is not an access token", code: "UNAUTHORIZED"}
	ErrAccessIssuerMismatch   = &accessPolicyError{msg: "token issuer mismatch", code: "UNAUTHORIZED"}
	ErrAccessAudienceMismatch = &accessPolicyError{msg: "token audience mismatch", code: "UNAUTHORIZED"}
	ErrAccessNotSessionBound  = &accessPolicyError{msg: "token is not bound to a session", code: "UNAUTHORIZED"}
	ErrAccessNoEpoch          = &accessPolicyError{msg: "token carries no auth epoch", code: "UNAUTHORIZED"}
	ErrAccessSessionInvalid   = &accessPolicyError{msg: "session is no longer valid", code: "UNAUTHORIZED"}
	ErrAccessRevoked          = &accessPolicyError{msg: "token revoked", code: "TOKEN_REVOKED"}
	ErrAccessNoWallet         = &accessPolicyError{msg: "token missing wallet address", code: "UNAUTHORIZED"}
)

// AccessErrorBody returns the HTTP status/body/code a policy error maps to. It
// keeps the wrapper byte-identical to the previous inline checks.
func AccessErrorBody(err error) (int, string, string) {
	var pe *accessPolicyError
	if errors.As(err, &pe) {
		return 401, pe.msg, pe.code
	}
	return 401, "invalid token", "UNAUTHORIZED"
}

// AuthorizeAccessClaims applies the access-token policy to parsed claims:
// credential kind, issuer, audience, live session binding, revocation and the
// wallet claim. It returns a typed error instead of aborting a transport, so
// both the HTTP middleware and the gRPC interceptor share ONE implementation.
func AuthorizeAccessClaims(ctx context.Context, policy AccessPolicy, claims jwt.MapClaims) (AccessDecision, error) {
	// Only an access token may authorize a management or streaming API call. A
	// playback capability is spent on the streaming routes only. An absent
	// token_type is refused outright, so a credential predating the scheme is
	// not grandfathered in.
	if tokenType, _ := claims["token_type"].(string); tokenType != "access" {
		return AccessDecision{}, ErrAccessNotAccessToken
	}

	if policy.Issuer != "" {
		if iss, _ := claims["iss"].(string); iss != policy.Issuer {
			return AccessDecision{}, ErrAccessIssuerMismatch
		}
	}

	if policy.Audience != "" && !audienceContains(claims["aud"], policy.Audience) {
		return AccessDecision{}, ErrAccessAudienceMismatch
	}

	// Every credential must be bound to a live session.
	sessionID, _ := claims["session_id"].(string)
	if sessionID == "" {
		return AccessDecision{}, ErrAccessNotSessionBound
	}
	epoch, hasEpoch := numericClaim(claims["auth_epoch"])
	if !hasEpoch {
		return AccessDecision{}, ErrAccessNoEpoch
	}
	if policy.SessionValidator != nil && !policy.SessionValidator.SessionValid(sessionID, epoch) {
		return AccessDecision{}, ErrAccessSessionInvalid
	}

	if policy.Blacklist != nil {
		if jti, _ := claims["jti"].(string); jti != "" && policy.Blacklist.IsTokenRevoked(ctx, jti) {
			return AccessDecision{}, ErrAccessRevoked
		}
	}

	walletAddress, _ := claims["wallet_address"].(string)
	if walletAddress == "" {
		return AccessDecision{}, ErrAccessNoWallet
	}

	return AccessDecision{Wallet: walletAddress, SessionID: sessionID, AuthEpoch: epoch}, nil
}

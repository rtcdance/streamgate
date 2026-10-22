package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"

	"go.uber.org/zap"
)

// PolicySnapshot is ONE consistent read of a content's authorization policy:
// the active rules, the rule version they were read at, and the time the
// authority was read. Both transports authorize against it, and a capability is
// minted from a decision taken over exactly this snapshot.
type PolicySnapshot struct {
	ContentID  string
	Rules      []middleware.GatingRule
	Version    int64
	VerifiedAt time.Time
}

// PolicySnapshotResolver reads one policy snapshot per call.
type PolicySnapshotResolver interface {
	ResolvePolicySnapshot(ctx context.Context, contentID string) (PolicySnapshot, error)
}

// SessionBinding is the login session a capability must inherit.
type SessionBinding struct {
	SessionID    string
	AuthEpoch    int64
	RestoreEpoch string
}

// Authorization errors. Transports map them to their own status codes; the
// business layer never speaks HTTP or gRPC codes.
var (
	// ErrAuthorizationSessionRequired means the caller supplied no session, so
	// there is nothing a capability could inherit.
	ErrAuthorizationSessionRequired = errors.New("a capability must inherit the caller's session")
	// ErrNoGatingRules means the content resolved to no active rules. The gate
	// fails closed on this.
	ErrNoGatingRules = errors.New("no active gating rules for this content")
	// ErrPlaybackDenied means a snapshot was read successfully but no rule
	// granted access.
	ErrPlaybackDenied = errors.New("nft access denied")
	// ErrAuthorizationUnavailable means the authorization could not be
	// performed at all (no resolver/gate, or an unreadable authority). A caller
	// must treat this as a service failure, not a denial.
	ErrAuthorizationUnavailable = errors.New("authorization is unavailable")
)

// authorizationSeal is the unexported marker only this package can set. A
// PlaybackAuthorization with the zero value cannot be used to issue anything, so
// a transport can never assemble a valid authorization itself.
var authorizationSeal = &struct{}{}

// PlaybackAuthorization is an IMMUTABLE authorization result. Every field is
// unexported and is set ONLY from the snapshot and the decision that granted —
// never from the caller — so a capability can only be minted from a real
// decision over real evidence.
type PlaybackAuthorization struct {
	seal      *struct{}
	wallet    string
	contentID string
	rule      middleware.GatingRule
	// ruleVersion is the version of the snapshot that ACTUALLY granted. It is
	// never re-read at issuance, so an older ruling cannot be re-labelled with
	// a newer version.
	ruleVersion int64
	// blockNumber/blockHash are the chain evidence the grant was read at.
	blockNumber uint64
	blockHash   string
	session     SessionBinding
	// evidenceObservedAt is the authority's read time for the granting snapshot.
	evidenceObservedAt time.Time
}

// Wallet is the subject the authorization was granted to.
func (a *PlaybackAuthorization) Wallet() string { return a.wallet }

// ContentID is the content the authorization covers.
func (a *PlaybackAuthorization) ContentID() string { return a.contentID }

// Rule is the rule that granted access.
func (a *PlaybackAuthorization) Rule() middleware.GatingRule { return a.rule }

// RuleVersion is the version of the snapshot that granted.
func (a *PlaybackAuthorization) RuleVersion() int64 { return a.ruleVersion }

// BlockEvidence is the block the grant was read at.
func (a *PlaybackAuthorization) BlockEvidence() (blockNumber uint64, blockHash string) {
	return a.blockNumber, a.blockHash
}

// Session is the login session the capability will inherit.
func (a *PlaybackAuthorization) Session() SessionBinding { return a.session }

// EvidenceObservedAt is when the granting snapshot's authority was read.
func (a *PlaybackAuthorization) EvidenceObservedAt() time.Time { return a.evidenceObservedAt }

// publicAuthorization is the authorization for an explicitly public content: the
// gate was configured "open", so there is no rule to name and no chain evidence
// to report. It still carries the snapshot's rule version, so writing a rule for
// that content later invalidates the capability without any per-segment read.
func publicAuthorization(contentID, wallet string, session SessionBinding, snapshot PolicySnapshot) *PlaybackAuthorization {
	return &PlaybackAuthorization{
		seal:               authorizationSeal,
		wallet:             wallet,
		contentID:          contentID,
		ruleVersion:        snapshot.Version,
		session:            session,
		evidenceObservedAt: snapshot.VerifiedAt,
	}
}

// PlaybackAuthorizer performs the whole authorization: resolve a policy snapshot,
// evaluate it against the real on-chain quantity, and — if the rule version
// moved before issuance — re-authorize once against the fresh snapshot. A second
// change is a refusal, never an unbounded retry.
type PlaybackAuthorizer struct {
	gate     *middleware.NFTGateConfig
	resolver PolicySnapshotResolver
	logger   *zap.Logger
}

// NewPlaybackAuthorizer builds an authorizer over the SAME gate configuration
// the HTTP gate uses (shared evidence cache included), so both entry points
// decide over one set of chain evidence.
func NewPlaybackAuthorizer(gate *middleware.NFTGateConfig, resolver PolicySnapshotResolver, logger *zap.Logger) *PlaybackAuthorizer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &PlaybackAuthorizer{gate: gate, resolver: resolver, logger: logger}
}

// AuthorizePlayback authorizes a wallet for a content under the given session
// binding. It refuses an empty session (a capability must inherit one), a missing
// resolver or gate, no rules (fail-closed) and no granting rule. If the rule
// version advances between the granting snapshot and issuance, it re-authorizes
// fully ONCE against the fresh snapshot; a second advance refuses.
func (a *PlaybackAuthorizer) AuthorizePlayback(ctx context.Context, contentID, wallet string, session SessionBinding) (*PlaybackAuthorization, error) {
	if session.SessionID == "" {
		return nil, ErrAuthorizationSessionRequired
	}
	if a == nil || a.resolver == nil {
		return nil, fmt.Errorf("%w: no policy snapshot resolver", ErrAuthorizationUnavailable)
	}
	if a.gate == nil {
		return nil, fmt.Errorf("%w: no gate configuration", ErrAuthorizationUnavailable)
	}

	first, err := a.resolver.ResolvePolicySnapshot(ctx, contentID)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to resolve the policy snapshot: %v", ErrAuthorizationUnavailable, err)
	}
	if len(first.Rules) == 0 {
		// The gate's own rule: no rules is a denial unless the operator explicitly
		// opted into "open" content. Honouring it here keeps the two entry points
		// on one policy instead of letting the HTTP gate allow and the issuance
		// path then refuse the same request.
		if a.gate.GatingMode == "open" {
			return publicAuthorization(contentID, wallet, session, first), nil
		}
		return nil, ErrNoGatingRules
	}

	decision, err := middleware.EvaluatePolicy(ctx, a.gate, a.logger, first.Rules, wallet)
	if err != nil {
		return nil, err
	}
	if !decision.Allowed {
		return nil, ErrPlaybackDenied
	}

	granting := first
	// Re-resolve before issuance. If the version moved, the decision may no
	// longer be valid, so re-authorize FULLY against the fresh snapshot rather
	// than minting a capability carrying the new version over an old decision.
	second, err := a.resolver.ResolvePolicySnapshot(ctx, contentID)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to resolve the policy snapshot: %v", ErrAuthorizationUnavailable, err)
	}
	if second.Version != first.Version {
		if len(second.Rules) == 0 {
			if a.gate.GatingMode == "open" {
				return publicAuthorization(contentID, wallet, session, second), nil
			}
			return nil, ErrNoGatingRules
		}
		decision, err = middleware.EvaluatePolicy(ctx, a.gate, a.logger, second.Rules, wallet)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			return nil, ErrPlaybackDenied
		}
		granting = second

		// A SECOND change while re-authorizing is a refusal, not a retry.
		third, err := a.resolver.ResolvePolicySnapshot(ctx, contentID)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to resolve the policy snapshot: %v", ErrAuthorizationUnavailable, err)
		}
		if third.Version != second.Version {
			return nil, fmt.Errorf("rule version changed again during issuance (%d -> %d); refusing", second.Version, third.Version)
		}
	}

	if decision.GrantingRule == nil {
		return nil, ErrPlaybackDenied
	}
	if decision.EvidenceObservedAt.IsZero() || time.Since(decision.EvidenceObservedAt) > middleware.EvidenceAgeBound(a.gate) {
		return nil, fmt.Errorf("NFT authorization evidence exceeded its freshness bound before capability issuance")
	}

	authz := &PlaybackAuthorization{
		seal:               authorizationSeal,
		wallet:             wallet,
		contentID:          contentID,
		rule:               *decision.GrantingRule,
		ruleVersion:        granting.Version,
		session:            session,
		evidenceObservedAt: decision.EvidenceObservedAt,
	}
	if decision.Anchor != nil {
		authz.blockNumber = decision.Anchor.Number
		authz.blockHash = decision.Anchor.Hash
	}
	return authz, nil
}

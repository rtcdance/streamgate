package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
)

type cachedRules struct {
	rules []middleware.GatingRule
	// version is the rule version these rules were read at. A cached rule set is
	// only reusable while the authority still reports this version, so a write in
	// ANOTHER process is noticed too — a local flush only covers this one.
	version   int64
	expiresAt time.Time
}

type gatingRuleResolverAdapter struct {
	svc *service.GatingRuleService
	// versions is the authority for "have the rules changed?". Without it the
	// only signal is the local TTL, which cannot see another process's write.
	versions storage.RuleVersionStore

	mu       sync.RWMutex
	cache    map[string]cachedRules
	cacheTTL time.Duration
}

func NewGatingRuleResolverAdapter(svc *service.GatingRuleService) middleware.GatingRuleResolver {
	return newGatingRuleResolverAdapter(svc, nil)
}

// NewVersionedGatingRuleResolverAdapter resolves rules through the rule-version
// authority: a cached set is reused only while the version still matches, so a
// change made anywhere becomes visible within the version's age bound instead of
// waiting out the cache TTL.
func NewVersionedGatingRuleResolverAdapter(svc *service.GatingRuleService, versions storage.RuleVersionStore) middleware.GatingRuleResolver {
	return newGatingRuleResolverAdapter(svc, versions)
}

func newGatingRuleResolverAdapter(svc *service.GatingRuleService, versions storage.RuleVersionStore) *gatingRuleResolverAdapter {
	a := &gatingRuleResolverAdapter{
		svc:      svc,
		versions: versions,
		cache:    make(map[string]cachedRules),
		cacheTTL: 30 * time.Second,
	}
	// Rule writes must be visible immediately. Relying on the TTL alone means an
	// owner can change a rule and still be authorised by the previous one for up
	// to 30s - and a rule retargeted at a different contract keeps authorising
	// against the old one for that whole window.
	if svc != nil {
		svc.SetInvalidator(a.Invalidate)
	}
	return a
}

// Invalidate drops cached rules. An empty contentID drops everything, which is
// what a delete needs: the rule is gone before we can learn which content it
// belonged to.
func (a *gatingRuleResolverAdapter) Invalidate(contentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if contentID == "" {
		a.cache = make(map[string]cachedRules)
		return
	}
	delete(a.cache, contentID)
}

func (a *gatingRuleResolverAdapter) GetActiveRulesForContent(ctx context.Context, contentID string) ([]middleware.GatingRule, error) {
	// Ask the authority which version is current BEFORE trusting anything
	// cached. An unreadable authority is a refusal: a version that cannot be
	// checked is not a version that matches, and serving the cached rules anyway
	// would extend the window the bound exists to cap.
	var current int64
	if a.versions != nil {
		version, err := a.versions.Read(ctx, contentID)
		if err != nil {
			return nil, err
		}
		current = version.Version

		a.mu.RLock()
		cached, ok := a.cache[contentID]
		a.mu.RUnlock()
		if ok && cached.version == current && time.Now().Before(cached.expiresAt) {
			return cached.rules, nil
		}
	} else {
		a.mu.RLock()
		cached, ok := a.cache[contentID]
		a.mu.RUnlock()
		if ok && time.Now().Before(cached.expiresAt) {
			return cached.rules, nil
		}
	}

	rules, err := a.svc.GetActiveRulesForContent(ctx, contentID)
	if err != nil {
		return nil, err
	}

	result := make([]middleware.GatingRule, len(rules))
	for i, r := range rules {
		result[i] = middleware.GatingRule{
			ContractAddress: r.ContractAddress,
			TokenID:         r.TokenID,
			ChainID:         r.ChainID,
			Standard:        r.Standard,
			MinBalance:      r.MinBalance,
		}
	}

	a.mu.Lock()
	a.cache[contentID] = cachedRules{rules: result, version: current, expiresAt: time.Now().Add(a.cacheTTL)}
	a.mu.Unlock()

	return result, nil
}

// RuleVersion implements middleware.GatingRuleVersionedResolver: it reads the
// CURRENT version from the authority. The gate uses it to label
// GateDecisionTotal with the rule revision its decision was made under; a
// missing authority version store reports "0".
func (a *gatingRuleResolverAdapter) RuleVersion(ctx context.Context, contentID string) (int64, error) {
	if a.versions == nil {
		return 0, nil
	}
	v, err := a.versions.Read(ctx, contentID)
	if err != nil {
		return 0, err
	}
	return v.Version, nil
}

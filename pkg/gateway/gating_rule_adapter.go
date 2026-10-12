package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
)

type cachedRules struct {
	rules     []middleware.GatingRule
	expiresAt time.Time
}

type gatingRuleResolverAdapter struct {
	svc      *service.GatingRuleService
	mu       sync.RWMutex
	cache    map[string]cachedRules
	cacheTTL time.Duration
}

func NewGatingRuleResolverAdapter(svc *service.GatingRuleService) middleware.GatingRuleResolver {
	a := &gatingRuleResolverAdapter{
		svc:      svc,
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
	a.mu.RLock()
	if cached, ok := a.cache[contentID]; ok && time.Now().Before(cached.expiresAt) {
		a.mu.RUnlock()
		return cached.rules, nil
	}
	a.mu.RUnlock()

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
	a.cache[contentID] = cachedRules{rules: result, expiresAt: time.Now().Add(a.cacheTTL)}
	a.mu.Unlock()

	return result, nil
}

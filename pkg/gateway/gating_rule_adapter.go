package gateway

import (
	"context"
	"sync"
	"time"

	"streamgate/pkg/middleware"
	"streamgate/pkg/service"
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
	return &gatingRuleResolverAdapter{
		svc:      svc,
		cache:    make(map[string]cachedRules),
		cacheTTL: 30 * time.Second,
	}
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

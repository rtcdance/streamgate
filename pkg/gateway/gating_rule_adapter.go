package gateway

import (
	"context"

	"streamgate/pkg/middleware"
	"streamgate/pkg/service"
)

type gatingRuleResolverAdapter struct {
	svc *service.GatingRuleService
}

func NewGatingRuleResolverAdapter(svc *service.GatingRuleService) middleware.GatingRuleResolver {
	return &gatingRuleResolverAdapter{svc: svc}
}

func (a *gatingRuleResolverAdapter) GetActiveRulesForContent(ctx context.Context, contentID string) ([]middleware.GatingRule, error) {
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
	return result, nil
}

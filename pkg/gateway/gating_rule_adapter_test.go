package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGatingRuleResolverAdapter_Caching(t *testing.T) {
	svc := service.NewGatingRuleService(nil, zap.NewNop())
	adapter := NewGatingRuleResolverAdapter(svc)

	adapter.(*gatingRuleResolverAdapter).cache["c1"] = cachedRules{
		rules: []middleware.GatingRule{
			{ContractAddress: "0xContract", TokenID: "1", ChainID: 1},
		},
		expiresAt: time.Now().Add(30 * time.Second),
	}

	rules, err := adapter.GetActiveRulesForContent(context.Background(), "c1")
	require.NoError(t, err)
	assert.Len(t, rules, 1)
	assert.Equal(t, "0xContract", rules[0].ContractAddress)
}

func TestGatingRuleResolverAdapter_ExpiredCache(t *testing.T) {
	svc := service.NewGatingRuleService(nil, zap.NewNop())
	adapter := NewGatingRuleResolverAdapter(svc)

	adapter.(*gatingRuleResolverAdapter).cache["c1"] = cachedRules{
		rules:     []middleware.GatingRule{},
		expiresAt: time.Now().Add(-time.Hour),
	}

	_, err := adapter.GetActiveRulesForContent(context.Background(), "c1")
	assert.Error(t, err)
}

func TestGatingRuleResolverAdapter_CacheTTL(t *testing.T) {
	svc := service.NewGatingRuleService(nil, zap.NewNop())
	adapter := NewGatingRuleResolverAdapter(svc)
	assert.Equal(t, 30*time.Second, adapter.(*gatingRuleResolverAdapter).cacheTTL)
}

func TestGatingRuleResolverAdapter_CacheMiss_NilDB(t *testing.T) {
	svc := service.NewGatingRuleService(nil, zap.NewNop())
	adapter := NewGatingRuleResolverAdapter(svc)

	_, err := adapter.GetActiveRulesForContent(context.Background(), "nonexistent")
	assert.Error(t, err)
}

package middleware

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestGateDecision_RuleEditsDoNotCreateSeries(t *testing.T) {
	resolver := &mockVersionedRuleResolver{
		mockGatingRuleResolver: mockGatingRuleResolver{rules: []GatingRule{
			{ContractAddress: testContractAddr, ChainID: 1, Standard: "erc721", MinBalance: 1},
		}},
		version: 1,
	}
	cfg := &NFTGateConfig{
		RuleResolver: resolver,
		BlockProver:  &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xanchor"}},
		Verifier: &mockNFTOwnershipCheckerOld{
			balanceFn: func(context.Context, int64, string, string) (*big.Int, error) {
				return big.NewInt(1), nil
			},
		},
		DefaultChainID: 1,
	}
	core, observed := observer.New(zap.DebugLevel)
	log := zap.New(core)
	router := gin.New()
	router.Use(JWTAuthMiddleware(JWTAuthConfig{Secret: "test-secret-that-is-at-least-32-chars"}, log))
	router.Use(NFTGateMiddleware(cfg, log))
	router.GET("/stream/:id/manifest.m3u8", func(c *gin.Context) { c.Status(http.StatusOK) })
	request := func() {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, authRequestWithWallet("/stream/content-metrics/manifest.m3u8", "0xOwner"))
		require.Equal(t, http.StatusOK, w.Code)
	}
	seriesCount := func() int {
		families, err := prometheus.DefaultGatherer.Gather()
		require.NoError(t, err)
		for _, family := range families {
			if family.GetName() != "streamgate_gate_decisions_total" {
				continue
			}
			for _, metric := range family.GetMetric() {
				require.Len(t, metric.GetLabel(), 2)
				for _, label := range metric.GetLabel() {
					require.Contains(t, []string{"decision", "reason"}, label.GetName())
				}
			}
			return len(family.GetMetric())
		}
		t.Fatal("gate decision metric was not recorded")
		return 0
	}
	request()
	before := seriesCount()
	for version := int64(2); version <= 51; version++ {
		resolver.version = version
		request()
	}
	require.Equal(t, before, seriesCount(), "rule edits must reuse the same outcome/reason series")
	entries := observed.FilterMessage("NFT gate decision").All()
	require.Len(t, entries, 51)
	fields := entries[len(entries)-1].ContextMap()
	require.Equal(t, "51", fields["rule_version"])
	require.Equal(t, "allow", fields["decision"])
	require.Equal(t, "ok", fields["reason"])
	require.Len(t, fields, 3, "decision logs must not include wallet credentials or capability URLs")
}

func TestNFTMetrics_UnknownChainsUseBoundedBucket(t *testing.T) {
	labels := map[string]string{"chain_id": "other", "result": "false"}
	before := metricValue(t, "streamgate_nft_verify_total", labels)
	cfg := &NFTGateConfig{
		BlockProver: &mockBlockProver{header: &BlockHeaderInfo{Number: 100, Hash: "0xanchor"}},
		Verifier: &mockNFTOwnershipCheckerOld{quantityFn: func(context.Context, int64, string, string, string) (*big.Int, error) {
			return nil, errors.New("unsupported RPC chain")
		}},
	}
	for chain := int64(900000000); chain < 900000050; chain++ {
		decision, _ := EvaluatePolicy(context.Background(), cfg, zap.NewNop(), []GatingRule{
			{ContractAddress: testContractAddr, ChainID: chain, Standard: "erc721", MinBalance: 1},
		}, "0xOwner")
		require.False(t, decision.Allowed)
	}
	after := metricValue(t, "streamgate_nft_verify_total", labels)
	require.Equal(t, before+50, after, "unsupported rule chain IDs must reuse one bounded metric bucket")
}

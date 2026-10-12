//go:build anvil

package web3_test

// SG-01 integration: a real ERC-1155 contract, read through the real
// Web3Service, authorized by the real NFTGateMiddleware. This is the only test
// that proves quantity semantics end to end instead of through a mock.

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type staticRuleResolver struct{ rules []middleware.GatingRule }

func (r staticRuleResolver) GetActiveRulesForContent(_ context.Context, _ string) ([]middleware.GatingRule, error) {
	return r.rules, nil
}

// gateRouter wires the real gate with a pre-authenticated wallet, so the test
// exercises rule resolution, quantity verification and the decision itself.
func gateRouter(verifier middleware.NFTOwnershipChecker, wallet string, rules ...middleware.GatingRule) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", wallet)
		c.Next()
	})
	r.Use(middleware.NFTGateMiddleware(&middleware.NFTGateConfig{
		Verifier:       verifier,
		RuleResolver:   staticRuleResolver{rules: rules},
		DefaultChainID: anvilChainID,
	}, zap.NewNop()))
	r.GET("/stream/:id/manifest.m3u8", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func gateStatus(t *testing.T, r *gin.Engine, path string) int {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	return w.Code
}

func TestAnvil_ERC1155_Web3Service_NFTGate(t *testing.T) {
	rpcURL, cleanup := startAnvil(t)
	defer cleanup()

	erc1155ABI, erc1155Bin := loadArtifact(t, "contracts/out/DemoERC1155.sol/DemoERC1155.json")

	ethCli, err := ethclient.Dial(rpcURL)
	require.NoError(t, err)
	defer ethCli.Close()

	auth := transactor(t, anvilKey(t, 0))
	wallet := auth.From.Hex()

	contractAddr, _ := deploy(t, ethCli, auth, erc1155ABI, erc1155Bin)
	contract := bind.NewBoundContract(contractAddr, erc1155ABI, ethCli, ethCli, ethCli)

	const tokenID = "7"

	cfg := &config.Config{}
	cfg.Web3.ChainID = anvilChainID
	cfg.Web3.Chains = []config.ChainConfigEntry{{ID: anvilChainID, Name: "Anvil", RPC: rpcURL}}

	svc, err := service.NewWeb3Service(service.DefaultWeb3Deps(cfg, zap.NewNop()), cfg, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// ERC-1155 has no collection-wide quantity: asking for one must error
	// rather than invent a total that a min>1 rule could be measured against.
	_, err = svc.GetNFTOwnershipQuantity(ctx, anvilChainID, contractAddr.Hex(), "", wallet)
	require.Error(t, err, "ERC-1155 collection quantity must be refused")

	ruleMin1 := middleware.GatingRule{ContractAddress: contractAddr.Hex(), TokenID: tokenID, ChainID: anvilChainID, Standard: "erc1155", MinBalance: 1}
	ruleMin2 := middleware.GatingRule{ContractAddress: contractAddr.Hex(), TokenID: tokenID, ChainID: anvilChainID, Standard: "erc1155", MinBalance: 2}

	// --- balance 1: min=1 allows, min=2 denies ---
	rc1 := transact(t, ethCli, auth, contract, "mint", auth.From, big.NewInt(7), big.NewInt(1))
	mineUntilSafe(t, rpcURL, rc1.BlockNumber.Uint64())

	qty1, err := svc.GetNFTOwnershipQuantity(ctx, anvilChainID, contractAddr.Hex(), tokenID, wallet)
	require.NoError(t, err)
	require.Equal(t, 0, qty1.Cmp(big.NewInt(1)), "on-chain ERC-1155 quantity must be 1")

	require.Equal(t, http.StatusOK, gateStatus(t, gateRouter(svc, wallet, ruleMin1), "/stream/c1/manifest.m3u8"),
		"balance 1 must satisfy min=1")
	require.Equal(t, http.StatusForbidden, gateStatus(t, gateRouter(svc, wallet, ruleMin2), "/stream/c1/manifest.m3u8"),
		"balance 1 must NOT satisfy min=2")

	// --- balance 2: min=2 now allows ---
	rc2 := transact(t, ethCli, auth, contract, "mint", auth.From, big.NewInt(7), big.NewInt(1))
	mineUntilSafe(t, rpcURL, rc2.BlockNumber.Uint64())

	qty2, err := svc.GetNFTOwnershipQuantity(ctx, anvilChainID, contractAddr.Hex(), tokenID, wallet)
	require.NoError(t, err)
	require.Equal(t, 0, qty2.Cmp(big.NewInt(2)), "on-chain ERC-1155 quantity must be 2")

	require.Equal(t, http.StatusOK, gateStatus(t, gateRouter(svc, wallet, ruleMin2), "/stream/c1/manifest.m3u8"),
		"balance 2 must satisfy min=2")
}

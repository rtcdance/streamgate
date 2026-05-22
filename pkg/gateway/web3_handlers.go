package gateway

import (
	"time"

	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Web3StatusProvider provides RPC status and chain configuration information.
type Web3StatusProvider interface {
	GetRPCStatuses() map[int64][]web3.RPCStatus
	GetSupportedChains() []*web3.ChainConfig
}

// RegisterWeb3Routes registers Web3 RPC status routes.
func RegisterWeb3Routes(router *gin.Engine, log *zap.Logger, web3Svc Web3StatusProvider) {
	w3 := router.Group(APIPrefix + "/web3")
	w3.GET("/rpc-status", func(c *gin.Context) {
		statusesByChain := web3Svc.GetRPCStatuses()
		chains := web3Svc.GetSupportedChains()
		nameByChain := make(map[int64]string, len(chains))
		for _, chain := range chains {
			nameByChain[chain.ID] = chain.Name
		}
		response := make([]gin.H, 0, len(statusesByChain))
		for chainID, statuses := range statusesByChain {
			rpcs := make([]gin.H, 0, len(statuses))
			for _, status := range statuses {
				rpc := gin.H{"url": status.URL, "is_active": status.IsActive, "failures": status.Failures}
				if !status.LastFailureAt.IsZero() {
					rpc["last_failure_at"] = status.LastFailureAt.Format(time.RFC3339)
				}
				if !status.CooldownUntil.IsZero() {
					rpc["cooldown_until"] = status.CooldownUntil.Format(time.RFC3339)
				}
				rpcs = append(rpcs, rpc)
			}
			response = append(response, gin.H{"chain_id": chainID, "name": nameByChain[chainID], "rpcs": rpcs})
		}
		respondOK(c, gin.H{"chains": response})
	})
	log.Info("Web3 routes registered")
}

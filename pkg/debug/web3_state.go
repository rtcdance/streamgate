package debug

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"streamgate/pkg/web3"
)

// Web3StateHandler returns an HTTP handler that exposes the internal state
// of Web3 components: RPC connections, scores, cache stats, reorg detector,
// event indexer status, and multi-chain configuration.
//
// This is primarily a learning/debugging tool: you can observe how the system
// reacts to RPC failures, cache hits, reorgs, and multi-chain routing in real time.
// Protected by DEBUG_ENABLED env var to prevent production exposure.
func Web3StateHandler(mcm *web3.MultiChainManager, nftVerifier *web3.NFTVerifier, reorgDetector *web3.ReorgDetector, eventIndexer *web3.EventIndexer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("DEBUG_ENABLED") != "true" {
			http.Error(w, "debug endpoints are disabled; set DEBUG_ENABLED=true", http.StatusForbidden)
			return
		}

		state := map[string]interface{}{
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}

		if mcm != nil {
			chainInfo := make(map[int64]interface{})
			for chainID, cfg := range web3.SupportedChains {
				entry := map[string]interface{}{
					"name":      cfg.Name,
					"is_testnet": cfg.IsTestnet,
					"currency":  cfg.Currency,
				}

				if client, err := mcm.GetClient(chainID); err == nil {
					statuses := client.GetRPCStatuses()
					scores := client.GetRPCScores()

					rpcList := make([]map[string]interface{}, len(statuses))
					for i, st := range statuses {
						rpcList[i] = map[string]interface{}{
							"url":            st.URL,
							"is_active":      st.IsActive,
							"score":          scores[st.URL],
							"failures":       st.Failures,
							"cooldown_until": st.CooldownUntil.Format(time.RFC3339),
						}
					}
					entry["rpcs"] = rpcList
				}
				chainInfo[chainID] = entry
			}
			state["chains"] = chainInfo

			allStatuses := mcm.GetRPCStatuses()
			state["rpc_status_count"] = len(allStatuses)
		}

		if nftVerifier != nil {
			state["nft_verifier"] = map[string]interface{}{
				"configured": true,
			}
		}

		if reorgDetector != nil {
			detectorState := map[string]interface{}{
				"configured": true,
				"max_blocks": "256",
			}
			if rd, ok := interface{}(reorgDetector).(interface{ Stats() map[string]interface{} }); ok {
				detectorState["stats"] = rd.Stats()
			}
			state["reorg_detector"] = detectorState
		}

		if eventIndexer != nil {
			indexerState := map[string]interface{}{
				"configured":   true,
				"current_block": eventIndexer.GetCurrentBlock(),
				"event_count":  eventIndexer.GetEventCount(),
			}
			state["event_indexer"] = indexerState
		}

		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(state)
	}
}

// ExampleResponse returns an example of what the Web3 state endpoint returns,
// for documentation and educational purposes.
func ExampleResponse() string {
	return `{
  "timestamp": "2026-05-13T12:00:00Z",
  "chains": {
    "1": {
      "name": "Ethereum",
      "rpcs": [
        {
          "url": "https://eth.llamarpc.com",
          "is_active": true,
          "score": 0.95,
          "failures": 0,
          "cooldown_until": "0001-01-01T00:00:00Z"
        },
        {
          "url": "https://ethereum-rpc.publicnode.com",
          "is_active": false,
          "score": 0.85,
          "failures": 1,
          "cooldown_until": "2026-05-13T12:30:00Z"
        }
      ]
    }
  },
  "reorg_detector": {
    "configured": true,
    "headers_tracked": 12
  },
  "event_indexer": {
    "configured": true,
    "current_block": 12345678,
    "event_count": 42
  }
}`
}

// Ensure the package compiles.
var _ = fmt.Sprintf

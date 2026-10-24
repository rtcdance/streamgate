package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"
)

func TestDemoNFTMinterBalanceRequiresABIData(t *testing.T) {
	for _, tc := range []struct {
		name, result, balance string
		wantError             bool
	}{
		{"empty EOA response", "0x", "", true},
		{"truncated ABI response", "0x00", "", true},
		{"encoded zero is valid", "0x" + strings.Repeat("0", 64), "0", false},
		{"encoded holder balance", "0x" + strings.Repeat("0", 63) + "7", "7", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Method != "eth_call" {
					http.Error(w, "expected eth_call", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": tc.result})
			}))
			defer server.Close()
			client, err := ethclient.Dial(server.URL)
			require.NoError(t, err)
			defer client.Close()
			parsed, err := abi.JSON(strings.NewReader(demoNFTSimpleABI))
			require.NoError(t, err)
			minter := &DemoNFTMinter{client: client, parsedABI: parsed, contractAddr: common.HexToAddress("0xdead")}
			balance, err := minter.BalanceOf(context.Background(), common.HexToAddress("0x1234"))
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, balance)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.balance, balance.String())
			}
		})
	}
}

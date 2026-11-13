package auth

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProfileWalletVerifier_ProbeWiringAndBudgets(t *testing.T) {
	var calls atomic.Int64
	var slow atomic.Bool
	var gas atomic.Value
	release := make(chan struct{})
	header := &types.Header{Number: big.NewInt(17), Difficulty: big.NewInt(0), GasLimit: 30000000, Extra: []byte{}}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			ID     any               `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad RPC", 400)
			return
		}
		if slow.Load() {
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		var result any
		switch request.Method {
		case "eth_chainId":
			result = "0x7a69"
		case "eth_getBlockByNumber":
			result = header
		case "eth_getCode":
			result = "0x6000"
		case "eth_call":
			var call map[string]any
			if len(request.Params) == 0 || json.Unmarshal(request.Params[0], &call) != nil {
				http.Error(w, "bad call", 400)
				return
			}
			gas.Store(call["gas"])
			result = "0x1626ba7e" + strings.Repeat("00", 28)
		default:
			http.Error(w, "unsupported RPC", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			return
		}
	}))
	defer rpc.Close()
	defer close(release)
	cfg := config.DefaultConfig()
	cfg.Web3.ChainID = 31337
	cfg.Web3.Chains = []config.ChainConfigEntry{{ID: 31337, RPCs: []string{rpc.URL, rpc.URL + "/fallback"}}}
	cfg.Auth.ContractWallet.MaxSignatureBytes = 2
	cfg.Auth.ContractWallet.MaxBodyBytes = 512
	cfg.Auth.ContractWallet.CallGasLimit = 100000
	server, err := NewAuthServer(cfg, zap.NewNop(), nil)
	require.NoError(t, err)
	handler := NewAuthHandler(server.verifier, zap.NewNop(), nil)
	body := `{"address":"0x1234567890123456789012345678901234567890","message":"test","signature":"0x0102"}`
	w := httptest.NewRecorder()
	handler.VerifySignatureHandler(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-signature", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"valid":true`)
	require.NotContains(t, w.Body.String(), "token")
	require.Equal(t, "0x186a0", gas.Load())
	before := calls.Load()
	large := strings.Replace(body, `"test"`, `"`+strings.Repeat("x", 600)+`"`, 1)
	w = httptest.NewRecorder()
	handler.VerifySignatureHandler(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-signature", strings.NewReader(large)))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, before, calls.Load())
	probe, err := NewProfileSignatureVerifier(cfg, zap.NewNop())
	require.NoError(t, err)
	_, err = probe.VerifySignature(t.Context(), "bad", "test", "0x01")
	require.ErrorIs(t, err, signature.ErrWalletFormat)
	require.Equal(t, before, calls.Load())
	cfg.Web3.Chains[0].RPCs = []string{rpc.URL, rpc.URL}
	probe, err = NewProfileSignatureVerifier(cfg, zap.NewNop())
	require.NoError(t, err)
	_, err = probe.VerifySignature(t.Context(), "0x1234567890123456789012345678901234567890", "test", "0x01")
	require.ErrorIs(t, err, signature.ErrWalletDependency)
	require.Equal(t, before, calls.Load())
	cfg.Web3.Chains[0].RPCs = []string{rpc.URL, rpc.URL + "/fallback"}
	cfg.Auth.ContractWallet.VerificationTimeout = 20 * time.Millisecond
	slow.Store(true)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	probe, err = NewProfileSignatureVerifier(cfg, zap.NewNop())
	require.NoError(t, err)
	_, err = probe.VerifySignature(ctx, "0x1234567890123456789012345678901234567890", "test", "0x01")
	require.ErrorIs(t, err, signature.ErrWalletDependency)
	require.NoError(t, ctx.Err(), "the adapter must honor its own total budget")
}

package tx

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewContractWriter(t *testing.T) {
	cfg := ContractWriterConfig{
		Client:      nil,
		Key:         nil,
		NonceMgr:    nil,
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	}
	cw := NewContractWriter(cfg)
	assert.NotNil(t, cw)
	assert.Equal(t, common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"), cw.fromAddress)
	assert.Equal(t, big.NewInt(1), cw.chainID)
}

func TestContractWriter_WithTracker(t *testing.T) {
	cw := NewContractWriter(ContractWriterConfig{
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	tracker := &TxTracker{}
	result := cw.WithTracker(tracker)
	assert.Equal(t, cw, result)
	assert.Equal(t, tracker, cw.tracker)
}

func TestContractWriter_SendTx_NilABI(t *testing.T) {
	cw := NewContractWriter(ContractWriterConfig{
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	assert.Panics(t, func() {
		_, _ = cw.SendTx(context.Background(), ContractTxOpts{
			To:        "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
			Method:    "transfer",
			ParsedABI: nil,
		})
	})
}

func TestContractWriter_SendTx_NilNonceMgr(t *testing.T) {
	parsedABI := getTestABI(t)

	cw := NewContractWriter(ContractWriterConfig{
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	_, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		Method:    "registerContent",
		ParsedABI: parsedABI,
	})
	assert.Error(t, err)
}

func getTestABI(t *testing.T) *abi.ABI {
	t.Helper()
	json := `[
		{
			"inputs": [
				{"name": "contentHash", "type": "bytes32"},
				{"name": "metadata", "type": "string"}
			],
			"name": "registerContent",
			"outputs": [],
			"type": "function"
		}
	]`
	parsed, err := abi.JSON(bytes.NewReader([]byte(json)))
	require.NoError(t, err)
	return &parsed
}

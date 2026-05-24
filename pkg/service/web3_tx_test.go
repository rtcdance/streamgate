package service

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestWeb3Service_VerifyMerkleWhitelist_ValidProof(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}

	items := [][]byte{
		common.HexToAddress("0x0000000000000000000000000000000000000001").Bytes(),
		common.HexToAddress("0x0000000000000000000000000000000000000002").Bytes(),
		common.HexToAddress("0x0000000000000000000000000000000000000003").Bytes(),
	}
	tree, err := web3.NewMerkleTree(items)
	require.NoError(t, err)

	rootHex := tree.RootHex()
	proof, err := tree.Proof(0)
	require.NoError(t, err)

	proofHex := make([]string, len(proof))
	for i, p := range proof {
		proofHex[i] = "0x" + hex.EncodeToString(p[:])
	}

	valid, err := svc.VerifyMerkleWhitelist(rootHex, "0x0000000000000000000000000000000000000001", proofHex)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestWeb3Service_VerifyMerkleWhitelist_WrongAddressInTree(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}

	items := [][]byte{
		common.HexToAddress("0x0000000000000000000000000000000000000001").Bytes(),
		common.HexToAddress("0x0000000000000000000000000000000000000002").Bytes(),
	}
	tree, err := web3.NewMerkleTree(items)
	require.NoError(t, err)

	rootHex := tree.RootHex()

	valid, err := svc.VerifyMerkleWhitelist(rootHex, "0x0000000000000000000000000000000000000003", nil)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestWeb3Service_VerifyMerkleWhitelist_EmptyProof_SingleLeaf(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}

	items := [][]byte{common.HexToAddress("0x0000000000000000000000000000000000000001").Bytes()}
	tree, err := web3.NewMerkleTree(items)
	require.NoError(t, err)

	rootHex := tree.RootHex()

	valid, err := svc.VerifyMerkleWhitelist(rootHex, "0x0000000000000000000000000000000000000001", nil)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestWeb3Service_VerifyMerkleWhitelist_WrongAddress(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}

	items := [][]byte{
		common.HexToAddress("0x0000000000000000000000000000000000000001").Bytes(),
		common.HexToAddress("0x0000000000000000000000000000000000000002").Bytes(),
	}
	tree, err := web3.NewMerkleTree(items)
	require.NoError(t, err)

	rootHex := tree.RootHex()
	proof, err := tree.Proof(0)
	require.NoError(t, err)

	proofHex := make([]string, len(proof))
	for i, p := range proof {
		proofHex[i] = "0x" + hex.EncodeToString(p[:])
	}

	valid, err := svc.VerifyMerkleWhitelist(rootHex, "0x0000000000000000000000000000000000000003", proofHex)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestWeb3Service_VerifyMerkleWhitelist_RootWithoutPrefix(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}

	items := [][]byte{common.HexToAddress("0x0000000000000000000000000000000000000001").Bytes()}
	tree, err := web3.NewMerkleTree(items)
	require.NoError(t, err)

	rootBytes := tree.Root()
	rootHexNoPrefix := hex.EncodeToString(rootBytes[:])

	valid, err := svc.VerifyMerkleWhitelist(rootHexNoPrefix, "0x0000000000000000000000000000000000000001", nil)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestWeb3Service_RegisterContent_NoKey(t *testing.T) {
	svc := &Web3Service{
		logger: zap.NewNop(),
		config: &config.Config{},
	}
	_, err := svc.RegisterContent(context.Background(), 1, "0xcontract", "hash", "uri")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_SubmitPermit_NoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.SubmitPermit(context.Background(), 99999, "0xcontract", "0xowner", "0xspender", nil, nil, 0, [32]byte{}, [32]byte{})
	assert.Error(t, err)
}

func TestWeb3Service_GetTokenBalance_NoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetTokenBalance(context.Background(), 99999, "0xcontract", "0xaccount")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chain client not found")
}

func TestWeb3Service_GetTokenAllowance_NoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetTokenAllowance(context.Background(), 99999, "0xcontract", "0xowner", "0xspender")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chain client not found")
}

func TestWeb3Service_GetTokenInfo_NoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetTokenInfo(context.Background(), 99999, "0xcontract")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chain client not found")
}

func TestWeb3Service_WaitForReceipt_NoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.WaitForReceipt(context.Background(), 99999, "0xhash", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chain client not found")
}

func TestWeb3Service_ReplaceStuckTransaction_NoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.ReplaceStuckTransaction(context.Background(), 99999, nil, 10)
	assert.Error(t, err)
}

func TestWeb3Service_CancelPendingTransaction_NoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.CancelPendingTransaction(context.Background(), 99999, nil, 10)
	assert.Error(t, err)
}

package contract

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestContentRegistryABI_Valid(t *testing.T) {
	assert.NotEmpty(t, ContentRegistryABI)
	assert.Contains(t, ContentRegistryABI, "registerContent")
	assert.Contains(t, ContentRegistryABI, "verifyContent")
	assert.Contains(t, ContentRegistryABI, "getContentInfo")
}

func TestERC721ABI_Valid(t *testing.T) {
	assert.NotEmpty(t, ERC721ABI)
	assert.Contains(t, ERC721ABI, "mint")
	assert.Contains(t, ERC721ABI, "ownerOf")
	assert.Contains(t, ERC721ABI, "balanceOf")
}

func TestContentRegistry_Events(t *testing.T) {
	cr := NewContentRegistry("0x1234567890123456789012345678901234567890")

	expectedContentRegistered := crypto.Keccak256Hash([]byte("ContentRegistered(bytes32,address,uint256)")).Hex()
	expectedContentVerified := crypto.Keccak256Hash([]byte("ContentVerified(bytes32,bool)")).Hex()
	expectedContentDeleted := crypto.Keccak256Hash([]byte("ContentDeleted(bytes32,address)")).Hex()

	assert.Equal(t, expectedContentRegistered, cr.Events["ContentRegistered"])
	assert.Equal(t, expectedContentVerified, cr.Events["ContentVerified"])
	assert.Equal(t, expectedContentDeleted, cr.Events["ContentDeleted"])
}

func TestNFTContract_Events(t *testing.T) {
	nc := NewNFTContract("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")

	expectedTransfer := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()
	expectedApproval := crypto.Keccak256Hash([]byte("Approval(address,address,uint256)")).Hex()

	assert.Equal(t, expectedTransfer, nc.Events["Transfer"])
	assert.Equal(t, expectedApproval, nc.Events["Approval"])
}

func TestSmartContractRegistry_ConcurrentAccess(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(idx int) {
			scr.RegisterContract(&SmartContractInfo{
				Name:    fmt.Sprintf("Contract%d", idx),
				Address: fmt.Sprintf("0x%d", idx),
				ChainID: int64(idx),
			})
			scr.GetContract(fmt.Sprintf("Contract%d", idx))
			scr.GetContractByAddress(fmt.Sprintf("0x%d", idx))
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	all := scr.GetAllContracts()
	assert.Len(t, all, 10)
}

func TestSmartContractInfo_Fields(t *testing.T) {
	info := &SmartContractInfo{
		Name:       "TestContract",
		Address:    "0x1234",
		ChainID:    1,
		ABI:        "[]",
		Bytecode:   "0x",
		DeployedAt: 1234567890,
		Verified:   true,
		SourceCode: "pragma solidity ^0.8.0;",
	}
	assert.Equal(t, "TestContract", info.Name)
	assert.True(t, info.Verified)
	assert.Equal(t, int64(1), info.ChainID)
}

func TestContentRegistryBytecode(t *testing.T) {
	assert.Equal(t, "0x", ContentRegistryBytecode)
}

func TestBalanceOfABIJSON(t *testing.T) {
	assert.NotEmpty(t, BalanceOfABIJSON)
	assert.Contains(t, BalanceOfABIJSON, "balanceOf")
}

func TestSmartContractRegistry_GetContractsByChain_Empty(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	result := scr.GetContractsByChain(1)
	assert.Empty(t, result)
}

func TestSmartContractRegistry_GetAllContracts_Empty(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	all := scr.GetAllContracts()
	assert.Empty(t, all)
}

func TestSmartContractRegistry_RegisterMultipleSameChain(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	scr.RegisterContract(&SmartContractInfo{Name: "A", Address: "0x1", ChainID: 1})
	scr.RegisterContract(&SmartContractInfo{Name: "B", Address: "0x2", ChainID: 1})
	scr.RegisterContract(&SmartContractInfo{Name: "C", Address: "0x3", ChainID: 1})

	chain1 := scr.GetContractsByChain(1)
	assert.Len(t, chain1, 3)
}

package contract

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSmartContractRegistry_New(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	require.NotNil(t, scr)
	assert.NotNil(t, scr.contracts)
	assert.NotNil(t, scr.byAddr)
}

func TestSmartContractRegistry_RegisterAndGet(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	info := &SmartContractInfo{
		Name:    "ContentRegistry",
		Address: "0x1234567890123456789012345678901234567890",
		ChainID: 1,
		ABI:     ContentRegistryABI,
	}
	scr.RegisterContract(info)
	got := scr.GetContract("ContentRegistry")
	require.NotNil(t, got)
	assert.Equal(t, "ContentRegistry", got.Name)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", got.Address)
	assert.Equal(t, int64(1), got.ChainID)
}

func TestSmartContractRegistry_GetContract_NotFound(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	got := scr.GetContract("NonExistent")
	assert.Nil(t, got)
}

func TestSmartContractRegistry_GetContractByAddress(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	info := &SmartContractInfo{
		Name:    "NFT",
		Address: "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		ChainID: 137,
	}
	scr.RegisterContract(info)
	got := scr.GetContractByAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	require.NotNil(t, got)
	assert.Equal(t, "NFT", got.Name)
}

func TestSmartContractRegistry_GetContractByAddress_NotFound(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	got := scr.GetContractByAddress("0x0000000000000000000000000000000000000000")
	assert.Nil(t, got)
}

func TestSmartContractRegistry_GetAllContracts(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	scr.RegisterContract(&SmartContractInfo{Name: "A", Address: "0x1", ChainID: 1})
	scr.RegisterContract(&SmartContractInfo{Name: "B", Address: "0x2", ChainID: 137})
	all := scr.GetAllContracts()
	assert.Len(t, all, 2)
	assert.Contains(t, all, "A")
	assert.Contains(t, all, "B")
}

func TestSmartContractRegistry_GetContractsByChain(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	scr.RegisterContract(&SmartContractInfo{Name: "A", Address: "0x1", ChainID: 1})
	scr.RegisterContract(&SmartContractInfo{Name: "B", Address: "0x2", ChainID: 137})
	scr.RegisterContract(&SmartContractInfo{Name: "C", Address: "0x3", ChainID: 1})
	chain1 := scr.GetContractsByChain(1)
	assert.Len(t, chain1, 2)
	chain137 := scr.GetContractsByChain(137)
	assert.Len(t, chain137, 1)
	assert.Equal(t, "B", chain137[0].Name)
	chain999 := scr.GetContractsByChain(999)
	assert.Empty(t, chain999)
}

func TestSmartContractRegistry_RegisterOverwrite(t *testing.T) {
	scr := NewSmartContractRegistry(zap.NewNop())
	scr.RegisterContract(&SmartContractInfo{Name: "A", Address: "0x1", ChainID: 1})
	scr.RegisterContract(&SmartContractInfo{Name: "A", Address: "0x2", ChainID: 137})
	got := scr.GetContract("A")
	require.NotNil(t, got)
	assert.Equal(t, "0x2", got.Address)
	assert.Equal(t, int64(137), got.ChainID)
}

func TestNewContentRegistry(t *testing.T) {
	cr := NewContentRegistry("0x1234567890123456789012345678901234567890")
	assert.Equal(t, "0x1234567890123456789012345678901234567890", cr.Address)
	assert.Equal(t, ContentRegistryABI, cr.ABI)
	assert.NotNil(t, cr.Events)
	assert.Contains(t, cr.Events, "ContentRegistered")
	assert.Contains(t, cr.Events, "ContentVerified")
	assert.Contains(t, cr.Events, "ContentDeleted")
}

func TestNewNFTContract(t *testing.T) {
	nc := NewNFTContract("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	assert.Equal(t, "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd", nc.Address)
	assert.Equal(t, ERC721ABI, nc.ABI)
	assert.NotNil(t, nc.Events)
	assert.Contains(t, nc.Events, "Transfer")
	assert.Contains(t, nc.Events, "Approval")
}

func TestDecodeCustomError_NoABI(t *testing.T) {
	_, _, ok := DecodeCustomError([]byte{0xab, 0xcd, 0xef, 0x01, 0x00})
	assert.False(t, ok)
}

func TestDecodeCustomError_TooShort(t *testing.T) {
	_, _, ok := DecodeCustomError([]byte{0x01, 0x02})
	assert.False(t, ok)
}

func TestDecodeCustomError_WithABI(t *testing.T) {
	errorABI := `[{"name":"InsufficientBalance","inputs":[{"name":"available","type":"uint256"},{"name":"required","type":"uint256"}],"type":"error"}]`
	parsed, err := abi.JSON(strings.NewReader(errorABI))
	require.NoError(t, err)
	errDef := parsed.Errors["InsufficientBalance"]
	require.NotNil(t, errDef)
	selector := errDef.ID[:4]
	available := make([]byte, 32)
	available[31] = 100
	required := make([]byte, 32)
	required[31] = 200
	data := append(selector, available...)
	data = append(data, required...)
	name, args, ok := DecodeCustomError(data, parsed)
	assert.True(t, ok)
	assert.Equal(t, "InsufficientBalance", name)
	assert.NotNil(t, args)
	assert.Contains(t, args, "available")
	assert.Contains(t, args, "required")
}

func TestDecodeCustomError_NoMatchingError(t *testing.T) {
	otherABI := `[{"name":"SomeOtherError","inputs":[{"name":"code","type":"uint256"}],"type":"error"}]`
	parsed, err := abi.JSON(strings.NewReader(otherABI))
	require.NoError(t, err)
	data := make([]byte, 36)
	data[0] = 0xff
	data[1] = 0xff
	data[2] = 0xff
	data[3] = 0xff
	_, _, ok := DecodeCustomError(data, parsed)
	assert.False(t, ok)
}

func TestRevertError_IsRetryable(t *testing.T) {
	revert := &RevertError{Reason: "test", RawData: []byte{0x01}}
	assert.False(t, revert.IsRetryable())
}

func TestExtractRevertData_With0XPrefix(t *testing.T) {
	payload := append([]byte{0x08, 0xc3, 0x79, 0xa0}, encodeABIString("not owner")...)
	hexStr := "0x" + hex.EncodeToString(payload)
	data := ExtractRevertData("error: " + hexStr)
	if data != nil {
		revert := ParseRevertReason(data)
		if revert != nil {
			assert.Equal(t, "not owner", revert.Reason)
		}
	}
}

func TestPanicCodeName_Unknown(t *testing.T) {
	name := panicCodeName(0x99)
	assert.Contains(t, name, "unknown panic code")
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{"big_int", big.NewInt(42), "42"},
		{"address", common.HexToAddress("0x1"), "0x0000000000000000000000000000000000000001"},
		{"string", "hello", "hello"},
		{"bool_true", true, true},
		{"bool_false", false, false},
		{"bytes", []byte{0xab, 0xcd}, "0xabcd"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatValue(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFormatValue_Addresses(t *testing.T) {
	addrs := []common.Address{
		common.HexToAddress("0x1"),
		common.HexToAddress("0x2"),
	}
	result := formatValue(addrs)
	list, ok := result.([]string)
	require.True(t, ok)
	assert.Len(t, list, 2)
}

func TestFormatValue_Default(t *testing.T) {
	result := formatValue(int64(42))
	assert.Equal(t, "42", result)
}

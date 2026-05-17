//nolint:unused
package web3_test

import (
	"context"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// mockEthCaller implements web3.EthCaller for testing
type mockEthCaller struct {
	mu      sync.Mutex
	calls   map[string][][]byte // method selector → list of responses
	callErr map[string]error    // method selector → error to return
	callIdx map[string]int      // method selector → call counter
}

func newMockEthCaller() *mockEthCaller {
	return &mockEthCaller{
		calls:   make(map[string][][]byte),
		callErr: make(map[string]error),
		callIdx: make(map[string]int),
	}
}

func (m *mockEthCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if call.To == nil {
		return nil, nil
	}

	selector := common.Bytes2Hex(call.Data[:4])
	if err, ok := m.callErr[selector]; ok {
		return nil, err
	}

	responses, ok := m.calls[selector]
	if !ok {
		return nil, nil
	}

	idx := 0
	if i, ok := m.callIdx[selector]; ok {
		idx = i
	}
	m.callIdx[selector] = idx + 1

	if idx >= len(responses) {
		return nil, nil
	}
	return responses[idx], nil
}

func (m *mockEthCaller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	return nil, nil
}

// setResponse sets a response for a method selector
func (m *mockEthCaller) setResponse(selector string, responses ...[]byte) {
	m.calls[selector] = responses
}

// setError sets an error for a method selector
func (m *mockEthCaller) setError(selector string, err error) {
	m.callErr[selector] = err
}

// packOwnerOfResponse packs an address into an ownerOf response (32 bytes left-padded)
func packOwnerOfResponse(addr common.Address) []byte {
	result := make([]byte, 32)
	copy(result[12:], addr[:])
	return result
}

// packBalanceOfResponse packs a uint256 balance response
func packBalanceOfResponse(balance int64) []byte {
	result := make([]byte, 32)
	new(big.Int).SetInt64(balance).FillBytes(result)
	return result
}

// packStringResponse packs a string response (ABI-encoded)
func packStringResponse(s string) []byte {
	data := []byte(s)
	offset := make([]byte, 32)
	offset[31] = 32 // offset to data

	length := make([]byte, 32)
	new(big.Int).SetInt64(int64(len(data))).FillBytes(length)

	paddedData := make([]byte, ((len(data)+31)/32)*32)
	copy(paddedData, data)

	result := make([]byte, 0, 32+32+int64(len(paddedData)))
	result = append(result, offset...)
	result = append(result, length...)
	result = append(result, paddedData...)
	return result
}

// knownOwnerAddress is a pre-defined test address
var knownOwnerAddress = common.HexToAddress("0x1234567890123456789012345678901234567890")

// setupNFTMock creates a mock caller with standard NFT responses
func setupNFTMock() *mockEthCaller {
	mock := newMockEthCaller()

	// ownerOf(uint256) → knownOwnerAddress
	selectorOwnerOf := "6352211e"
	mock.setResponse(selectorOwnerOf, packOwnerOfResponse(knownOwnerAddress))

	// balanceOf(address) → 3
	selectorBalanceOf := "70a08231"
	mock.setResponse(selectorBalanceOf, packBalanceOfResponse(3))

	// name() → "TestNFT"
	selectorName := "06fdde03"
	mock.setResponse(selectorName, packStringResponse("TestNFT"))

	// symbol() → "TNFT"
	selectorSymbol := "95d89b41"
	mock.setResponse(selectorSymbol, packStringResponse("TNFT"))

	// tokenURI(uint256) → "https://example.com/metadata/1"
	selectorTokenURI := "c87b56dd"
	mock.setResponse(selectorTokenURI, packStringResponse("https://example.com/metadata/1"))

	return mock
}

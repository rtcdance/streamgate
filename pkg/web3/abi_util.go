package web3

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/rtcdance/streamgate/pkg/web3/internal/abiutil"
)

func mustParseABI(name, jsonStr string) abi.ABI {
	return abiutil.MustParseABI(name, jsonStr)
}

func getOrParseABI(abiJSON string) (abi.ABI, error) {
	return abiutil.GetOrParseABI(abiJSON)
}

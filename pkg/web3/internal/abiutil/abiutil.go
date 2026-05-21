package abiutil

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

func MustParseABI(name, jsonStr string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(jsonStr))
	if err != nil {
		panic(fmt.Sprintf("mustParseABI(%s): %v", name, err))
	}
	return parsed
}

var abiCache sync.Map

func GetOrParseABI(abiJSON string) (abi.ABI, error) {
	if cached, ok := abiCache.Load(abiJSON); ok {
		return cached.(abi.ABI), nil
	}
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return abi.ABI{}, err
	}
	abiCache.Store(abiJSON, parsed)
	return parsed, nil
}

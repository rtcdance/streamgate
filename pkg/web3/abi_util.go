package web3

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// mustParseABI parses a hardcoded ABI JSON string at init time.
// It panics only if the constant is malformed, which is a programmer
// error caught at startup — far preferable to panicking inside a
// constructor that could be called at any point during runtime.
func mustParseABI(name, jsonStr string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(jsonStr))
	if err != nil {
		// This can only happen if the ABI constant is malformed,
		// which is a compile-time programmer error. Panic at init
		// is acceptable for this case.
		panic(fmt.Sprintf("mustParseABI(%s): %v", name, err))
	}
	return parsed
}

package web3

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// RevertError represents a decoded contract revert reason.
type RevertError struct {
	Reason    string // decoded human-readable reason
	RawData   []byte // raw ABI-encoded revert data
	IsPanic   bool   // true for Panic(uint256), false for Error(string)
	PanicCode uint64 // panic code if IsPanic
}

func (e *RevertError) Error() string {
	if e.IsPanic {
		return fmt.Sprintf("contract panic(0x%x): %s", e.PanicCode, panicCodeName(e.PanicCode))
	}
	return fmt.Sprintf("contract revert: %s", e.Reason)
}

// ParseRevertReason attempts to decode an ABI-encoded revert reason from
// the data returned by a failed contract call or transaction receipt.
//
// It handles two standard patterns:
//   - Error(string): selector 0x08c379a0
//   - Panic(uint256): selector 0x4e487b71
//
// Returns a RevertError with the decoded reason, or nil if the data
// doesn't match either pattern.
func ParseRevertReason(data []byte) *RevertError {
	if len(data) < 4 {
		return nil
	}

	selector := common.Bytes2Hex(data[:4])

	switch selector {
	case "08c379a0": // Error(string)
		reason, err := decodeString(data[4:])
		if err != nil {
			return &RevertError{Reason: hex.EncodeToString(data), RawData: data}
		}
		return &RevertError{Reason: reason, RawData: data}

	//nolint:gocritic // Solidity error selector
	case "4e487b71": // Panic(uint256)
		code, err := decodeUint256(data[4:])
		if err != nil {
			return &RevertError{Reason: hex.EncodeToString(data), RawData: data, IsPanic: true}
		}
		return &RevertError{
			RawData:   data,
			IsPanic:   true,
			PanicCode: code,
		}
	}

	return nil
}

// DecodeCustomError attempts to match the error data selector against known
// ABI custom error definitions. Returns the error name and decoded args if
// a match is found.
func DecodeCustomError(data []byte, abis ...abi.ABI) (name string, args map[string]interface{}, ok bool) {
	if len(data) < 4 {
		return "", nil, false
	}

	selector := data[:4]
	for _, parsedABI := range abis {
		for _, errDef := range parsedABI.Errors {
			if len(errDef.ID) == 32 {
				// Compare first 4 bytes of the error ID (selector)
				errSelector := errDef.ID[:4]
				if common.Bytes2Hex(errSelector) == common.Bytes2Hex(selector) {
					args := make(map[string]interface{})
					if len(data) > 4 {
						vals, unpackErr := errDef.Inputs.Unpack(data[4:])
						if unpackErr == nil {
							idx := 0
							for _, input := range errDef.Inputs {
								if idx < len(vals) {
									args[input.Name] = formatValue(vals[idx])
									idx++
								}
							}
						}
					}
					return errDef.Name, args, true
				}
			}
		}
	}
	return "", nil, false
}

// ExtractRevertData extracts the revert data from an error message.
// go-ethereum wraps revert data in errors like:
//
//	"execution reverted: 0x..."
//
// or
//
//	"0x08c379a000000000000000000000000000000000000000000000000000000000000000200..."
func ExtractRevertData(errMsg string) []byte {
	// Try to find hex data in the error message
	for _, prefix := range []string{"0x", "0X"} {
		idx := strings.Index(errMsg, prefix)
		if idx >= 0 {
			hexStr := errMsg[idx+2:]
			// Remove any trailing non-hex chars
			end := len(hexStr)
			for i, c := range hexStr {
				if !isHexChar(c) {
					end = i
					break
				}
			}
			if data, err := hex.DecodeString(hexStr[:end]); err == nil && len(data) >= 4 {
				return data
			}
		}
	}
	return nil
}

// decodeString decodes an ABI-encoded string (offset + length + utf8 bytes).
func decodeString(data []byte) (string, error) {
	if len(data) < 64 {
		return "", fmt.Errorf("insufficient data for string offset")
	}

	// Read offset (first 32 bytes)
	offset := new(bigIntFromBytes)
	offset.SetBytes(data[:32])
	if offset.Uint64() > uint64(len(data)) {
		return "", fmt.Errorf("string offset out of range")
	}

	// Read length at offset
	start := offset.Uint64()
	if start+32 > uint64(len(data)) {
		return "", fmt.Errorf("insufficient data for string length")
	}
	length := new(bigIntFromBytes)
	length.SetBytes(data[start : start+32])

	if start+32+length.Uint64() > uint64(len(data)) {
		return "", fmt.Errorf("string data truncated")
	}

	return string(data[start+32 : start+32+length.Uint64()]), nil
}

// decodeUint256 decodes an ABI-encoded uint256.
func decodeUint256(data []byte) (uint64, error) {
	if len(data) < 32 {
		return 0, fmt.Errorf("insufficient data for uint256")
	}
	val := new(bigIntFromBytes)
	val.SetBytes(data[:32])
	return val.Uint64(), nil
}

// bigIntFromBytes is a helper type for decoding ABI integers.
type bigIntFromBytes struct {
	value []byte
}

func (b *bigIntFromBytes) SetBytes(data []byte) {
	b.value = make([]byte, len(data))
	copy(b.value, data)
}

func (b *bigIntFromBytes) Uint64() uint64 {
	// Trim leading zeros and convert
	var result uint64
	for _, byt := range b.value {
		result = result<<8 | uint64(byt)
	}
	return result
}

// panicCodeName returns a human-readable name for Solidity panic codes.
func panicCodeName(code uint64) string {
	switch code {
	case 0x01:
		return "assertion failed"
	case 0x11:
		return "arithmetic overflow/underflow"
	case 0x12:
		return "division or modulo by zero"
	case 0x21:
		return "enum conversion out of bounds"
	case 0x22:
		return "bad storage access"
	case 0x31:
		return "pop from empty array"
	case 0x32:
		return "array out-of-bounds access"
	case 0x41:
		return "out of memory"
	case 0x51:
		return "uninitialized function pointer"
	default:
		return fmt.Sprintf("unknown panic code 0x%x", code)
	}
}

func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

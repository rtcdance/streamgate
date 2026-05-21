package contract

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type RevertError struct {
	Reason    string
	RawData   []byte
	IsPanic   bool
	PanicCode uint64
}

func (e *RevertError) Error() string {
	if e.IsPanic {
		return fmt.Sprintf("contract panic(0x%x): %s", e.PanicCode, panicCodeName(e.PanicCode))
	}
	return fmt.Sprintf("contract revert: %s", e.Reason)
}

func (e *RevertError) IsRetryable() bool { return false }

func ParseRevertReason(data []byte) *RevertError {
	if len(data) < 4 {
		return nil
	}

	selector := common.Bytes2Hex(data[:4])

	switch selector {
	case "08c379a0":
		reason, err := decodeString(data[4:])
		if err != nil {
			return &RevertError{Reason: hex.EncodeToString(data), RawData: data}
		}
		return &RevertError{Reason: reason, RawData: data}

	case "4e487b71":
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

func DecodeCustomError(data []byte, abis ...abi.ABI) (name string, args map[string]interface{}, ok bool) {
	if len(data) < 4 {
		return "", nil, false
	}

	selector := data[:4]
	for _, parsedABI := range abis {
		for _, errDef := range parsedABI.Errors {
			if len(errDef.ID) == 32 {
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

func ExtractRevertData(errMsg string) []byte {
	for _, prefix := range []string{"0x", "0X"} {
		idx := strings.Index(errMsg, prefix)
		if idx >= 0 {
			hexStr := errMsg[idx+2:]
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

func decodeString(data []byte) (string, error) {
	if len(data) < 64 {
		return "", fmt.Errorf("insufficient data for string offset")
	}

	offset := new(bigIntFromBytes)
	offset.SetBytes(data[:32])
	if offset.Uint64() > uint64(len(data)) {
		return "", fmt.Errorf("string offset out of range")
	}

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

func decodeUint256(data []byte) (uint64, error) {
	if len(data) < 32 {
		return 0, fmt.Errorf("insufficient data for uint256")
	}
	val := new(bigIntFromBytes)
	val.SetBytes(data[:32])
	return val.Uint64(), nil
}

type bigIntFromBytes struct {
	value []byte
}

func (b *bigIntFromBytes) SetBytes(data []byte) {
	b.value = make([]byte, len(data))
	copy(b.value, data)
}

func (b *bigIntFromBytes) Uint64() uint64 {
	v := new(big.Int).SetBytes(b.value)
	if !v.IsUint64() {
		return 0
	}
	return v.Uint64()
}

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

func formatValue(v interface{}) interface{} {
	switch val := v.(type) {
	case *big.Int:
		return val.String()
	case common.Address:
		return val.Hex()
	case []common.Address:
		addrs := make([]string, len(val))
		for i, a := range val {
			addrs[i] = a.Hex()
		}
		return addrs
	case string:
		return val
	case bool:
		return val
	case []byte:
		return fmt.Sprintf("0x%x", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

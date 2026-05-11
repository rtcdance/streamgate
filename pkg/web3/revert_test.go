package web3

import (
	"encoding/hex"
	"testing"
)

func TestParseRevertReason_ErrorString(t *testing.T) {
	// Error(string) = 0x08c379a0 + ABI-encoded "insufficient balance"
	// ABI encoding: offset(32) + length(32) + utf8 bytes(padded to 32)
	reason := "insufficient balance"
	encoded := encodeABIString(reason)
	data := append([]byte{0x08, 0xc3, 0x79, 0xa0}, encoded...)

	revert := ParseRevertReason(data)
	if revert == nil {
		t.Fatal("expected non-nil RevertError")
	}
	if revert.IsPanic {
		t.Error("should not be a panic")
	}
	if revert.Reason != reason {
		t.Errorf("expected reason %q, got %q", reason, revert.Reason)
	}
}

func TestParseRevertReason_Panic(t *testing.T) {
	// Panic(uint256) = 0x4e487b71 + uint256(0x11) = arithmetic overflow
	panicCode := make([]byte, 32)
	panicCode[31] = 0x11 // overflow
	data := append([]byte{0x4e, 0x48, 0x7b, 0x71}, panicCode...)

	revert := ParseRevertReason(data)
	if revert == nil {
		t.Fatal("expected non-nil RevertError")
	}
	if !revert.IsPanic {
		t.Error("should be a panic")
	}
	if revert.PanicCode != 0x11 {
		t.Errorf("expected panic code 0x11, got 0x%x", revert.PanicCode)
	}
	if revert.Error() != "contract panic(0x11): arithmetic overflow/underflow" {
		t.Errorf("unexpected error message: %s", revert.Error())
	}
}

func TestParseRevertReason_PanicCodes(t *testing.T) {
	codes := map[uint64]string{
		0x01: "assertion failed",
		0x12: "division or modulo by zero",
		0x21: "enum conversion out of bounds",
		0x31: "pop from empty array",
		0x32: "array out-of-bounds access",
		0x41: "out of memory",
		0x51: "uninitialized function pointer",
	}
	for code, expected := range codes {
		name := panicCodeName(code)
		if name != expected {
			t.Errorf("panicCodeName(0x%x) = %q, want %q", code, name, expected)
		}
	}
}

func TestParseRevertReason_UnknownSelector(t *testing.T) {
	data := []byte{0xab, 0xcd, 0xef, 0x01, 0x00, 0x00, 0x00, 0x00}
	revert := ParseRevertReason(data)
	if revert != nil {
		t.Error("expected nil for unknown selector")
	}
}

func TestParseRevertReason_TooShort(t *testing.T) {
	revert := ParseRevertReason([]byte{0x08, 0xc3})
	if revert != nil {
		t.Error("expected nil for data < 4 bytes")
	}
}

func TestExtractRevertData_HexInMessage(t *testing.T) {
	// Simulate a go-ethereum error with hex revert data
	reason := "not owner"
	encoded := encodeABIString(reason)
	fullData := append([]byte{0x08, 0xc3, 0x79, 0xa0}, encoded...)
	hexMsg := "execution reverted: 0x" + hex.EncodeToString(fullData)

	data := ExtractRevertData(hexMsg)
	if data == nil {
		t.Fatal("expected non-nil data")
	}
	revert := ParseRevertReason(data)
	if revert == nil {
		t.Fatal("expected non-nil RevertError")
	}
	if revert.Reason != reason {
		t.Errorf("expected reason %q, got %q", reason, revert.Reason)
	}
}

func TestExtractRevertData_NoHex(t *testing.T) {
	data := ExtractRevertData("some random error message")
	if data != nil {
		t.Error("expected nil for message without hex data")
	}
}

func TestRevertError_ErrorString(t *testing.T) {
	revert := &RevertError{Reason: "not authorized", RawData: []byte{0x01}}
	if revert.Error() != "contract revert: not authorized" {
		t.Errorf("unexpected error string: %s", revert.Error())
	}
}

func TestRevertError_PanicString(t *testing.T) {
	revert := &RevertError{IsPanic: true, PanicCode: 0x12, RawData: []byte{0x01}}
	errStr := revert.Error()
	if errStr != "contract panic(0x12): division or modulo by zero" {
		t.Errorf("unexpected panic string: %s", errStr)
	}
}

// encodeABIString encodes a string in ABI format (offset + length + utf8 padded to 32 bytes).
func encodeABIString(s string) []byte {
	// offset = 32 (data starts right after offset word)
	offset := make([]byte, 32)
	offset[31] = 32 // offset = 0x20

	// length
	length := make([]byte, 32)
	length[31] = byte(len(s))

	// utf8 bytes padded to 32-byte boundary
	paddedLen := ((len(s) + 31) / 32) * 32
	if paddedLen == 0 {
		paddedLen = 32
	}
	utf8 := make([]byte, paddedLen)
	copy(utf8, s)

	result := make([]byte, 0, 64+paddedLen)
	result = append(result, offset...)
	result = append(result, length...)
	result = append(result, utf8...)
	return result
}

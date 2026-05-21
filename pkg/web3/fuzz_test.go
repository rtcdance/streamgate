package web3

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func FuzzVerifySignature(f *testing.F) {
	// Seed with valid and invalid inputs
	f.Add("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "hello world", "0xabcdef1234567890")
	f.Add("", "", "")
	f.Add("not_an_address", "msg", "0x00")
	f.Add("0x0000000000000000000000000000000000000001", "test message", "0x1234")

	f.Fuzz(func(t *testing.T, address, message, signature string) {
		// Should never panic
		verifier := NewSignatureVerifier(zap.NewNop())
		_, _ = verifier.VerifySignature(context.Background(), address, message, signature)
	})
}

func FuzzValidateAddress(f *testing.F) {
	f.Add("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	f.Add("0x0000000000000000000000000000000000000000")
	f.Add("")
	f.Add("not_an_address")
	f.Add("0x1234")

	f.Fuzz(func(t *testing.T, address string) {
		// Should never panic
		wm := NewWalletManager(nil)
		_ = wm.ValidateAddress(address)
	})
}

func FuzzHashLeaf(f *testing.F) {
	f.Add([]byte("alice"))
	f.Add([]byte(""))
	f.Add([]byte{0x00})
	f.Add([]byte{0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		// HashLeaf should never panic regardless of input
		_ = HashLeaf(data)
	})
}

func FuzzParseRevertReason(f *testing.F) {
	f.Add([]byte{0x08, 0xc3, 0x79, 0xa0})
	f.Add([]byte{0x4e, 0x48, 0x7b, 0x71})
	f.Add([]byte{})
	f.Add([]byte{0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Should never panic
		_ = ParseRevertReason(data)
	})
}

func FuzzParseSIWEMessage(f *testing.F) {
	f.Add("streamgate.io wants you to sign in with your Ethereum account:\n0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18\n\nSign in to StreamGate\n\nURI: https://streamgate.io/login\nVersion: 1\nChain ID: 11155111\nNonce: abc123\nIssued At: 2024-01-01T00:00:00Z")
	f.Add("")
	f.Add("random gibberish that is not a SIWE message")
	f.Add("streamgate.io\n\n\n\nURI: invalid")

	f.Fuzz(func(t *testing.T, message string) {
		_, _ = ParseSIWEMessage(message)
	})
}

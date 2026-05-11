package auth

import (
	"context"
	"errors"
)

// SignatureHelper provides signature verification utilities.
// When a WalletSignatureVerifier is injected, it delegates to the real
// implementation. Otherwise, it returns an explicit "not configured" error.
type SignatureHelper struct {
	verifier WalletSignatureVerifier
}

// NewSignatureHelper creates a signature helper without a backend verifier.
func NewSignatureHelper() *SignatureHelper {
	return &SignatureHelper{}
}

// NewSignatureHelperWithVerifier creates a signature helper backed by a real verifier.
func NewSignatureHelperWithVerifier(verifier WalletSignatureVerifier) *SignatureHelper {
	return &SignatureHelper{verifier: verifier}
}

// Verify verifies a signature against an address and message.
// Returns an error if no verifier is configured.
func (sh *SignatureHelper) Verify(ctx context.Context, address, message, signature string) (bool, error) {
	if sh.verifier == nil {
		return false, errors.New("signature verification not configured: inject WalletSignatureVerifier via NewSignatureHelperWithVerifier")
	}
	return sh.verifier.VerifySignature(ctx, address, message, signature)
}

// RecoverAddress recovers the signer address from a message and signature.
// Returns an error if no verifier is configured.
func (sh *SignatureHelper) RecoverAddress(ctx context.Context, message, signature string) (string, error) {
	if sh.verifier == nil {
		return "", errors.New("address recovery not configured: inject WalletSignatureVerifier via NewSignatureHelperWithVerifier")
	}
	// Delegate to verification — if valid, the verifier confirms the address.
	// Direct address recovery would require exposing internal crypto logic.
	return "", errors.New("address recovery not directly supported: use Verify with expected address instead")
}

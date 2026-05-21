package service

import (
	"context"

	"github.com/rtcdance/streamgate/pkg/web3"

	"go.uber.org/zap"
)

// MultiChainSignatureVerifier routes signature verification to the correct
// algorithm based on chain type: EVM uses secp256k1/EIP-191, Solana uses ed25519.
type MultiChainSignatureVerifier struct {
	evmVerifier    *web3.SignatureVerifier
	solanaVerifier web3.SolanaSigner
}

// NewMultiChainSignatureVerifier creates a new chain-aware signature verifier.
func NewMultiChainSignatureVerifier(logger *zap.Logger, solanaVerifier web3.SolanaSigner) *MultiChainSignatureVerifier {
	return &MultiChainSignatureVerifier{
		evmVerifier:    web3.NewSignatureVerifier(logger),
		solanaVerifier: solanaVerifier,
	}
}

// VerifySignature verifies an EVM (secp256k1/EIP-191) signature.
func (v *MultiChainSignatureVerifier) VerifySignature(ctx context.Context, address, message, signature string) (bool, error) {
	return v.evmVerifier.VerifySignature(ctx, address, message, signature)
}

// VerifySolanaSignature verifies a Solana (ed25519) signature.
func (v *MultiChainSignatureVerifier) VerifySolanaSignature(ctx context.Context, address, message, signature string) (bool, error) {
	if v.solanaVerifier == nil {
		return false, ErrSolanaNotConfigured
	}
	return v.solanaVerifier.VerifySignature(address, message, signature)
}

// VerifyOffchainMessage verifies a Solana off-chain message with standard prefix.
func (v *MultiChainSignatureVerifier) VerifyOffchainMessage(ctx context.Context, address, message, signature string) (bool, error) {
	if v.solanaVerifier == nil {
		return false, ErrSolanaNotConfigured
	}
	return v.solanaVerifier.VerifyOffchainMessage(address, message, signature)
}

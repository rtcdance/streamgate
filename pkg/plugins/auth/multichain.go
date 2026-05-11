package auth

import (
	"context"
	"errors"
)

// SolanaSignatureVerifier verifies Solana ed25519 signatures.
type SolanaSignatureVerifier interface {
	VerifySolanaSignature(address, message, signature string) (bool, error)
}

// MultiChainVerifier verifies signatures on multiple chains.
// When verifiers are injected, it delegates to the real implementations.
// Otherwise, it returns explicit "not configured" errors.
type MultiChainVerifier struct {
	evmVerifier    WalletSignatureVerifier
	solanaVerifier SolanaSignatureVerifier
}

// NewMultiChainVerifier creates a multichain verifier without backend verifiers.
func NewMultiChainVerifier() *MultiChainVerifier {
	return &MultiChainVerifier{}
}

// NewMultiChainVerifierWithVerifiers creates a multichain verifier with injected backends.
func NewMultiChainVerifierWithVerifiers(evm WalletSignatureVerifier, solana SolanaSignatureVerifier) *MultiChainVerifier {
	return &MultiChainVerifier{
		evmVerifier:    evm,
		solanaVerifier: solana,
	}
}

// VerifyEVM verifies an EVM (secp256k1/EIP-191) signature.
// Returns an error if no EVM verifier is configured.
func (v *MultiChainVerifier) VerifyEVM(ctx context.Context, address, message, signature string) (bool, error) {
	if v.evmVerifier == nil {
		return false, errors.New("EVM signature verification not configured: inject WalletSignatureVerifier via NewMultiChainVerifierWithVerifiers")
	}
	return v.evmVerifier.VerifySignature(ctx, address, message, signature)
}

// VerifySolana verifies a Solana (ed25519) signature.
// Returns an error if no Solana verifier is configured.
func (v *MultiChainVerifier) VerifySolana(ctx context.Context, address, message, signature string) (bool, error) {
	if v.solanaVerifier == nil {
		return false, errors.New("Solana signature verification not configured: inject SolanaSignatureVerifier via NewMultiChainVerifierWithVerifiers")
	}
	return v.solanaVerifier.VerifySolanaSignature(address, message, signature)
}

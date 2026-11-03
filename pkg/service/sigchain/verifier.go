package sigchain

import (
	"context"

	"github.com/rtcdance/streamgate/pkg/service/serviceerrors"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/rtcdance/streamgate/pkg/web3/signature"

	"go.uber.org/zap"
)

// MultiChainSignatureVerifier routes signature verification to the correct
// algorithm based on chain type: EVM uses secp256k1/EIP-191, Solana uses ed25519.
type MultiChainSignatureVerifier struct {
	evmVerifier    *web3.SignatureVerifier
	solanaVerifier web3.SolanaSigner
	chains         web3.ChainReader
	logger         *zap.Logger
	budget         signature.VerificationBudget
}

// NewMultiChainSignatureVerifier creates a new chain-aware signature verifier.
func NewMultiChainSignatureVerifier(logger *zap.Logger, solanaVerifier web3.SolanaSigner, chains ...web3.ChainReader) *MultiChainSignatureVerifier {
	v := &MultiChainSignatureVerifier{
		evmVerifier:    web3.NewSignatureVerifier(logger),
		logger:         logger,
		solanaVerifier: solanaVerifier,
	}
	if len(chains) > 0 {
		v.chains = chains[0]
	}
	return v
}

// VerifySignature verifies an EVM (secp256k1/EIP-191) signature.
func (v *MultiChainSignatureVerifier) VerifySignature(ctx context.Context, address, message, signature string) (bool, error) {
	return v.evmVerifier.VerifySignature(ctx, address, message, signature)
}

// VerifySolanaSignature verifies a Solana (ed25519) signature.
func (v *MultiChainSignatureVerifier) VerifySolanaSignature(ctx context.Context, address, message, signature string) (bool, error) {
	if v.solanaVerifier == nil {
		return false, serviceerrors.ErrSolanaNotConfigured
	}
	return v.solanaVerifier.VerifySignature(address, message, signature)
}

// VerifyOffchainMessage verifies a Solana off-chain message with standard prefix.
func (v *MultiChainSignatureVerifier) VerifyOffchainMessage(ctx context.Context, address, message, signature string) (bool, error) {
	if v.solanaVerifier == nil {
		return false, serviceerrors.ErrSolanaNotConfigured
	}
	return v.solanaVerifier.VerifyOffchainMessage(address, message, signature)
}

// VerifyWalletProof routes one challenge to its server-configured chain without
// mutating a shared EVM checker. Session issuance must retain the returned kind.
func (v *MultiChainSignatureVerifier) VerifyWalletProof(ctx context.Context, chainID int64, scheme, address, message, encoded string) (*signature.AccountProof, error) {
	if v.chains == nil {
		return nil, signature.ErrWalletDependency
	}
	client, err := v.chains.GetClient(chainID)
	if err != nil {
		return nil, signature.ErrWalletFormat
	}
	if client == nil {
		return nil, signature.ErrWalletDependency
	}
	return signature.NewAccountVerifier(client, v.logger, v.budget).Verify(ctx, chainID, scheme, address, message, encoded)
}

// NewMultiChainSignatureVerifierWithBudget fixes budgets at construction, not per request.
func NewMultiChainSignatureVerifierWithBudget(logger *zap.Logger, solana web3.SolanaSigner, chains web3.ChainReader, budget signature.VerificationBudget) *MultiChainSignatureVerifier {
	v := NewMultiChainSignatureVerifier(logger, solana, chains)
	v.budget = budget
	return v
}

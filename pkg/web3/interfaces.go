package web3

import (
	"context"
)

// ChainManagerInterface abstracts multi-chain RPC operations.
// *MultiChainManager satisfies this interface.
//go:generate mockgen -destination=mocks/mock_chain_manager.go -package=mocks streamgate/pkg/web3 ChainManagerInterface
type ChainManagerInterface interface {
	AddChain(chainID int64) error
	GetClient(chainID int64) (*ChainClient, error)
	GetSolanaClient(chainID int64) (*SolanaVerifier, error)
	GetSupportedChains() []*ChainConfig
	GetRPCStatuses() map[int64][]RPCStatus
	GetTestnetChains() []*ChainConfig
	GetMainnetChains() []*ChainConfig
	SetRateLimiter(rl *RPCRateLimiter)
	Close()
}

// SignatureVerifierInterface abstracts signature verification.
// *SignatureVerifier satisfies this interface.
//go:generate mockgen -destination=mocks/mock_sig_verifier.go -package=mocks streamgate/pkg/web3 SignatureVerifierInterface
type SignatureVerifierInterface interface {
	VerifySignature(ctx context.Context, address, message, signature string) (bool, error)
}

// SolanaVerifierInterface abstracts Solana verification.
// *SolanaVerifier satisfies this interface.
//go:generate mockgen -destination=mocks/mock_solana_verifier.go -package=mocks streamgate/pkg/web3 SolanaVerifierInterface
type SolanaVerifierInterface interface {
	VerifyTokenAccount(ctx context.Context, tokenAccount, ownerAddress string) (bool, error)
	VerifyMintAuthority(ctx context.Context, mintAddress, authorityAddress string) (bool, error)
	VerifyMetaplexMetadata(ctx context.Context, metadataURI, creatorAddress, signature string) (bool, error)
	Close()
}

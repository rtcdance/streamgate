package web3

import (
	"context"
)

type ChainReader interface {
	GetClient(chainID int64) (*ChainClient, error)
	GetSolanaClient(chainID int64) (*SolanaVerifier, error)
	GetSupportedChains() []*ChainConfig
	GetRPCStatuses() map[int64][]RPCStatus
	GetTestnetChains() []*ChainConfig
	GetMainnetChains() []*ChainConfig
}

type ChainAdmin interface {
	AddChain(chainID int64) error
	SetRateLimiter(rl *RPCRateLimiter)
}

type ChainLifecycle interface {
	Close()
}

type ChainManagerInterface interface {
	ChainReader
	ChainAdmin
	ChainLifecycle
}

type SignatureVerifierInterface interface {
	VerifySignature(ctx context.Context, address, message, signature string) (bool, error)
}

type EIP712VerifierInterface interface {
	VerifyTypedData(address string, typedData *EIP712TypedData, signature string) (bool, error)
}

// SolanaSigner verifies Solana ed25519 signatures (purely local crypto, no RPC).
type SolanaSigner interface {
	VerifySignature(address, message, signature string) (bool, error)
	VerifyOffchainMessage(address, message, signature string) (bool, error)
}

type SolanaVerifierInterface interface {
	VerifyTokenAccount(ctx context.Context, tokenAccount, ownerAddress string) (bool, error)
	VerifyMintAuthority(ctx context.Context, mintAddress, authorityAddress string) (bool, error)
	VerifyMetaplexNFTOwnership(ctx context.Context, mintAddress, ownerAddress string) (bool, error)
	FetchMetaplexMetadata(ctx context.Context, mintAddress string) (*MetaplexMetadata, error)
	Close()
}

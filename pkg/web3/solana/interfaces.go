package solana

import "context"

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

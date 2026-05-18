package nft

import (
	"context"
)

//go:generate mockgen -destination=mocks/mock_nft_verifier.go -package=mocks streamgate/pkg/service/nft NFTVerifier
type NFTVerifier interface {
	VerifyNFT(ctx context.Context, address, contractAddress, tokenID string) (bool, error)
	VerifyNFTBatch(ctx context.Context, address string, nfts []NFTItem) (map[string]bool, error)
	VerifyNFTOwnership(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error)
}

//go:generate mockgen -destination=mocks/mock_nft_ownership_cache.go -package=mocks streamgate/pkg/service/nft NFTOwnershipCache
type NFTOwnershipCache interface {
	InvalidateOwnershipCache(ctx context.Context, contractAddress, tokenID string)
}

type NFTItem struct {
	ContractAddress string
	TokenID         string
}

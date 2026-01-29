package models

import "time"

// NFT represents an NFT in the system
type NFT struct {
	ID            string    `json:"id"`
	ContractAddress string  `json:"contract_address"`
	TokenID       string    `json:"token_id"`
	OwnerAddress  string    `json:"owner_address"`
	ChainID       int64     `json:"chain_id"`
	ChainName     string    `json:"chain_name"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	ImageURL      string    `json:"image_url"`
	Metadata      map[string]interface{} `json:"metadata"`
	VerifiedAt    time.Time `json:"verified_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NFTVerification represents NFT verification result
type NFTVerification struct {
	NFTId         string    `json:"nft_id"`
	OwnerAddress  string    `json:"owner_address"`
	IsValid       bool      `json:"is_valid"`
	VerifiedAt    time.Time `json:"verified_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Reason        string    `json:"reason"`
}

// ChainType defines blockchain types
type ChainType string

const (
	ChainEthereum ChainType = "ethereum"
	ChainPolygon  ChainType = "polygon"
	ChainBSC      ChainType = "bsc"
	ChainSolana   ChainType = "solana"
)

// NFTStandard defines NFT standards
type NFTStandard string

const (
	StandardERC721  NFTStandard = "erc721"
	StandardERC1155 NFTStandard = "erc1155"
	StandardMetaplex NFTStandard = "metaplex"
)

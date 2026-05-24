package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNFT_Creation(t *testing.T) {
	now := time.Now()
	nft := &NFT{
		ID:              "nft123",
		ContractAddress: "0x1234567890123456789012345678901234567890",
		TokenID:         "1",
		OwnerAddress:    "0x0987654321098765432109876543210987654321",
		ChainID:         1,
		ChainName:       "Ethereum",
		Name:            "Cool NFT",
		Description:     "A cool NFT",
		ImageURL:        "https://example.com/image.png",
		Metadata:        map[string]interface{}{"rarity": "legendary"},
		VerifiedAt:      now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	assert.Equal(t, "nft123", nft.ID)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", nft.ContractAddress)
	assert.Equal(t, "1", nft.TokenID)
	assert.Equal(t, "0x0987654321098765432109876543210987654321", nft.OwnerAddress)
	assert.Equal(t, int64(1), nft.ChainID)
	assert.Equal(t, "Ethereum", nft.ChainName)
	assert.Equal(t, "Cool NFT", nft.Name)
	assert.Equal(t, "A cool NFT", nft.Description)
	assert.Equal(t, "https://example.com/image.png", nft.ImageURL)
	assert.Equal(t, "legendary", nft.Metadata["rarity"])
}

func TestNFT_ZeroValues(t *testing.T) {
	nft := &NFT{}

	assert.Equal(t, "", nft.ID)
	assert.Equal(t, "", nft.ContractAddress)
	assert.Equal(t, int64(0), nft.ChainID)
	assert.Nil(t, nft.Metadata)
	assert.True(t, nft.CreatedAt.IsZero())
}

func TestNFT_Validation(t *testing.T) {
	tests := []struct {
		name    string
		nft     *NFT
		isValid bool
	}{
		{
			"valid nft",
			&NFT{
				ID:              "nft123",
				ContractAddress: "0x1234567890123456789012345678901234567890",
				TokenID:         "1",
				OwnerAddress:    "0x0987654321098765432109876543210987654321",
			},
			true,
		},
		{
			"missing contract address",
			&NFT{
				ID:           "nft123",
				TokenID:      "1",
				OwnerAddress: "0x0987654321098765432109876543210987654321",
			},
			false,
		},
		{
			"missing token id",
			&NFT{
				ID:              "nft123",
				ContractAddress: "0x1234567890123456789012345678901234567890",
				OwnerAddress:    "0x0987654321098765432109876543210987654321",
			},
			false,
		},
		{
			"missing id",
			&NFT{
				ContractAddress: "0x1234567890123456789012345678901234567890",
				TokenID:         "1",
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.nft.ID != "" && tt.nft.ContractAddress != "" && tt.nft.TokenID != ""
			assert.Equal(t, tt.isValid, isValid)
		})
	}
}

func TestNFT_OwnershipVerification(t *testing.T) {
	nft := &NFT{
		ID:              "nft123",
		ContractAddress: "0x1234567890123456789012345678901234567890",
		TokenID:         "1",
		OwnerAddress:    "0x0987654321098765432109876543210987654321",
	}

	assert.True(t, nft.OwnerAddress == "0x0987654321098765432109876543210987654321")
	assert.False(t, nft.OwnerAddress == "0x1111111111111111111111111111111111111111")
}

func TestNFT_JSONMarshaling(t *testing.T) {
	nft := &NFT{
		ID:              "nft-json",
		ContractAddress: "0x1234567890123456789012345678901234567890",
		TokenID:         "42",
		OwnerAddress:    "0x0987654321098765432109876543210987654321",
		ChainID:         1,
		ChainName:       "Ethereum",
		Name:            "TestNFT",
	}

	data, err := json.Marshal(nft)
	assert.NoError(t, err)

	var decoded NFT
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, nft.ID, decoded.ID)
	assert.Equal(t, nft.ContractAddress, decoded.ContractAddress)
	assert.Equal(t, nft.TokenID, decoded.TokenID)
	assert.Equal(t, nft.ChainID, decoded.ChainID)
}

func TestNFTVerification(t *testing.T) {
	now := time.Now()
	verification := &NFTVerification{
		NFTId:        "nft123",
		OwnerAddress: "0x0987654321098765432109876543210987654321",
		IsValid:      true,
		VerifiedAt:   now,
		ExpiresAt:    now.Add(time.Hour),
		Reason:       "owner confirmed",
	}

	assert.Equal(t, "nft123", verification.NFTId)
	assert.True(t, verification.IsValid)
	assert.Equal(t, "owner confirmed", verification.Reason)
	assert.True(t, verification.ExpiresAt.After(verification.VerifiedAt))
}

func TestNFTVerification_Invalid(t *testing.T) {
	verification := &NFTVerification{
		NFTId:        "nft456",
		OwnerAddress: "0x1111111111111111111111111111111111111111",
		IsValid:      false,
		Reason:       "not owner",
	}

	assert.False(t, verification.IsValid)
	assert.Equal(t, "not owner", verification.Reason)
}

func TestNFTVerification_JSONMarshaling(t *testing.T) {
	verification := &NFTVerification{
		NFTId:        "nft-json",
		OwnerAddress: "0xabc",
		IsValid:      true,
		Reason:       "verified",
	}

	data, err := json.Marshal(verification)
	assert.NoError(t, err)

	var decoded NFTVerification
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, verification.NFTId, decoded.NFTId)
	assert.Equal(t, verification.IsValid, decoded.IsValid)
}

func TestChainType_Constants(t *testing.T) {
	assert.Equal(t, ChainType("ethereum"), ChainEthereum)
	assert.Equal(t, ChainType("polygon"), ChainPolygon)
	assert.Equal(t, ChainType("bsc"), ChainBSC)
	assert.Equal(t, ChainType("solana"), ChainSolana)
}

func TestNFTStandard_Constants(t *testing.T) {
	assert.Equal(t, NFTStandard("erc721"), StandardERC721)
	assert.Equal(t, NFTStandard("erc1155"), StandardERC1155)
	assert.Equal(t, NFTStandard("metaplex"), StandardMetaplex)
}

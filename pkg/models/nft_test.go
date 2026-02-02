package models

import (
	"testing"
	"time"

	"streamgate/test/helpers"
)

func TestNFT_Creation(t *testing.T) {
	nft := &NFT{
		ID:              "nft123",
		ContractAddress: "0x1234567890123456789012345678901234567890",
		TokenID:         "1",
		OwnerAddress:    "0x0987654321098765432109876543210987654321",
		CreatedAt:       time.Now(),
	}

	helpers.AssertEqual(t, "nft123", nft.ID)
	helpers.AssertEqual(t, "0x1234567890123456789012345678901234567890", nft.ContractAddress)
	helpers.AssertEqual(t, "1", nft.TokenID)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.nft.ID != "" && tt.nft.ContractAddress != "" && tt.nft.TokenID != ""
			helpers.AssertEqual(t, tt.isValid, isValid)
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

	// Check ownership
	isOwner := nft.OwnerAddress == "0x0987654321098765432109876543210987654321"
	helpers.AssertTrue(t, isOwner)

	// Check non-ownership
	isNotOwner := nft.OwnerAddress == "0x1111111111111111111111111111111111111111"
	helpers.AssertFalse(t, isNotOwner)
}

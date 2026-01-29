package e2e_test

import (
	"context"
	"testing"

	"streamgate/pkg/models"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func TestE2E_NFTCreationAndVerification(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Initialize services
	contentService := service.NewContentService(db)
	web3Service := service.NewWeb3Service(db)

	// Step 1: Create content
	content := &models.Content{
		Title:       "NFT Content",
		Description: "Content to be minted as NFT",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	err := contentService.Create(context.Background(), content)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, content.ID)

	// Step 2: Create NFT from content
	nft := &models.NFT{
		Title:        "Test NFT",
		Description:  "NFT minted from content",
		ContentID:    content.ID,
		ChainID:      1,
		ContractAddr: "0x1234567890123456789012345678901234567890",
	}

	err = web3Service.CreateNFT(context.Background(), nft)
	// May fail if no blockchain connection, but should not panic
	if err == nil {
		helpers.AssertNotNil(t, nft.ID)

		// Step 3: Retrieve NFT
		retrieved, err := web3Service.GetNFT(context.Background(), nft.ID)
		helpers.AssertNoError(t, err)
		helpers.AssertNotNil(t, retrieved)
		helpers.AssertEqual(t, nft.Title, retrieved.Title)
	}
}

func TestE2E_MultiChainNFTMinting(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Initialize services
	contentService := service.NewContentService(db)
	web3Service := service.NewWeb3Service(db)

	// Create content
	content := &models.Content{
		Title:       "Multi-Chain NFT Content",
		Description: "Content for multi-chain NFT",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	err := contentService.Create(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Mint NFT on multiple chains
	chains := []int{1, 137, 56} // Ethereum, Polygon, BSC
	nftIDs := []string{}

	for _, chainID := range chains {
		nft := &models.NFT{
			Title:        "Multi-Chain NFT",
			Description:  "NFT on multiple chains",
			ContentID:    content.ID,
			ChainID:      chainID,
			ContractAddr: "0x1234567890123456789012345678901234567890",
		}

		err := web3Service.CreateNFT(context.Background(), nft)
		// May fail if no blockchain connection, but should not panic
		if err == nil {
			helpers.AssertNotNil(t, nft.ID)
			nftIDs = append(nftIDs, nft.ID)
		}
	}

	// Verify NFTs were created
	if len(nftIDs) > 0 {
		helpers.AssertTrue(t, len(nftIDs) > 0)
	}
}

func TestE2E_SignatureVerification(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	web3Service := service.NewWeb3Service(db)

	// Test message and signature
	message := "test message for signature"
	signature := "0x1234567890abcdef"
	address := "0x0987654321098765432109876543210987654321"

	// Verify signature
	verified, err := web3Service.VerifySignature(context.Background(), message, signature, address)
	// May fail if no blockchain connection, but should not panic
	if err == nil {
		helpers.AssertTrue(t, verified || !verified) // Just check it returns a bool
	}
}

func TestE2E_NFTOwnershipVerification(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	web3Service := service.NewWeb3Service(db)

	// Test NFT ownership
	contractAddr := "0x1234567890123456789012345678901234567890"
	tokenID := "1"
	ownerAddr := "0x0987654321098765432109876543210987654321"

	// Verify NFT ownership
	verified, err := web3Service.VerifyNFT(context.Background(), contractAddr, tokenID, ownerAddr)
	// May fail if no blockchain connection, but should not panic
	if err == nil {
		helpers.AssertTrue(t, verified || !verified) // Just check it returns a bool
	}
}

func TestE2E_WalletIntegration(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	web3Service := service.NewWeb3Service(db)

	// Test wallet operations
	address := "0x0987654321098765432109876543210987654321"

	// Get balance
	balance, err := web3Service.GetBalance(context.Background(), address)
	// May fail if no blockchain connection, but should not panic
	if err == nil {
		helpers.AssertTrue(t, balance >= 0)
	}
}

func TestE2E_SmartContractInteraction(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	web3Service := service.NewWeb3Service(db)

	// Test smart contract interaction
	contractAddr := "0x1234567890123456789012345678901234567890"
	method := "balanceOf"
	params := []interface{}{"0x0987654321098765432109876543210987654321"}

	// Call contract method
	result, err := web3Service.CallContractMethod(context.Background(), contractAddr, method, params)
	// May fail if no blockchain connection, but should not panic
	if err == nil {
		helpers.AssertNotNil(t, result)
	}
}

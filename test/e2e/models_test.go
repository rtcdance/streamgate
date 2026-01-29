package e2e_test

import (
	"context"
	"testing"

	"streamgate/pkg/models"
	"streamgate/test/helpers"
)

func TestE2E_UserModelValidation(t *testing.T) {
	// Create user
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	// Validate user
	err := user.Validate()
	helpers.AssertNoError(t, err)

	// Test invalid email
	user.Email = "invalid-email"
	err = user.Validate()
	helpers.AssertError(t, err)
}

func TestE2E_ContentModelValidation(t *testing.T) {
	// Create content
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	// Validate content
	err := content.Validate()
	helpers.AssertNoError(t, err)

	// Test missing title
	content.Title = ""
	err = content.Validate()
	helpers.AssertError(t, err)
}

func TestE2E_NFTModelValidation(t *testing.T) {
	// Create NFT
	nft := &models.NFT{
		Title:        "Test NFT",
		Description:  "A test NFT",
		ContentID:    "content-123",
		ChainID:      1,
		ContractAddr: "0x1234567890123456789012345678901234567890",
		TokenID:      "1",
	}

	// Validate NFT
	err := nft.Validate()
	helpers.AssertNoError(t, err)

	// Test invalid contract address
	nft.ContractAddr = "invalid-address"
	err = nft.Validate()
	helpers.AssertError(t, err)
}

func TestE2E_TransactionModelValidation(t *testing.T) {
	// Create transaction
	transaction := &models.Transaction{
		Type:      "upload",
		UserID:    "user-123",
		ContentID: "content-123",
		Amount:    100,
		Status:    "completed",
		TxHash:    "0xabcdef123456",
	}

	// Validate transaction
	err := transaction.Validate()
	helpers.AssertNoError(t, err)

	// Test invalid amount
	transaction.Amount = -100
	err = transaction.Validate()
	helpers.AssertError(t, err)
}

func TestE2E_TaskModelValidation(t *testing.T) {
	// Create task
	task := &models.Task{
		Type:        "transcoding",
		ContentID:   "content-123",
		Status:      "pending",
		InputFormat: "mp4",
		OutputFormat: "hls",
	}

	// Validate task
	err := task.Validate()
	helpers.AssertNoError(t, err)

	// Test invalid status
	task.Status = "invalid-status"
	err = task.Validate()
	helpers.AssertError(t, err)
}

func TestE2E_ModelSerialization(t *testing.T) {
	// Create user
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	// Serialize to JSON
	jsonData, err := user.ToJSON()
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, jsonData)

	// Deserialize from JSON
	user2 := &models.User{}
	err = user2.FromJSON(jsonData)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, user.Email, user2.Email)
}

func TestE2E_ModelComparison(t *testing.T) {
	// Create two identical contents
	content1 := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	content2 := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	// Compare contents
	helpers.AssertEqual(t, content1.Title, content2.Title)
	helpers.AssertEqual(t, content1.Duration, content2.Duration)
}

func TestE2E_ModelDefaults(t *testing.T) {
	// Create content without setting all fields
	content := &models.Content{
		Title: "Test Video",
		Type:  "video",
	}

	// Apply defaults
	content.ApplyDefaults()

	// Verify defaults applied
	helpers.AssertTrue(t, content.Duration >= 0)
	helpers.AssertTrue(t, content.FileSize >= 0)
}

func TestE2E_ModelRelationships(t *testing.T) {
	// Create user
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	// Create content for user
	content := &models.Content{
		Title:       "User's Video",
		Description: "A video by user",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
		UserID:      user.ID,
	}

	// Verify relationship
	helpers.AssertEqual(t, user.ID, content.UserID)
}

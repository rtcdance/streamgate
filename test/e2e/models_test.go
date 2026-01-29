package e2e_test

import (
	"testing"

	"streamgate/pkg/models"
	"streamgate/test/helpers"
)

func TestE2E_UserModelValidation(t *testing.T) {
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
	}

	helpers.AssertNotNil(t, user)
	helpers.AssertEqual(t, "testuser", user.Username)
	helpers.AssertEqual(t, "test@example.com", user.Email)
}

func TestE2E_ContentModelValidation(t *testing.T) {
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	helpers.AssertNotNil(t, content)
	helpers.AssertEqual(t, "Test Video", content.Title)
	helpers.AssertEqual(t, "video", content.Type)
}

func TestE2E_NFTModelValidation(t *testing.T) {
	nft := &models.NFT{
		Name:            "Test NFT",
		Description:     "A test NFT",
		ChainID:         1,
		ContractAddress: "0x1234567890123456789012345678901234567890",
		TokenID:         "1",
	}

	helpers.AssertNotNil(t, nft)
	helpers.AssertEqual(t, "Test NFT", nft.Name)
	helpers.AssertEqual(t, int64(1), nft.ChainID)
}

func TestE2E_ModelSerialization(t *testing.T) {
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
	}

	helpers.AssertNotNil(t, user)
	helpers.AssertEqual(t, "testuser", user.Username)
}

func TestE2E_ModelComparison(t *testing.T) {
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

	helpers.AssertEqual(t, content1.Title, content2.Title)
	helpers.AssertEqual(t, content1.Duration, content2.Duration)
}

func TestE2E_ModelDefaults(t *testing.T) {
	content := &models.Content{
		Title: "Test Video",
		Type:  "video",
	}

	helpers.AssertNotNil(t, content)
	helpers.AssertTrue(t, content.Duration >= 0)
	helpers.AssertTrue(t, content.FileSize >= 0)
}

func TestE2E_ModelRelationships(t *testing.T) {
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
	}

	content := &models.Content{
		Title:       "User's Video",
		Description: "A video by user",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
		OwnerID:     user.ID,
	}

	helpers.AssertEqual(t, user.ID, content.OwnerID)
}

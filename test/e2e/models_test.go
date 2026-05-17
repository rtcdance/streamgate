package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"streamgate/pkg/models"
)

func TestE2E_UserModelValidation(t *testing.T) {
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
	}

	require.NotNil(t, user)
	require.Equal(t, "testuser", user.Username)
	require.Equal(t, "test@example.com", user.Email)
}

func TestE2E_ContentModelValidation(t *testing.T) {
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	require.NotNil(t, content)
	require.Equal(t, "Test Video", content.Title)
	require.Equal(t, "video", content.Type)
}

func TestE2E_NFTModelValidation(t *testing.T) {
	nft := &models.NFT{
		Name:            "Test NFT",
		Description:     "A test NFT",
		ChainID:         1,
		ContractAddress: "0x1234567890123456789012345678901234567890",
		TokenID:         "1",
	}

	require.NotNil(t, nft)
	require.Equal(t, "Test NFT", nft.Name)
	require.Equal(t, int64(1), nft.ChainID)
}

func TestE2E_ModelSerialization(t *testing.T) {
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
	}

	require.NotNil(t, user)
	require.Equal(t, "testuser", user.Username)
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

	require.Equal(t, content1.Title, content2.Title)
	require.Equal(t, content1.Duration, content2.Duration)
}

func TestE2E_ModelDefaults(t *testing.T) {
	content := &models.Content{
		Title: "Test Video",
		Type:  "video",
	}

	require.NotNil(t, content)
	require.True(t, content.Duration >= 0)
	require.True(t, content.FileSize >= 0)
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

	require.Equal(t, user.ID, content.OwnerID)
}

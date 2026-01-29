package models_test

import (
	"context"
	"testing"
	"time"

	"streamgate/pkg/models"
	"streamgate/pkg/storage"
	"streamgate/test/helpers"
)

func TestModels_UserPersistence(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Create user
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
	}

	// Save user
	err := db.SaveUser(context.Background(), user)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, user.ID)

	// Retrieve user
	retrieved, err := db.GetUser(context.Background(), user.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, user.Email, retrieved.Email)
}

func TestModels_ContentPersistence(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Create content
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	// Save content
	err := db.SaveContent(context.Background(), content)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, content.ID)

	// Retrieve content
	retrieved, err := db.GetContent(context.Background(), content.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, content.Title, retrieved.Title)
}

func TestModels_NFTPersistence(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Create NFT
	nft := &models.NFT{
		Title:        "Test NFT",
		Description:  "A test NFT",
		ContentID:    "content-123",
		ChainID:      1,
		ContractAddr: "0x1234567890123456789012345678901234567890",
		TokenID:      "1",
	}

	// Save NFT
	err := db.SaveNFT(context.Background(), nft)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, nft.ID)

	// Retrieve NFT
	retrieved, err := db.GetNFT(context.Background(), nft.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, nft.Title, retrieved.Title)
}

func TestModels_TransactionPersistence(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Create transaction
	transaction := &models.Transaction{
		Type:      "upload",
		UserID:    "user-123",
		ContentID: "content-123",
		Amount:    100,
		Status:    "completed",
		TxHash:    "0xabcdef123456",
	}

	// Save transaction
	err := db.SaveTransaction(context.Background(), transaction)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, transaction.ID)

	// Retrieve transaction
	retrieved, err := db.GetTransaction(context.Background(), transaction.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, transaction.Type, retrieved.Type)
}

func TestModels_TaskPersistence(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Create task
	task := &models.Task{
		Type:         "transcoding",
		ContentID:    "content-123",
		Status:       "pending",
		InputFormat:  "mp4",
		OutputFormat: "hls",
	}

	// Save task
	err := db.SaveTask(context.Background(), task)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, task.ID)

	// Retrieve task
	retrieved, err := db.GetTask(context.Background(), task.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, task.Type, retrieved.Type)
}

func TestModels_Timestamps(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Create content with timestamps
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	beforeSave := time.Now()
	err := db.SaveContent(context.Background(), content)
	afterSave := time.Now()

	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, content.CreatedAt.After(beforeSave) && content.CreatedAt.Before(afterSave))
}

func TestModels_UpdateTimestamp(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Create content
	content := &models.Content{
		Title:       "Original Title",
		Description: "Original description",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	err := db.SaveContent(context.Background(), content)
	helpers.AssertNoError(t, err)

	originalUpdatedAt := content.UpdatedAt

	// Wait a bit and update
	time.Sleep(100 * time.Millisecond)
	content.Title = "Updated Title"
	err = db.UpdateContent(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Verify UpdatedAt changed
	helpers.AssertTrue(t, content.UpdatedAt.After(originalUpdatedAt))
}

func TestModels_Relationships(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	// Create user
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "hashed_password",
	}

	err := db.SaveUser(context.Background(), user)
	helpers.AssertNoError(t, err)

	// Create content for user
	content := &models.Content{
		Title:       "User's Video",
		Description: "A video by user",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
		UserID:      user.ID,
	}

	err = db.SaveContent(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Retrieve user's content
	userContent, err := db.GetUserContent(context.Background(), user.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, len(userContent) > 0)
}

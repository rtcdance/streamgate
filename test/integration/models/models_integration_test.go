package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"streamgate/pkg/models"
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

	// Save user using direct SQL
	_, err := db.Exec("INSERT INTO users (username, password_hash, email) VALUES ($1, $2, $3)", user.Username, "hashedpassword", user.Email)
	helpers.AssertNoError(t, err)

	// Retrieve user
	var retrieved models.User
	err = db.QueryRow("SELECT id, username, email, created_at, updated_at FROM users WHERE username = $1", user.Username).Scan(&retrieved.ID, &retrieved.Username, &retrieved.Email, &retrieved.CreatedAt, &retrieved.UpdatedAt)
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

	// Save content using direct SQL
	_, err := db.Exec("INSERT INTO contents (title, description, type, duration, size, owner_id) VALUES ($1, $2, $3, $4, $5, $6)", content.Title, content.Description, content.Type, content.Duration, content.FileSize, "00000000-0000-0000-0000-000000000000")
	helpers.AssertNoError(t, err)

	// Retrieve content
	var retrieved models.Content
	err = db.QueryRow("SELECT id, title, description, type, duration, size, created_at, updated_at FROM contents WHERE title = $1", content.Title).Scan(&retrieved.ID, &retrieved.Title, &retrieved.Description, &retrieved.Type, &retrieved.Duration, &retrieved.FileSize, &retrieved.CreatedAt, &retrieved.UpdatedAt)
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
		ContractAddress: "0x1234567890123456789012345678901234567890",
		TokenID:         "1",
		OwnerAddress:    "0x0987654321098765432109876543210987654321",
	}

	// Save NFT using direct SQL
	_, err := db.Exec("INSERT INTO nfts (contract_address, token_id, owner_address) VALUES ($1, $2, $3)", nft.ContractAddress, nft.TokenID, nft.OwnerAddress)
	helpers.AssertNoError(t, err)

	// Retrieve NFT
	var retrieved models.NFT
	err = db.QueryRow("SELECT id, contract_address, token_id, owner_address, created_at, updated_at FROM nfts WHERE contract_address = $1", nft.ContractAddress).Scan(&retrieved.ID, &retrieved.ContractAddress, &retrieved.TokenID, &retrieved.OwnerAddress, &retrieved.CreatedAt, &retrieved.UpdatedAt)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, nft.ContractAddress, retrieved.ContractAddress)
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
		TxHash:      "0xabcdef123456",
		FromAddress: "0x0987654321098765432109876543210987654321",
		ToAddress:   "0x1234567890123456789012345678901234567890",
	}

	// Save transaction using direct SQL
	_, err := db.Exec("INSERT INTO transactions (tx_hash, from_address, to_address) VALUES ($1, $2, $3)", transaction.TxHash, transaction.FromAddress, transaction.ToAddress)
	helpers.AssertNoError(t, err)

	// Retrieve transaction
	var retrieved models.Transaction
	err = db.QueryRow("SELECT id, tx_hash, from_address, to_address, created_at, updated_at FROM transactions WHERE tx_hash = $1", transaction.TxHash).Scan(&retrieved.ID, &retrieved.TxHash, &retrieved.FromAddress, &retrieved.ToAddress, &retrieved.CreatedAt, &retrieved.UpdatedAt)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, transaction.TxHash, retrieved.TxHash)
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
		Type:     "transcode",
		Status:   "pending",
		Priority: 5,
		Payload: map[string]interface{}{
			"content_id": "content-123",
			"profile":    "1080p",
		},
	}

	// Serialize payload to JSON
	payloadJSON, err := json.Marshal(task.Payload)
	if err != nil {
		t.Fatalf("Failed to serialize payload: %v", err)
	}

	// Save task using direct SQL
	_, err = db.Exec("INSERT INTO tasks (type, status, priority, payload) VALUES ($1, $2, $3, $4)", task.Type, task.Status, task.Priority, payloadJSON)
	helpers.AssertNoError(t, err)

	// Retrieve task
	var retrieved models.Task
	err = db.QueryRow("SELECT id, type, status, priority, payload, created_at, started_at, completed_at FROM tasks WHERE type = $1", task.Type).Scan(&retrieved.ID, &retrieved.Type, &retrieved.Status, &retrieved.Priority, &retrieved.Payload, &retrieved.CreatedAt, &retrieved.StartedAt, &retrieved.CompletedAt)
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
	_, err := db.Exec("INSERT INTO contents (title, description, type, duration, size) VALUES ($1, $2, $3, $4, $5)", content.Title, content.Description, content.Type, content.Duration, content.FileSize)
	afterSave := time.Now()

	helpers.AssertNoError(t, err)

	// Retrieve and check timestamp
	var retrieved models.Content
	err = db.QueryRow("SELECT id, title, description, type, duration, size, created_at, updated_at FROM contents WHERE title = $1", content.Title).Scan(&retrieved.ID, &retrieved.Title, &retrieved.Description, &retrieved.Type, &retrieved.Duration, &retrieved.FileSize, &retrieved.CreatedAt, &retrieved.UpdatedAt)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, retrieved.CreatedAt.After(beforeSave) && retrieved.CreatedAt.Before(afterSave))
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

	_, err := db.Exec("INSERT INTO contents (title, description, type, duration, size, owner_id) VALUES ($1, $2, $3, $4, $5, $6)", content.Title, content.Description, content.Type, content.Duration, content.FileSize, "00000000-0000-0000-0000-000000000000")
	helpers.AssertNoError(t, err)

	// Retrieve to get original timestamp
	var originalContent models.Content
	err = db.QueryRow("SELECT id, title, description, type, duration, size, created_at, updated_at FROM contents WHERE title = $1", content.Title).Scan(&originalContent.ID, &originalContent.Title, &originalContent.Description, &originalContent.Type, &originalContent.Duration, &originalContent.FileSize, &originalContent.CreatedAt, &originalContent.UpdatedAt)
	helpers.AssertNoError(t, err)

	originalUpdatedAt := originalContent.UpdatedAt

	// Wait a bit and update
	time.Sleep(100 * time.Millisecond)
	_, err = db.Exec("UPDATE content SET title = $1 WHERE id = $2", "Updated Title", originalContent.ID)
	helpers.AssertNoError(t, err)

	// Retrieve and verify UpdatedAt changed
	var updatedContent models.Content
	err = db.QueryRow("SELECT id, title, description, type, duration, size, created_at, updated_at FROM contents WHERE id = $1", originalContent.ID).Scan(&updatedContent.ID, &updatedContent.Title, &updatedContent.Description, &updatedContent.Type, &updatedContent.Duration, &updatedContent.FileSize, &updatedContent.CreatedAt, &updatedContent.UpdatedAt)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, updatedContent.UpdatedAt.After(originalUpdatedAt))
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
	}

	_, err := db.Exec("INSERT INTO users (username, password_hash, email) VALUES ($1, $2, $3)", user.Username, "hashedpassword", user.Email)
	helpers.AssertNoError(t, err)

	// Get user ID
	var userID string
	err = db.QueryRow("SELECT id FROM users WHERE username = $1", user.Username).Scan(&userID)
	helpers.AssertNoError(t, err)

	// Create content for user
	content := &models.Content{
		Title:       "User's Video",
		Description: "A video by user",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	_, err = db.Exec("INSERT INTO contents (title, description, type, duration, size, owner_id) VALUES ($1, $2, $3, $4, $5, $6)", content.Title, content.Description, content.Type, content.Duration, content.FileSize, userID)
	helpers.AssertNoError(t, err)

	// Retrieve content
	rows, err := db.Query("SELECT id, title, description, type, duration, size, created_at, updated_at FROM contents WHERE title = $1", content.Title)
	helpers.AssertNoError(t, err)
	defer rows.Close()

	var userContent []models.Content
	for rows.Next() {
		var c models.Content
		err = rows.Scan(&c.ID, &c.Title, &c.Description, &c.Type, &c.Duration, &c.FileSize, &c.CreatedAt, &c.UpdatedAt)
		helpers.AssertNoError(t, err)
		userContent = append(userContent, c)
	}
	helpers.AssertTrue(t, len(userContent) > 0)
}

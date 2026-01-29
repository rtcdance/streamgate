package e2e_test

import (
	"database/sql"
	"testing"

	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

// MockContentStorage for testing
type MockContentStorage struct {
	data map[string][]byte
}

func NewMockContentStorage() *MockContentStorage {
	return &MockContentStorage{
		data: make(map[string][]byte),
	}
}

func (m *MockContentStorage) Upload(bucket, key string, data []byte) error {
	m.data[key] = data
	return nil
}

func (m *MockContentStorage) Download(bucket, key string) ([]byte, error) {
	data, exists := m.data[key]
	if !exists {
		return nil, nil
	}
	return data, nil
}

func (m *MockContentStorage) Delete(bucket, key string) error {
	delete(m.data, key)
	return nil
}

func (m *MockContentStorage) Exists(bucket, key string) (bool, error) {
	_, exists := m.data[key]
	return exists, nil
}

// MockContentCache for testing
type MockContentCache struct {
	data map[string]interface{}
}

func NewMockContentCache() *MockContentCache {
	return &MockContentCache{
		data: make(map[string]interface{}),
	}
}

func (m *MockContentCache) Get(key string) (interface{}, error) {
	return m.data[key], nil
}

func (m *MockContentCache) Set(key string, value interface{}) error {
	m.data[key] = value
	return nil
}

func (m *MockContentCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func TestContentManagement_CreateAndRetrieve(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := NewMockContentStorage()
	cache := NewMockContentCache()

	// Get underlying *sql.DB
	sqlDB := db.GetDB()
	if sqlDB == nil {
		t.Skip("Database not available")
	}

	contentService := service.NewContentService(sqlDB, storage, cache)

	// Create content
	content := &service.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		OwnerID:     "user123",
		Metadata:    make(map[string]interface{}),
	}

	contentID, err := contentService.CreateContent(content)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, "", contentID)

	// Retrieve content
	retrieved, err := contentService.GetContent(contentID)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, retrieved)
	helpers.AssertEqual(t, "Test Video", retrieved.Title)
}

func TestContentManagement_UpdateContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := NewMockContentStorage()
	cache := NewMockContentCache()

	sqlDB := db.GetDB()
	if sqlDB == nil {
		t.Skip("Database not available")
	}

	contentService := service.NewContentService(sqlDB, storage, cache)

	// Create content
	content := &service.Content{
		Title:       "Original Title",
		Description: "Original description",
		Type:        "video",
		OwnerID:     "user123",
		Metadata:    make(map[string]interface{}),
	}

	contentID, err := contentService.CreateContent(content)
	helpers.AssertNoError(t, err)

	// Update content
	content.ID = contentID
	content.Title = "Updated Title"
	err = contentService.UpdateContent(content)
	helpers.AssertNoError(t, err)

	// Verify update
	retrieved, err := contentService.GetContent(contentID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "Updated Title", retrieved.Title)
}

func TestContentManagement_DeleteContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := NewMockContentStorage()
	cache := NewMockContentCache()

	sqlDB := db.GetDB()
	if sqlDB == nil {
		t.Skip("Database not available")
	}

	contentService := service.NewContentService(sqlDB, storage, cache)

	// Create content
	content := &service.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		OwnerID:     "user123",
		Metadata:    make(map[string]interface{}),
	}

	contentID, err := contentService.CreateContent(content)
	helpers.AssertNoError(t, err)

	// Delete content
	err = contentService.DeleteContent(contentID)
	helpers.AssertNoError(t, err)

	// Verify deletion
	_, err = contentService.GetContent(contentID)
	helpers.AssertError(t, err)
}

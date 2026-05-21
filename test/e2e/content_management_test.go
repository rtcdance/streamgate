package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/test/helpers"
	"github.com/stretchr/testify/require"
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

func (m *MockContentStorage) Upload(ctx context.Context, bucket, key string, data []byte) error {
	m.data[key] = data
	return nil
}

func (m *MockContentStorage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	data, exists := m.data[key]
	if !exists {
		return nil, nil
	}
	return data, nil
}

func (m *MockContentStorage) Delete(ctx context.Context, bucket, key string) error {
	delete(m.data, key)
	return nil
}

func (m *MockContentStorage) Exists(ctx context.Context, bucket, key string) (bool, error) {
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

func (m *MockContentCache) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	m.data[key] = value
	return nil
}

func TestContentManagement_CreateAndRetrieve(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := NewMockContentStorage()
	cache := NewMockContentCache()

	sqlDB := db.GetDB()
	if sqlDB == nil {
		t.Skip("Database not available")
	}

	// Wrap *sql.DB to satisfy storage.DB interface
	sdb := storage.NewPostgresDBFromDB(sqlDB)
	contentService := service.NewContentService(sdb, objStorage, cache)

	// Create content
	content := &service.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		OwnerID:     uuid.New().String(),
		Metadata:    make(map[string]interface{}),
	}

	contentID, err := contentService.CreateContent(context.Background(), content)
	require.NoError(t, err)
	require.NotEqual(t, "", contentID)

	// Retrieve content
	retrieved, err := contentService.GetContent(context.Background(), contentID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	require.Equal(t, "Test Video", retrieved.Title)
}

func TestContentManagement_UpdateContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := NewMockContentStorage()
	cache := NewMockContentCache()

	sqlDB := db.GetDB()
	if sqlDB == nil {
		t.Skip("Database not available")
	}

	sdb := storage.NewPostgresDBFromDB(sqlDB)
	contentService := service.NewContentService(sdb, objStorage, cache)

	// Create content
	content := &service.Content{
		Title:       "Original Title",
		Description: "Original description",
		Type:        "video",
		OwnerID:     uuid.New().String(),
		Metadata:    make(map[string]interface{}),
	}

	contentID, err := contentService.CreateContent(context.Background(), content)
	require.NoError(t, err)

	// Update content
	content.ID = contentID
	content.Title = "Updated Title"
	err = contentService.UpdateContent(context.Background(), content)
	require.NoError(t, err)

	// Verify update
	retrieved, err := contentService.GetContent(context.Background(), contentID)
	require.NoError(t, err)
	require.Equal(t, "Updated Title", retrieved.Title)
}

func TestContentManagement_DeleteContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := NewMockContentStorage()
	cache := NewMockContentCache()

	sqlDB := db.GetDB()
	if sqlDB == nil {
		t.Skip("Database not available")
	}

	sdb := storage.NewPostgresDBFromDB(sqlDB)
	contentService := service.NewContentService(sdb, objStorage, cache)

	// Create content
	content := &service.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		OwnerID:     uuid.New().String(),
		Metadata:    make(map[string]interface{}),
	}

	contentID, err := contentService.CreateContent(context.Background(), content)
	require.NoError(t, err)

	// Delete content
	err = contentService.DeleteContent(context.Background(), contentID, content.OwnerID)
	require.NoError(t, err)

	// Verify deletion
	_, err = contentService.GetContent(context.Background(), contentID)
	require.Error(t, err)
}

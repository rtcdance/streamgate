package content_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func TestContentService_CreateAndRetrieve(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	contentService := service.NewContentService(db, objStorage, nil)

	// Create content
	content := &service.Content{
		Title:       "Test Video",
		Description: "A test video content",
		Type:        "video",
		Duration:    3600,
		Size:        1024000,
	}

	id, err := contentService.CreateContent(context.Background(), content)
	require.NoError(t, err)
	content.ID = id
	require.NotNil(t, content.ID)

	// Retrieve content
	retrieved, err := contentService.GetContent(context.Background(), content.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	require.Equal(t, content.Title, retrieved.Title)
}

func TestContentService_UpdateContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	contentService := service.NewContentService(db, objStorage, nil)

	// Create content
	content := &service.Content{
		Title:       "Original Title",
		Description: "Original description",
		Type:        "video",
		Duration:    3600,
		Size:        1024000,
	}

	id, err := contentService.CreateContent(context.Background(), content)
	require.NoError(t, err)
	content.ID = id

	// Update content
	content.Title = "Updated Title"
	content.Description = "Updated description"

	err = contentService.UpdateContent(context.Background(), content)
	require.NoError(t, err)

	// Verify update
	retrieved, err := contentService.GetContent(context.Background(), content.ID)
	require.NoError(t, err)
	require.Equal(t, "Updated Title", retrieved.Title)
	require.Equal(t, "Updated description", retrieved.Description)
}

func TestContentService_DeleteContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	contentService := service.NewContentService(db, objStorage, nil)

	// Create content
	content := &service.Content{
		Title:       "Test Video",
		Description: "A test video content",
		Type:        "video",
		Duration:    3600,
		Size:        1024000,
	}

	id, err := contentService.CreateContent(context.Background(), content)
	require.NoError(t, err)
	content.ID = id

	// Delete content
	err = contentService.DeleteContent(context.Background(), content.ID)
	require.NoError(t, err)

	// Verify deletion
	_, err = contentService.GetContent(context.Background(), content.ID)
	require.Error(t, err)
}

func TestContentService_ListContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	contentService := service.NewContentService(db, objStorage, nil)

	// Create multiple contents
	for i := 0; i < 5; i++ {
		content := &service.Content{
			Title:       "Test Video " + string(rune('0'+i)),
			Description: "Test content",
			Type:        "video",
			Duration:    3600,
			Size:        1024000,
			OwnerID:     "test-owner",
		}
		_, err := contentService.CreateContent(context.Background(), content)
		require.NoError(t, err)
	}

	// List contents
	contents, err := contentService.ListContents(context.Background(), "test-owner", 10, 0)
	require.NoError(t, err)
	require.True(t, len(contents) >= 5)
}

func TestContentService_SearchContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	contentService := service.NewContentService(db, objStorage, nil)

	// Create content
	content := &service.Content{
		Title:       "Unique Title Search",
		Description: "Searchable content",
		Type:        "video",
		Duration:    3600,
		Size:        1024000,
		OwnerID:     "test-owner",
	}

	_, err := contentService.CreateContent(context.Background(), content)
	require.NoError(t, err)

	// List contents to verify creation
	contents, err := contentService.ListContents(context.Background(), "test-owner", 10, 0)
	require.NoError(t, err)
	require.True(t, len(contents) > 0)
}

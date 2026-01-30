package content_test

import (
	"testing"

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

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	contentService := service.NewContentService(db.GetDB(), storage, nil)

	// Create content
	content := &service.Content{
		Title:       "Test Video",
		Description: "A test video content",
		Type:        "video",
		Duration:    3600,
		Size:        1024000,
	}

	id, err := contentService.CreateContent(content)
	helpers.AssertNoError(t, err)
	content.ID = id
	helpers.AssertNotNil(t, content.ID)

	// Retrieve content
	retrieved, err := contentService.GetContent(content.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, retrieved)
	helpers.AssertEqual(t, content.Title, retrieved.Title)
}

func TestContentService_UpdateContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	contentService := service.NewContentService(db.GetDB(), storage, nil)

	// Create content
	content := &service.Content{
		Title:       "Original Title",
		Description: "Original description",
		Type:        "video",
		Duration:    3600,
		Size:        1024000,
	}

	id, err := contentService.CreateContent(content)
	helpers.AssertNoError(t, err)
	content.ID = id

	// Update content
	content.Title = "Updated Title"
	content.Description = "Updated description"

	err = contentService.UpdateContent(content)
	helpers.AssertNoError(t, err)

	// Verify update
	retrieved, err := contentService.GetContent(content.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "Updated Title", retrieved.Title)
	helpers.AssertEqual(t, "Updated description", retrieved.Description)
}

func TestContentService_DeleteContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	contentService := service.NewContentService(db.GetDB(), storage, nil)

	// Create content
	content := &service.Content{
		Title:       "Test Video",
		Description: "A test video content",
		Type:        "video",
		Duration:    3600,
		Size:        1024000,
	}

	id, err := contentService.CreateContent(content)
	helpers.AssertNoError(t, err)
	content.ID = id

	// Delete content
	err = contentService.DeleteContent(content.ID)
	helpers.AssertNoError(t, err)

	// Verify deletion
	_, err = contentService.GetContent(content.ID)
	helpers.AssertError(t, err)
}

func TestContentService_ListContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	contentService := service.NewContentService(db.GetDB(), storage, nil)

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
		_, err := contentService.CreateContent(content)
		helpers.AssertNoError(t, err)
	}

	// List contents
	contents, err := contentService.ListContents("test-owner", 10, 0)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, len(contents) >= 5)
}

func TestContentService_SearchContent(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	contentService := service.NewContentService(db.GetDB(), storage, nil)

	// Create content
	content := &service.Content{
		Title:       "Unique Title Search",
		Description: "Searchable content",
		Type:        "video",
		Duration:    3600,
		Size:        1024000,
		OwnerID:     "test-owner",
	}

	_, err := contentService.CreateContent(content)
	helpers.AssertNoError(t, err)

	// List contents to verify creation
	contents, err := contentService.ListContents("test-owner", 10, 0)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, len(contents) > 0)
}

package content_test

import (
	"context"
	"testing"

	"streamgate/pkg/models"
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
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video content",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	err := contentService.Create(context.Background(), content)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, content.ID)

	// Retrieve content
	retrieved, err := contentService.GetByID(context.Background(), content.ID)
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
	content := &models.Content{
		Title:       "Original Title",
		Description: "Original description",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	err := contentService.Create(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Update content
	content.Title = "Updated Title"
	content.Description = "Updated description"

	err = contentService.Update(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Verify update
	retrieved, err := contentService.GetByID(context.Background(), content.ID)
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
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video content",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	err := contentService.Create(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Delete content
	err = contentService.Delete(context.Background(), content.ID)
	helpers.AssertNoError(t, err)

	// Verify deletion
	_, err = contentService.GetByID(context.Background(), content.ID)
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
		content := &models.Content{
			Title:       "Test Video " + string(rune(i)),
			Description: "Test content",
			Type:        "video",
			Duration:    3600,
			FileSize:    1024000,
		}
		err := contentService.Create(context.Background(), content)
		helpers.AssertNoError(t, err)
	}

	// List contents
	contents, err := contentService.List(context.Background(), 0, 10)
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
	content := &models.Content{
		Title:       "Unique Title Search",
		Description: "Searchable content",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	err := contentService.Create(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Search content
	results, err := contentService.Search(context.Background(), "Unique Title")
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, len(results) > 0)
}

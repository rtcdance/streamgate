package benchmark_test

import (
	"context"
	"testing"

	"streamgate/pkg/models"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func BenchmarkContentService_Create(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	contentService := service.NewContentService(db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content := &models.Content{
			Title:       "Test Video " + string(rune(i)),
			Description: "A test video",
			Type:        "video",
			Duration:    3600,
			FileSize:    1024000,
		}
		contentService.Create(context.Background(), content)
	}
}

func BenchmarkContentService_GetByID(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	contentService := service.NewContentService(db)

	// Setup: Create content
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}
	contentService.Create(context.Background(), content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		contentService.GetByID(context.Background(), content.ID)
	}
}

func BenchmarkContentService_Update(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	contentService := service.NewContentService(db)

	// Setup: Create content
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}
	contentService.Create(context.Background(), content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content.Title = "Updated Title " + string(rune(i))
		contentService.Update(context.Background(), content)
	}
}

func BenchmarkContentService_List(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	contentService := service.NewContentService(db)

	// Setup: Create multiple contents
	for i := 0; i < 100; i++ {
		content := &models.Content{
			Title:       "Test Video " + string(rune(i)),
			Description: "A test video",
			Type:        "video",
			Duration:    3600,
			FileSize:    1024000,
		}
		contentService.Create(context.Background(), content)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		contentService.List(context.Background(), 0, 10)
	}
}

func BenchmarkContentService_Search(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	contentService := service.NewContentService(db)

	// Setup: Create contents
	for i := 0; i < 100; i++ {
		content := &models.Content{
			Title:       "Test Video " + string(rune(i)),
			Description: "A test video",
			Type:        "video",
			Duration:    3600,
			FileSize:    1024000,
		}
		contentService.Create(context.Background(), content)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		contentService.Search(context.Background(), "Test")
	}
}

func BenchmarkContentService_Delete(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	contentService := service.NewContentService(db)

	// Setup: Create contents
	contentIDs := []string{}
	for i := 0; i < b.N; i++ {
		content := &models.Content{
			Title:       "Test Video " + string(rune(i)),
			Description: "A test video",
			Type:        "video",
			Duration:    3600,
			FileSize:    1024000,
		}
		contentService.Create(context.Background(), content)
		contentIDs = append(contentIDs, content.ID)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i < len(contentIDs) {
			contentService.Delete(context.Background(), contentIDs[i])
		}
	}
}

func BenchmarkContentService_ConcurrentOperations(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	contentService := service.NewContentService(db)

	// Setup: Create content
	content := &models.Content{
		Title:       "Test Video",
		Description: "A test video",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}
	contentService.Create(context.Background(), content)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			contentService.GetByID(context.Background(), content.ID)
		}
	})
}

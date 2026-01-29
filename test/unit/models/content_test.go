package models_test

import (
	"testing"
	"time"

	"streamgate/pkg/models"
	"streamgate/test/helpers"
)

func TestContent_Creation(t *testing.T) {
	content := &models.Content{
		ID:    "content123",
		Title: "Test Video",
		Type:  "video",
		Size:  1024000,
		Status: "ready",
		CreatedAt: time.Now(),
	}

	helpers.AssertEqual(t, "content123", content.ID)
	helpers.AssertEqual(t, "Test Video", content.Title)
	helpers.AssertEqual(t, "video", content.Type)
	helpers.AssertEqual(t, int64(1024000), content.Size)
}

func TestContent_StatusTransition(t *testing.T) {
	content := &models.Content{
		ID:     "content123",
		Title:  "Test Video",
		Status: "pending",
	}

	// Transition to processing
	content.Status = "processing"
	helpers.AssertEqual(t, "processing", content.Status)

	// Transition to ready
	content.Status = "ready"
	helpers.AssertEqual(t, "ready", content.Status)
}

func TestContent_Validation(t *testing.T) {
	tests := []struct {
		name    string
		content *models.Content
		isValid bool
	}{
		{
			"valid content",
			&models.Content{
				ID:    "content123",
				Title: "Test Video",
				Type:  "video",
			},
			true,
		},
		{
			"missing title",
			&models.Content{
				ID:   "content123",
				Type: "video",
			},
			false,
		},
		{
			"missing type",
			&models.Content{
				ID:    "content123",
				Title: "Test Video",
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.content.ID != "" && tt.content.Title != "" && tt.content.Type != ""
			helpers.AssertEqual(t, tt.isValid, isValid)
		})
	}
}

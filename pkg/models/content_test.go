package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContent_Creation(t *testing.T) {
	content := &Content{
		ID:        "content123",
		Title:     "Test Video",
		Type:      "video",
		FileSize:  1024000,
		Status:    "ready",
		CreatedAt: time.Now(),
	}

	assert.Equal(t, "content123", content.ID)
	assert.Equal(t, "Test Video", content.Title)
	assert.Equal(t, "video", content.Type)
	assert.Equal(t, int64(1024000), content.FileSize)
}

func TestContent_StatusTransition(t *testing.T) {
	content := &Content{
		ID:     "content123",
		Title:  "Test Video",
		Status: "pending",
	}

	// Transition to processing
	content.Status = "processing"
	assert.Equal(t, "processing", content.Status)

	// Transition to ready
	content.Status = "ready"
	assert.Equal(t, "ready", content.Status)
}

func TestContent_Validation(t *testing.T) {
	tests := []struct {
		name    string
		content *Content
		isValid bool
	}{
		{
			"valid content",
			&Content{
				ID:    "content123",
				Title: "Test Video",
				Type:  "video",
			},
			true,
		},
		{
			"missing title",
			&Content{
				ID:   "content123",
				Type: "video",
			},
			false,
		},
		{
			"missing type",
			&Content{
				ID:    "content123",
				Title: "Test Video",
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.content.ID != "" && tt.content.Title != "" && tt.content.Type != ""
			assert.Equal(t, tt.isValid, isValid)
		})
	}
}

package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContent_Creation(t *testing.T) {
	now := time.Now()
	content := &Content{
		ID:          "content123",
		Title:       "Test Video",
		Description: "A test video",
		OwnerID:     "user1",
		Type:        "video",
		Status:      "ready",
		FileHash:    "abc123",
		FileSize:    1024000,
		Duration:    120,
		Thumbnail:   "thumb.jpg",
		Tags:        []string{"test", "video"},
		Metadata:    map[string]interface{}{"resolution": "1080p"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	assert.Equal(t, "content123", content.ID)
	assert.Equal(t, "Test Video", content.Title)
	assert.Equal(t, "A test video", content.Description)
	assert.Equal(t, "user1", content.OwnerID)
	assert.Equal(t, "video", content.Type)
	assert.Equal(t, "ready", content.Status)
	assert.Equal(t, "abc123", content.FileHash)
	assert.Equal(t, int64(1024000), content.FileSize)
	assert.Equal(t, int64(120), content.Duration)
	assert.Equal(t, "thumb.jpg", content.Thumbnail)
	assert.Equal(t, []string{"test", "video"}, content.Tags)
	assert.Equal(t, "1080p", content.Metadata["resolution"])
}

func TestContent_ZeroValues(t *testing.T) {
	content := &Content{}

	assert.Equal(t, "", content.ID)
	assert.Equal(t, "", content.Title)
	assert.Equal(t, int64(0), content.FileSize)
	assert.Nil(t, content.Tags)
	assert.Nil(t, content.Metadata)
	assert.True(t, content.CreatedAt.IsZero())
}

func TestContent_JSONMarshaling(t *testing.T) {
	now := time.Now()
	content := &Content{
		ID:        "json-test",
		Title:     "JSON Test",
		Type:      "video",
		Status:    "published",
		FileSize:  2048,
		Tags:      []string{"json"},
		Metadata:  map[string]interface{}{"bitrate": "4000k"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(content)
	assert.NoError(t, err)

	var decoded Content
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, content.ID, decoded.ID)
	assert.Equal(t, content.Title, decoded.Title)
	assert.Equal(t, content.Type, decoded.Type)
	assert.Equal(t, content.Status, decoded.Status)
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
		{
			"missing id",
			&Content{
				Title: "Test Video",
				Type:  "video",
			},
			false,
		},
		{
			"all missing",
			&Content{},
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

func TestContentType_Constants(t *testing.T) {
	assert.Equal(t, ContentType("video"), TypeVideo)
	assert.Equal(t, ContentType("audio"), TypeAudio)
	assert.Equal(t, ContentType("image"), TypeImage)
	assert.Equal(t, ContentType("document"), TypeDocument)
}

func TestContentStatus_Constants(t *testing.T) {
	assert.Equal(t, ContentStatus("draft"), StatusDraft)
	assert.Equal(t, ContentStatus("processing"), StatusProcessing)
	assert.Equal(t, ContentStatus("published"), StatusPublished)
	assert.Equal(t, ContentStatus("archived"), StatusArchived)
	assert.Equal(t, ContentStatus("ready"), StatusReady)
	assert.Equal(t, ContentStatus("failed"), ContentStatusFailed)
}

func TestIsValidContentTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    ContentStatus
		to      ContentStatus
		isValid bool
	}{
		{"draft to processing", StatusDraft, StatusProcessing, true},
		{"draft to archived", StatusDraft, StatusArchived, true},
		{"draft to published", StatusDraft, StatusPublished, false},
		{"processing to ready", StatusProcessing, StatusReady, true},
		{"processing to published", StatusProcessing, StatusPublished, true},
		{"processing to failed", StatusProcessing, ContentStatusFailed, true},
		{"processing to archived", StatusProcessing, StatusArchived, true},
		{"ready to published", StatusReady, StatusPublished, true},
		{"ready to archived", StatusReady, StatusArchived, true},
		{"ready to draft", StatusReady, StatusDraft, false},
		{"published to archived", StatusPublished, StatusArchived, true},
		{"published to draft", StatusPublished, StatusDraft, true},
		{"published to processing", StatusPublished, StatusProcessing, false},
		{"archived to draft", StatusArchived, StatusDraft, true},
		{"archived to published", StatusArchived, StatusPublished, false},
		{"same status", StatusDraft, StatusDraft, true},
		{"same status published", StatusPublished, StatusPublished, true},
		{"failed to draft", ContentStatusFailed, StatusDraft, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidContentTransition(tt.from, tt.to)
			assert.Equal(t, tt.isValid, result)
		})
	}
}

func TestContentMetadata(t *testing.T) {
	meta := ContentMetadata{
		Resolution: "1920x1080",
		Bitrate:    "4000k",
		Codec:      "h264",
		Format:     "mp4",
		Language:   "en",
		Subtitles:  []string{"en", "es"},
	}

	assert.Equal(t, "1920x1080", meta.Resolution)
	assert.Equal(t, "4000k", meta.Bitrate)
	assert.Equal(t, "h264", meta.Codec)
	assert.Equal(t, "mp4", meta.Format)
	assert.Equal(t, "en", meta.Language)
	assert.Equal(t, []string{"en", "es"}, meta.Subtitles)
}

func TestContentMetadata_JSONMarshaling(t *testing.T) {
	meta := ContentMetadata{
		Resolution: "1920x1080",
		Codec:      "h264",
		Subtitles:  []string{"en"},
	}

	data, err := json.Marshal(meta)
	assert.NoError(t, err)

	var decoded ContentMetadata
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, meta.Resolution, decoded.Resolution)
	assert.Equal(t, meta.Codec, decoded.Codec)
}

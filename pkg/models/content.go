package models

import "time"

// Content represents content in the system
type Content struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	OwnerID     string                 `json:"owner_id"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	FileHash    string                 `json:"file_hash"`
	FileSize    int64                  `json:"file_size"`
	Duration    int64                  `json:"duration"`
	Thumbnail   string                 `json:"thumbnail"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// ContentType defines content types
type ContentType string

const (
	TypeVideo    ContentType = "video"
	TypeAudio    ContentType = "audio"
	TypeImage    ContentType = "image"
	TypeDocument ContentType = "document"
)

// ContentStatus defines content status
type ContentStatus string

const (
	StatusDraft      ContentStatus = "draft"
	StatusProcessing ContentStatus = "processing"
	StatusPublished  ContentStatus = "published"
	StatusArchived   ContentStatus = "archived"
)

// ContentMetadata represents content metadata
type ContentMetadata struct {
	Resolution string   `json:"resolution"`
	Bitrate    string   `json:"bitrate"`
	Codec      string   `json:"codec"`
	Format     string   `json:"format"`
	Language   string   `json:"language"`
	Subtitles  []string `json:"subtitles"`
}

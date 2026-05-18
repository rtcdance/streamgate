package content

import (
	"context"
	"time"
)

//go:generate mockgen -destination=mocks/mock_content_service.go -package=mocks streamgate/pkg/service/content ContentService
type ContentService interface {
	GetContent(ctx context.Context, id string) (*Content, error)
	ListContents(ctx context.Context, ownerID string, limit, offset int) ([]*Content, error)
	CreateContent(ctx context.Context, content *Content) (string, error)
	UpdateContent(ctx context.Context, content *Content) error
	DeleteContent(ctx context.Context, id string) error
}

type Content struct {
	ID           string                 `json:"id"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description"`
	Type         string                 `json:"type"`
	URL          string                 `json:"url"`
	ThumbnailURL string                 `json:"thumbnail_url"`
	Duration     int                    `json:"duration"`
	Size         int64                  `json:"size"`
	Status       string                 `json:"status"`
	OwnerID      string                 `json:"owner_id"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Metadata     map[string]interface{} `json:"metadata"`
}

package repository

import (
	"context"

	"streamgate/pkg/service/content"
)

type ContentRepository interface {
	GetContentByID(ctx context.Context, id string) (*content.Content, error)
	ListContent(ctx context.Context, limit, offset int) ([]*content.Content, error)
	CreateContent(ctx context.Context, c *content.Content) error
	UpdateContent(ctx context.Context, c *content.Content) error
	DeleteContent(ctx context.Context, id string) (url string, err error)
}

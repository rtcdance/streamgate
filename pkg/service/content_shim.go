package service

import "github.com/rtcdance/streamgate/pkg/service/content"

type (
	ContentService      = content.ContentService
	Content             = content.Content
	ContentRegistry     = content.ContentRegistry
	ContentObjectStorage = content.ContentObjectStorage
)

var (
	NewContentService = content.NewContentService
)
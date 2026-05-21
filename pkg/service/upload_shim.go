package service

import "github.com/rtcdance/streamgate/pkg/service/upload"

type (
	UploadService    = upload.UploadService
	UploadInfo       = upload.UploadInfo
	ChunkInfo        = upload.ChunkInfo
	PresignedURLer   = upload.PresignedURLer
	UploadObjectStorage = upload.UploadObjectStorage
)

var (
	NewUploadService = upload.NewUploadService
)
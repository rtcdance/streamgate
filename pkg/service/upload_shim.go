package service

import "github.com/rtcdance/streamgate/pkg/service/upload"

type (
	UploadService          = upload.UploadService
	UploadInfo             = upload.UploadInfo
	ChunkInfo              = upload.ChunkInfo
	PresignedURLer         = upload.PresignedURLer
	UploadPresignedURLer   = upload.UploadPresignedURLer
	UploadObjectStorage    = upload.UploadObjectStorage
	AutoTranscodeHookDeps  = upload.AutoTranscodeHookDeps
	PostUploadHook         = upload.PostUploadHook
)

var (
	NewUploadService = upload.NewUploadService
)
package service

import (
	"github.com/rtcdance/streamgate/pkg/service/transcoding"
	"go.uber.org/zap"
)

type (
	TranscodingService    = transcoding.TranscodingService
	TranscodingTask       = transcoding.TranscodingTask
	TranscodingQueue      = transcoding.TranscodingQueue
	VideoTranscoder       = transcoding.VideoTranscoder
	SegmentStorage        = transcoding.SegmentStorage
	PostTranscodeHook     = transcoding.PostTranscodeHook
	TranscodingOption     = transcoding.TranscodingOption
	TranscodingProfile    = transcoding.TranscodingProfile
	MemoryTranscodingQueue = transcoding.MemoryTranscodingQueue
)

var (
	NewTranscodingService    = transcoding.NewTranscodingService
	NewMemoryTranscodingQueue = transcoding.NewMemoryTranscodingQueue
	DefaultProfiles          = transcoding.DefaultProfiles
)

func WithTranscoder(t VideoTranscoder) TranscodingOption {
	return transcoding.WithTranscoder(t)
}

func WithStorage(st SegmentStorage) TranscodingOption {
	return transcoding.WithStorage(st)
}

func WithLogger(l *zap.Logger) TranscodingOption {
	return transcoding.WithLogger(l)
}

package service

import (
	"github.com/rtcdance/streamgate/pkg/service/transcoding"
	"go.uber.org/zap"
)

type (
	TranscodingService     = transcoding.TranscodingService
	TranscodingTask        = transcoding.TranscodingTask
	TranscodingQueue       = transcoding.TranscodingQueue
	VideoTranscoder        = transcoding.VideoTranscoder
	SegmentStorage         = transcoding.SegmentStorage
	PostTranscodeHook      = transcoding.PostTranscodeHook
	TranscodingOption      = transcoding.TranscodingOption
	TranscodingProfile     = transcoding.TranscodingProfile
	MemoryTranscodingQueue = transcoding.MemoryTranscodingQueue
)

var (
	NewTranscodingService     = transcoding.NewTranscodingService
	NewMemoryTranscodingQueue = transcoding.NewMemoryTranscodingQueue
	DefaultProfiles           = transcoding.DefaultProfiles
	// ErrInputTooLarge is returned when an original exceeds the ceiling, so a
	// caller can answer 413 rather than a generic failure.
	ErrInputTooLarge              = transcoding.ErrInputTooLarge
	DefaultMaxTranscodeInputBytes = transcoding.DefaultMaxTranscodeInputBytes
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

// WithMaxInputBytes bounds an ORIGINAL. A non-positive value leaves the default
// in place: "no limit" is not an option this service offers.
func WithMaxInputBytes(n int64) TranscodingOption {
	return transcoding.WithMaxInputBytes(n)
}

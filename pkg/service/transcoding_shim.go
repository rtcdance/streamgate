package service

import (
	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/service/transcoding"
	"go.uber.org/zap"
)

type (
	TranscodingService     = transcoding.TranscodingService
	TranscodingTask        = transcoding.TranscodingTask
	TranscodingQueue       = transcoding.TranscodingQueue
	DeliveryHandle         = models.DeliveryHandle
	VideoTranscoder        = transcoding.VideoTranscoder
	SegmentStorage         = transcoding.SegmentStorage
	PostTranscodeHook      = transcoding.PostTranscodeHook
	TranscodingOption      = transcoding.TranscodingOption
	TranscodingProfile     = transcoding.TranscodingProfile
	MemoryTranscodingQueue = transcoding.MemoryTranscodingQueue
)

var (
	NewTranscodingService              = transcoding.NewTranscodingService
	NewMemoryTranscodingQueue          = transcoding.NewMemoryTranscodingQueue
	NewMemoryTranscodingQueueWithLimit = transcoding.NewMemoryTranscodingQueueWithLimit
	DefaultProfiles                    = transcoding.DefaultProfiles
	// ErrInputTooLarge is returned when an original exceeds the ceiling, so a
	// caller can answer 413 rather than a generic failure.
	ErrInputTooLarge = transcoding.ErrInputTooLarge
	// ErrQueueFull / ErrOwnerTaskLimit mark saturation: callers answer with
	// 503/429 + Retry-After instead of a generic 500.
	ErrQueueFull                  = transcoding.ErrQueueFull
	ErrOwnerTaskLimit             = transcoding.ErrOwnerTaskLimit
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

// WithMinWorkers sets the initial worker pool size.
func WithMinWorkers(n int) TranscodingOption {
	return transcoding.WithMinWorkers(n)
}

// WithMaxWorkers caps the autoscaler's worker pool.
func WithMaxWorkers(n int) TranscodingOption {
	return transcoding.WithMaxWorkers(n)
}

// WithQueueLimit bounds pending tasks at submit time (ErrQueueFull).
func WithQueueLimit(n int) TranscodingOption {
	return transcoding.WithQueueLimit(n)
}

// WithOwnerTaskLimit bounds pending/running tasks per owner (ErrOwnerTaskLimit).
func WithOwnerTaskLimit(n int) TranscodingOption {
	return transcoding.WithOwnerTaskLimit(n)
}

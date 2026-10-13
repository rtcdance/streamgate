package transcoder

import (
	"context"
	"time"

	"github.com/rtcdance/streamgate/pkg/service"

	"go.uber.org/zap"
)

// hlsEngine is the slice of *FFmpegTranscoder the adapter drives. The field is
// an interface (production always passes the concrete transcoder) so tests can
// pin the adapter's mapping without spawning FFmpeg.
type hlsEngine interface {
	TranscodeHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(variant string, progress float64)) error
	TranscodeToHLS(ctx context.Context, inputPath, outputDir string, profiles []TranscodeProfile, callback ProgressCallback, variantProgressFn func(variant string, progress float64)) error
}

// VideoTranscoderAdapter adapts FFmpegTranscoder to service.VideoTranscoder so
// the gateway and the isolated transcoder worker run the SAME profile mapping.
var _ service.VideoTranscoder = (*VideoTranscoderAdapter)(nil)

type VideoTranscoderAdapter struct {
	ft  hlsEngine
	log *zap.Logger
}

// NewVideoTranscoder adapts the FFmpeg engine for service.TranscodingService.
func NewVideoTranscoder(ft *FFmpegTranscoder, log *zap.Logger) *VideoTranscoderAdapter {
	return &VideoTranscoderAdapter{ft: ft, log: log}
}

func (a *VideoTranscoderAdapter) TranscodeHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(variant string, progress float64)) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
	}
	// For ABR, delegate to FFmpegTranscoder which probes source height and
	// selects profiles that do not exceed the input resolution.
	if profile == "abr" || profile == "" {
		return a.ft.TranscodeHLS(ctx, inputPath, outputDir, profile, progressFn)
	}

	profiles := []TranscodeProfile{
		{Resolution: "1920x1080", Bitrate: "5000k", Format: "hls"},
		{Resolution: "1280x720", Bitrate: "2500k", Format: "hls"},
		{Resolution: "854x480", Bitrate: "1000k", Format: "hls"},
		{Resolution: "640x360", Bitrate: "500k", Format: "hls"},
	}

	switch profile {
	case "1080p":
		profiles = []TranscodeProfile{{Resolution: "1920x1080", Bitrate: "5000k", Format: "hls"}}
	case "720p":
		profiles = []TranscodeProfile{{Resolution: "1280x720", Bitrate: "2500k", Format: "hls"}}
	case "480p":
		profiles = []TranscodeProfile{{Resolution: "854x480", Bitrate: "1000k", Format: "hls"}}
	case "360p":
		profiles = []TranscodeProfile{{Resolution: "640x360", Bitrate: "500k", Format: "hls"}}
	}

	callback := func(p *TranscodeProgress) {
		if progressFn != nil && p != nil {
			progressFn("", p.Progress)
		}
	}
	variantFn := func(variant string, progress float64) {
		if progressFn != nil {
			progressFn(variant, progress)
		}
	}
	return a.ft.TranscodeToHLS(ctx, inputPath, outputDir, profiles, callback, variantFn)
}

package upload

import (
	"context"
	"time"

	"streamgate/pkg/plugins/transcoder"
	"streamgate/pkg/service"

	"go.uber.org/zap"
)

type ffmpegAdapter struct {
	ft  *transcoder.FFmpegTranscoder
	log *zap.Logger
}

func (a *ffmpegAdapter) TranscodeToHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(progress float64)) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
	}
	tp := transcoder.TranscodeProfile{Resolution: "1280x720", Bitrate: "2500k", Format: "hls"}
	switch profile {
	case "1080p":
		tp = transcoder.TranscodeProfile{Resolution: "1920x1080", Bitrate: "5000k", Format: "hls"}
	case "720p":
		tp = transcoder.TranscodeProfile{Resolution: "1280x720", Bitrate: "2500k", Format: "hls"}
	case "480p":
		tp = transcoder.TranscodeProfile{Resolution: "854x480", Bitrate: "1000k", Format: "hls"}
	case "360p":
		tp = transcoder.TranscodeProfile{Resolution: "640x360", Bitrate: "500k", Format: "hls"}
	}
	callback := func(p *transcoder.TranscodeProgress) {
		if progressFn != nil && p != nil {
			progressFn(p.Progress)
		}
	}
	return a.ft.TranscodeToHLS(ctx, inputPath, outputDir, []transcoder.TranscodeProfile{tp}, callback)
}

type zapInfoLogger struct {
	*zap.Logger
}

func (l *zapInfoLogger) Info(msg string, fields ...interface{}) {
	l.Logger.Info(msg, zap.Any("fields", fields))
}

var _ service.VideoTranscoder = (*ffmpegAdapter)(nil)

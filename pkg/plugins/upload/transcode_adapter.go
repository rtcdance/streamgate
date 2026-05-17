package upload

import (
	"context"
	"fmt"
	"time"

	"streamgate/pkg/plugins/transcoder"
	"streamgate/pkg/service"

	"go.uber.org/zap"
)

type ffmpegAdapter struct {
	ft  *transcoder.FFmpegTranscoder
	log *zap.Logger
}

var profileDefs = map[string]transcoder.TranscodeProfile{
	"1080p": {Resolution: "1920x1080", Bitrate: "5000k", Format: "hls"},
	"720p":  {Resolution: "1280x720", Bitrate: "2500k", Format: "hls"},
	"480p":  {Resolution: "854x480", Bitrate: "1000k", Format: "hls"},
	"360p":  {Resolution: "640x360", Bitrate: "500k", Format: "hls"},
}

type resolution struct{ w, h int }

var profileRes = map[string]resolution{
	"1080p": {1920, 1080},
	"720p":  {1280, 720},
	"480p":  {854, 480},
	"360p":  {640, 360},
}

func (a *ffmpegAdapter) SelectProfiles(ctx context.Context, inputPath, requestedProfile string) ([]transcoder.TranscodeProfile, error) {
	info, err := a.ft.GetVideoInfo(ctx, inputPath)
	if err != nil {
		a.log.Warn("FFprobe analysis failed, falling back to requested profile",
			zap.String("input", inputPath), zap.Error(err))
		if tp, ok := profileDefs[requestedProfile]; ok {
			return []transcoder.TranscodeProfile{tp}, nil
		}
		return []transcoder.TranscodeProfile{profileDefs["720p"]}, nil
	}

	inputHeight := info.Height
	a.log.Info("Input video analyzed",
		zap.Int("width", info.Width),
		zap.Int("height", inputHeight),
		zap.Float64("fps", info.FrameRate),
		zap.Int("bitrate", info.VideoBitrate))

	var profiles []transcoder.TranscodeProfile
	chainOrder := []string{"1080p", "720p", "480p", "360p"}
	started := false
	for _, name := range chainOrder {
		r := profileRes[name]
		if r.w <= info.Width && r.h <= inputHeight {
			started = true
		}
		if started {
			profiles = append(profiles, profileDefs[name])
		}
	}

	if len(profiles) == 0 {
		a.log.Warn("Input resolution lower than minimum profile, using 360p",
			zap.Int("width", info.Width), zap.Int("height", inputHeight))
		profiles = []transcoder.TranscodeProfile{profileDefs["360p"]}
	}

	return profiles, nil
}

func (a *ffmpegAdapter) TranscodeToHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(progress float64)) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
	}

	profiles, err := a.SelectProfiles(ctx, inputPath, profile)
	if err != nil {
		return fmt.Errorf("failed to select profiles: %w", err)
	}

	callback := func(p *transcoder.TranscodeProgress) {
		if progressFn != nil && p != nil {
			progressFn(p.Progress)
		}
	}
	return a.ft.TranscodeToHLS(ctx, inputPath, outputDir, profiles, callback)
}

type zapInfoLogger struct {
	*zap.Logger
}

func (l *zapInfoLogger) Info(msg string, fields ...interface{}) {
	l.Logger.Info(msg, zap.Any("fields", fields))
}

var _ service.VideoTranscoder = (*ffmpegAdapter)(nil)

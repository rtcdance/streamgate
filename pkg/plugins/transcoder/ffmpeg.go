package transcoder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// FFmpegConfig holds FFmpeg configuration
type FFmpegConfig struct {
	FFmpegPath     string
	FFprobePath    string
	TempDir        string
	MaxRetries     int
	Timeout        time.Duration
	EnableHardware bool
	VideoCodec     string
	AudioCodec     string
	MaxFileSize    int64   // Maximum input file size in bytes (0 = no limit)
	MaxDuration    float64 // Maximum input duration in seconds (0 = default ceiling)
	MaxWidth       int     // Maximum input video width in pixels (0 = default ceiling)
	MaxHeight      int     // Maximum input video height in pixels (0 = default ceiling)
	Threads        int     // ffmpeg -threads value (0 = default)
	ProbeTimeout   time.Duration
}

// Default ceilings for untrusted input. They are applied by NewFFmpegTranscoder
// so a wiring site that forgets them still runs with a bounded pipeline.
const (
	DefaultMaxDurationSeconds = 600.0
	DefaultMaxWidth           = 1920
	DefaultMaxHeight          = 1080
	DefaultFFmpegThreads      = 2
	DefaultProbeTimeout       = 60 * time.Second
)

// probeOutputLimit caps combined ffprobe stdout+stderr so a hostile or broken
// probe target cannot make the server buffer unbounded output.
const probeOutputLimit = 1 << 20 // 1 MiB

// ErrMediaLimitExceeded is returned when an input is refused by the media
// gate: wrong container/codec, or beyond the duration/resolution/size
// ceilings. It is a distinct error so callers can answer with a business
// error instead of a generic 500.
var ErrMediaLimitExceeded = errors.New("input exceeds limit")

// FFmpegTranscoder handles FFmpeg transcoding operations
type FFmpegTranscoder struct {
	config *FFmpegConfig
	logger *zap.Logger
}

// VideoInfo contains video file information
type VideoInfo struct {
	Duration     float64
	Width        int
	Height       int
	VideoCodec   string
	AudioCodec   string
	VideoBitrate int
	AudioBitrate int
	FrameRate    float64
	TotalFrames  int64
	FileSize     int64
	Format       string
}

// TranscodeProgress represents transcoding progress
type TranscodeProgress struct {
	Progress       float64
	CurrentBitrate string
	Speed          string
	Frame          int64
	FPS            float64
	Processed      time.Duration
	Remaining      time.Duration
}

// ProgressCallback is called during transcoding to report progress
type ProgressCallback func(progress *TranscodeProgress)

// NewFFmpegTranscoder creates a new FFmpeg transcoder
func NewFFmpegTranscoder(config *FFmpegConfig, logger *zap.Logger) *FFmpegTranscoder {
	if config.FFmpegPath == "" {
		config.FFmpegPath = "ffmpeg"
	}
	if config.FFprobePath == "" {
		config.FFprobePath = "ffprobe"
	}
	if config.TempDir == "" {
		config.TempDir = "/tmp/streamgate"
	}
	// Untrusted input gets bounded defaults rather than "no limit": a wiring
	// site that forgets a ceiling must not inherit an unbounded pipeline.
	if config.MaxDuration <= 0 {
		config.MaxDuration = DefaultMaxDurationSeconds
	}
	if config.MaxWidth <= 0 {
		config.MaxWidth = DefaultMaxWidth
	}
	if config.MaxHeight <= 0 {
		config.MaxHeight = DefaultMaxHeight
	}
	if config.Threads <= 0 {
		config.Threads = DefaultFFmpegThreads
	}
	if config.ProbeTimeout <= 0 {
		config.ProbeTimeout = DefaultProbeTimeout
	}

	return &FFmpegTranscoder{
		config: config,
		logger: logger,
	}
}

type ffprobeFormat struct {
	Duration   string `json:"duration"`
	Size       string `json:"size"`
	BitRate    string `json:"bit_rate"`
	FormatName string `json:"format_name"`
}

type ffprobeStream struct {
	CodecName  string `json:"codec_name"`
	CodecType  string `json:"codec_type"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RFrameRate string `json:"r_frame_rate"`
	NBFrames   string `json:"nb_frames"`
	BitRate    string `json:"bit_rate"`
}

type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

// cappedBuffer accumulates output up to limit bytes and reports truncation
// instead of growing without bound. Writes always report the full length so
// the exec pipe copier is never told to abort mid-stream.
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if remain := b.limit - int64(b.buf.Len()); remain > 0 {
		if int64(len(p)) > remain {
			_, _ = b.buf.Write(p[:remain])
			b.truncated = true
		} else {
			_, _ = b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *cappedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// tailBuffer retains only the last limit bytes written to it, so an error
// message can quote recent FFmpeg stderr without holding the whole stream.
type tailBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(p) >= t.limit {
		t.buf = append(t.buf[:0], p[len(p)-t.limit:]...)
		return len(p), nil
	}
	if overflow := len(t.buf) + len(p) - t.limit; overflow > 0 {
		t.buf = t.buf[overflow:]
	}
	t.buf = append(t.buf, p...)
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

func truncateForError(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[len(s)-maxLen:] + "…"
}

// GetVideoInfo retrieves video file information using ffprobe
func (ft *FFmpegTranscoder) GetVideoInfo(ctx context.Context, inputPath string) (*VideoInfo, error) {
	// ffprobe gets its OWN deadline: the caller context may carry a long
	// transcode budget (or none at all), and a hung probe must not inherit it.
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ft.config.ProbeTimeout)
	defer cancel()

	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	}

	cmd := exec.CommandContext(probeCtx, ft.config.FFprobePath, args...)
	configureProcGroup(cmd)
	cmd.Cancel = func() error { return killProcGroup(cmd) }
	cmd.WaitDelay = 5 * time.Second

	out := &cappedBuffer{limit: probeOutputLimit}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffprobe: %w", err)
	}
	err := cmd.Wait()
	if err != nil && probeCtx.Err() != nil {
		return nil, fmt.Errorf("ffprobe timed out after %s: %w", ft.config.ProbeTimeout, probeCtx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w, output: %s", err, truncateForError(out.String(), 4096))
	}
	if out.Truncated() {
		return nil, fmt.Errorf("ffprobe output exceeded the %d-byte cap", probeOutputLimit)
	}

	var probe ffprobeOutput
	if err := json.Unmarshal([]byte(out.String()), &probe); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	info := &VideoInfo{Format: probe.Format.FormatName}

	if probe.Format.Duration != "" {
		if d, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			info.Duration = d
		}
	}
	if probe.Format.Size != "" {
		if size, err := strconv.ParseInt(probe.Format.Size, 10, 64); err == nil {
			info.FileSize = size
		}
	}

	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			if info.VideoCodec == "" {
				info.VideoCodec = stream.CodecName
				info.Width = stream.Width
				info.Height = stream.Height
				if stream.BitRate != "" {
					if br, err := strconv.Atoi(stream.BitRate); err == nil {
						info.VideoBitrate = br
					}
				}
				if stream.RFrameRate != "" {
					parts := strings.Split(stream.RFrameRate, "/")
					if len(parts) == 2 {
						if num, err := strconv.Atoi(parts[0]); err == nil {
							if den, err := strconv.Atoi(parts[1]); err == nil && den > 0 {
								info.FrameRate = float64(num) / float64(den)
							}
						}
					}
				}
				if stream.NBFrames != "" {
					if frames, err := strconv.ParseInt(stream.NBFrames, 10, 64); err == nil {
						info.TotalFrames = frames
					}
				}
			}
		case "audio":
			if info.AudioCodec == "" {
				info.AudioCodec = stream.CodecName
				if stream.BitRate != "" {
					if br, err := strconv.Atoi(stream.BitRate); err == nil {
						info.AudioBitrate = br
					}
				}
			}
		}
	}

	if info.Duration == 0 {
		return nil, fmt.Errorf("failed to extract video duration")
	}

	return info, nil
}

// allowedContainer reports whether ffprobe's format_name belongs to the
// mp4/mov family. ffprobe reports e.g. "mov,mp4,m4a,3gp,3g2,mj2" for MP4.
func allowedContainer(formatName string) bool {
	return strings.Contains(formatName, "mp4") || strings.Contains(formatName, "mov")
}

// ValidateMediaFile validates that an input file is a playable media file
// within configured size and duration limits. Returns VideoInfo on success.
// For HTTP/HTTPS URLs, the os.Stat check is skipped and ffprobe is used
// directly — FFmpeg natively supports remote input.
//
// The gate is allowlist-based and fails closed: only mp4/mov container with
// H.264 video (and AAC audio when an audio stream is present) passes; anything
// else is refused with ErrMediaLimitExceeded.
func (ft *FFmpegTranscoder) ValidateMediaFile(ctx context.Context, inputPath string) (*VideoInfo, error) {
	// Skip os.Stat for HTTP URLs — FFmpeg can access them directly.
	if !strings.HasPrefix(inputPath, "http://") && !strings.HasPrefix(inputPath, "https://") {
		stat, err := os.Stat(inputPath)
		if err != nil {
			return nil, fmt.Errorf("cannot stat input file: %w", err)
		}

		if ft.config.MaxFileSize > 0 && stat.Size() > ft.config.MaxFileSize {
			return nil, fmt.Errorf("%w: input file size %d exceeds maximum %d bytes", ErrMediaLimitExceeded, stat.Size(), ft.config.MaxFileSize)
		}
	}

	// Use ffprobe to validate the file is a playable media file
	info, err := ft.GetVideoInfo(ctx, inputPath)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("media probe failed: %w", err)
		}
		return nil, fmt.Errorf("%w: not a valid media file: %v", ErrMediaLimitExceeded, err)
	}

	if !allowedContainer(info.Format) {
		return nil, fmt.Errorf("%w: container %q is not supported (want mp4/mov family)", ErrMediaLimitExceeded, info.Format)
	}

	if info.VideoCodec == "" {
		return nil, fmt.Errorf("%w: no video stream found", ErrMediaLimitExceeded)
	}
	if info.VideoCodec != "h264" {
		return nil, fmt.Errorf("%w: video codec %q is not supported (want h264)", ErrMediaLimitExceeded, info.VideoCodec)
	}
	if info.AudioCodec != "" && info.AudioCodec != "aac" {
		return nil, fmt.Errorf("%w: audio codec %q is not supported (want aac)", ErrMediaLimitExceeded, info.AudioCodec)
	}

	if info.Duration == 0 {
		return nil, fmt.Errorf("input file has zero duration (possibly corrupted)")
	}

	if ft.config.MaxDuration > 0 && info.Duration > ft.config.MaxDuration {
		return nil, fmt.Errorf("%w: input duration %.1fs exceeds maximum %.1fs", ErrMediaLimitExceeded, info.Duration, ft.config.MaxDuration)
	}

	if (ft.config.MaxWidth > 0 && info.Width > ft.config.MaxWidth) ||
		(ft.config.MaxHeight > 0 && info.Height > ft.config.MaxHeight) {
		return nil, fmt.Errorf("%w: resolution %dx%d exceeds maximum %dx%d",
			ErrMediaLimitExceeded, info.Width, info.Height, ft.config.MaxWidth, ft.config.MaxHeight)
	}

	ft.logger.Debug("Media file validated",
		zap.String("path", inputPath),
		zap.String("format", info.Format),
		zap.Float64("duration", info.Duration),
		zap.Int64("size", info.FileSize),
		zap.Int("width", info.Width),
		zap.Int("height", info.Height))

	return info, nil
}

// Transcode transcodes video to specified format
func (ft *FFmpegTranscoder) Transcode(ctx context.Context, inputPath, outputPath string, profile TranscodeProfile, callback ProgressCallback) error {
	videoCodec := ft.config.VideoCodec
	if videoCodec == "" {
		videoCodec = "libx264"
	}

	audioCodec := ft.config.AudioCodec
	if audioCodec == "" {
		audioCodec = "aac"
	}

	args := []string{
		"-i", inputPath,
		"-c:v", videoCodec,
		"-preset", "ultrafast",
		"-crf", "28",
		"-c:a", audioCodec,
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y",
		outputPath,
	}

	return ft.runFFmpeg(ctx, args, 0, callback)
}

// TranscodeToHLS transcodes video to HLS format with multiple quality levels.
// It validates the input file before transcoding and cleans up partial outputs on failure.
func (ft *FFmpegTranscoder) TranscodeToHLS(ctx context.Context, inputPath, outputDir string, profiles []TranscodeProfile, callback ProgressCallback, variantProgressFn func(variant string, progress float64)) error {
	info, err := ft.ValidateMediaFile(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("input validation failed: %w", err)
	}

	totalDuration := time.Duration(info.Duration * float64(time.Second))

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	var firstErr error
	for _, profile := range profiles {
		outputPath := filepath.Join(outputDir, fmt.Sprintf("%s.m3u8", profile.Resolution))
		variantCB := callback
		if variantProgressFn != nil {
			p := profile
			variantCB = func(pg *TranscodeProgress) {
				if callback != nil {
					callback(pg)
				}
				variantProgressFn(p.Resolution, pg.Progress)
			}
		}
		if err := ft.transcodeToHLSVariant(ctx, inputPath, outputPath, profile, totalDuration, variantCB); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to transcode to %s: %w", profile.Resolution, err)
			}
		}
	}

	if firstErr != nil {
		// Clean up partial outputs on failure
		ft.cleanupPartialOutput(outputDir)
		return firstErr
	}

	return ft.generateHLSMasterPlaylist(outputDir, profiles)
}

func selectABRProfiles(sourceHeight int) []TranscodeProfile {
	profiles := make([]TranscodeProfile, 0, 4)
	for _, name := range []string{"1080p", "720p", "480p", "360p"} {
		if p, ok := defaultProfileMap[name]; ok {
			if sourceHeight > 0 {
				if ph := parseProfileHeight(p.Resolution); ph > sourceHeight {
					continue
				}
			}
			profiles = append(profiles, p)
		}
	}
	if len(profiles) == 0 {
		if p, ok := defaultProfileMap["360p"]; ok {
			profiles = append(profiles, p)
		}
	}
	return profiles
}

func parseProfileHeight(resolution string) int {
	parts := strings.Split(resolution, "x")
	if len(parts) != 2 {
		return 0
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return h
}

func (ft *FFmpegTranscoder) probeSourceHeight(ctx context.Context, inputPath string) int {
	info, err := ft.GetVideoInfo(ctx, inputPath)
	if err != nil || info == nil {
		if ft.logger != nil {
			ft.logger.Warn("probeSourceHeight failed, skipping upscaling filter", zap.String("input", inputPath), zap.Error(err))
		}
		return 0
	}
	if ft.logger != nil {
		ft.logger.Info("probeSourceHeight", zap.String("input", inputPath), zap.Int("height", info.Height))
	}
	return info.Height
}

var defaultProfileMap = map[string]TranscodeProfile{
	"1080p": {Resolution: "1920x1080", Bitrate: "5000k", Format: "hls"},
	"720p":  {Resolution: "1280x720", Bitrate: "2500k", Format: "hls"},
	"480p":  {Resolution: "854x480", Bitrate: "1000k", Format: "hls"},
	"360p":  {Resolution: "640x360", Bitrate: "500k", Format: "hls"},
}

// TranscodeHLS transcodes video to HLS for a profile name (e.g. "720p").
// Profile "abr" or "" transcodes to all 4 predefined resolutions (1080p/720p/480p/360p).
// This method satisfies the service.VideoTranscoder interface.
func (ft *FFmpegTranscoder) TranscodeHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(variant string, progress float64)) error {
	if profile == "abr" || profile == "" {
		sourceHeight := ft.probeSourceHeight(ctx, inputPath)
		profiles := selectABRProfiles(sourceHeight)
		return ft.TranscodeToHLS(ctx, inputPath, outputDir, profiles, func(pg *TranscodeProgress) {
			if progressFn != nil {
				progressFn("", pg.Progress)
			}
		}, progressFn)
	}
	p, ok := defaultProfileMap[profile]
	if !ok {
		p = defaultProfileMap["720p"]
	}
	return ft.TranscodeToHLS(ctx, inputPath, outputDir, []TranscodeProfile{p}, func(pg *TranscodeProgress) {
		if progressFn != nil {
			progressFn(profile, pg.Progress)
		}
	}, nil)
}

// cleanupPartialOutput removes .ts and .m3u8 files from a failed transcode attempt
func (ft *FFmpegTranscoder) cleanupPartialOutput(outputDir string) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".m3u8") {
			if err := os.Remove(filepath.Join(outputDir, name)); err != nil {
				ft.logger.Warn("Failed to clean up partial output", zap.String("file", name), zap.Error(err))
			}
		}
	}
}

// transcodeToHLSVariant transcodes a single HLS variant
func (ft *FFmpegTranscoder) transcodeToHLSVariant(ctx context.Context, inputPath, outputPath string, profile TranscodeProfile, totalDuration time.Duration, callback ProgressCallback) error {
	videoCodec := ft.config.VideoCodec
	if videoCodec == "" {
		videoCodec = "libx264"
	}

	audioCodec := ft.config.AudioCodec
	if audioCodec == "" {
		audioCodec = "aac"
	}

	args := []string{
		"-i", inputPath,
		"-c:v", videoCodec,
		"-preset", "ultrafast",
		"-crf", "28",
		"-vf", fmt.Sprintf("scale=%s", profile.Resolution),
		"-b:v", profile.Bitrate,
		"-maxrate", profile.Bitrate,
		"-bufsize", fmt.Sprintf("%dk", parseBitrate(profile.Bitrate)*2),
		"-c:a", audioCodec,
		"-b:a", "128k",
		"-ac", "2",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", fmt.Sprintf("%s_%%03d.ts", outputPath[:len(outputPath)-5]),
		"-y",
		outputPath,
	}

	return ft.runFFmpeg(ctx, args, totalDuration, callback)
}

// generateHLSMasterPlaylist generates the HLS master playlist
func (ft *FFmpegTranscoder) generateHLSMasterPlaylist(outputDir string, profiles []TranscodeProfile) error {
	masterPath := filepath.Join(outputDir, "master.m3u8")

	var builder strings.Builder
	builder.WriteString("#EXTM3U\n")
	builder.WriteString("#EXT-X-VERSION:3\n\n")

	for _, profile := range profiles {
		variantPath := fmt.Sprintf("%s.m3u8", profile.Resolution)
		bandwidth := parseBitrate(profile.Bitrate) * 1000

		fmt.Fprintf(&builder, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bandwidth, profile.Resolution)
		fmt.Fprintf(&builder, "%s\n", variantPath)
	}

	return os.WriteFile(masterPath, []byte(builder.String()), 0o644)
}

// ExtractThumbnail extracts a thumbnail from video
func (ft *FFmpegTranscoder) ExtractThumbnail(ctx context.Context, inputPath, outputPath, timestamp string) error {
	args := []string{
		"-ss", timestamp,
		"-i", inputPath,
		"-vframes", "1",
		"-q:v", "2",
		"-y",
		outputPath,
	}

	return ft.runFFmpeg(ctx, args, 0, nil)
}

// runFFmpeg executes FFmpeg command with progress monitoring.
// Args are passed to exec as an argv array — never through a shell.
func (ft *FFmpegTranscoder) runFFmpeg(ctx context.Context, args []string, totalDuration time.Duration, callback ProgressCallback) error {
	if ft.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ft.config.Timeout)
		defer cancel()
	}

	if ft.config.Threads > 0 && len(args) > 0 {
		// -threads is an output option: insert it immediately before the output
		// path (the last argument) so it binds to the encoder.
		output := args[len(args)-1]
		args = append(args[:len(args)-1:len(args)], "-threads", strconv.Itoa(ft.config.Threads), output)
	}

	cmd := exec.CommandContext(ctx, ft.config.FFmpegPath, args...)
	// FFmpeg spawns child processes (muxers/workers); cancelling must signal the
	// whole process group, not just the direct child, so nothing survives the
	// timeout.
	configureProcGroup(cmd)
	cmd.Cancel = func() error { return killProcGroup(cmd) }
	cmd.WaitDelay = 10 * time.Second

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	// Retain only the tail of stderr so a failure can quote recent output
	// without holding the whole stream in memory or in the error message.
	stderrTail := newTailBuffer(8 << 10)
	src := io.TeeReader(stderr, stderrTail)

	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		if callback != nil {
			ft.monitorProgress(src, totalDuration, callback)
		} else {
			_, _ = io.Copy(io.Discard, src)
		}
	}()

	<-progressDone

	if err := cmd.Wait(); err != nil {
		if tail := stderrTail.String(); tail != "" {
			return fmt.Errorf("FFmpeg process failed: %w (stderr tail: %s)", err, truncateForError(tail, 8<<10))
		}
		return fmt.Errorf("FFmpeg process failed: %w", err)
	}

	return nil
}

var progressRegex = regexp.MustCompile(`frame=\s*(\d+)\s+fps=\s*([\d.]+)\s+q=\s*([-.\d]+)\s+L?size=\s*(\S+)\s+time=\s*([\d:.]+)\s+bitrate=\s*(\S+)\s+speed=\s*([\d.]+)x`)

// monitorProgress monitors FFmpeg progress output
func (ft *FFmpegTranscoder) monitorProgress(stderrPipe io.Reader, totalDuration time.Duration, callback ProgressCallback) {
	buf := make([]byte, 4096)
	var lineBuf []byte
	var totalLines int

	for {
		n, err := stderrPipe.Read(buf)
		if n > 0 {
			lineBuf = append(lineBuf, buf[:n]...)
			for {
				idx := bytes.IndexAny(lineBuf, "\r\n")
				if idx < 0 {
					break
				}
				line := string(lineBuf[:idx])
				lineBuf = lineBuf[idx+1:]
				if len(lineBuf) > 0 && lineBuf[0] == '\n' {
					lineBuf = lineBuf[1:]
				}
				if line == "" {
					continue
				}

				totalLines++
				matches := progressRegex.FindStringSubmatch(line)
				if len(matches) >= 8 {
					frame, _ := strconv.ParseInt(matches[1], 10, 64)
					fps, _ := strconv.ParseFloat(matches[2], 64)
					bitrate := matches[6]
					speed := matches[7]

					timeStr := matches[5]
					processed := parseTime(timeStr)

					progress := &TranscodeProgress{
						Frame:          frame,
						FPS:            fps,
						CurrentBitrate: bitrate,
						Speed:          speed,
						Processed:      processed,
					}

					if totalDuration > 0 {
						pct := float64(processed) / float64(totalDuration) * 100
						if pct > 99 {
							pct = 99
						}
						progress.Progress = pct
						if processed < totalDuration {
							progress.Remaining = totalDuration - processed
						}
						ft.logger.Debug("ffmpeg progress",
							zap.Duration("processed", processed),
							zap.Duration("total", totalDuration),
							zap.Float64("pct", pct))
					}

					callback(progress)
				} else if totalLines <= 3 {
					if strings.HasPrefix(line, "ffmpeg version") || strings.HasPrefix(line, "  ") {
						continue
					}
					ft.logger.Debug("ffmpeg stderr line (no regex match)",
						zap.Int("line_num", totalLines),
						zap.String("line", line))
				}
			}
		}
		if err != nil {
			ft.logger.Debug("ffmpeg monitor done",
				zap.Int("total_lines", totalLines),
				zap.Duration("total_duration", totalDuration),
				zap.Error(err))
			break
		}
	}
}

// parseTime parses time string in format HH:MM:SS.mmm
func parseTime(timeStr string) time.Duration {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return 0
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil || hours < 0 {
		return 0
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil || minutes < 0 || minutes > 59 {
		return 0
	}
	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil || seconds < 0 || seconds > 60 {
		return 0
	}

	duration := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds*1000)*time.Millisecond

	return duration
}

// parseBitrate parses bitrate string (e.g., "5000k" -> 5000)
func parseBitrate(bitrate string) int {
	bitrate = strings.ToLower(strings.TrimSpace(bitrate))
	if strings.HasSuffix(bitrate, "kbps") {
		bitrate = strings.TrimSuffix(bitrate, "kbps")
	} else if strings.HasSuffix(bitrate, "k") {
		bitrate = strings.TrimSuffix(bitrate, "k")
	} else if strings.HasSuffix(bitrate, "mbps") {
		bitrate = strings.TrimSuffix(bitrate, "mbps")
		val, err := strconv.Atoi(bitrate)
		if err != nil {
			return 0
		}
		return val * 1000
	} else if strings.HasSuffix(bitrate, "m") {
		bitrate = strings.TrimSuffix(bitrate, "m")
		val, err := strconv.Atoi(bitrate)
		if err != nil {
			return 0
		}
		return val * 1000
	}

	val, err := strconv.Atoi(bitrate)
	if err != nil {
		return 0
	}
	return val
}

// CleanupTempFiles cleans up temporary files
func (ft *FFmpegTranscoder) CleanupTempFiles() error {
	if _, err := os.Stat(ft.config.TempDir); os.IsNotExist(err) {
		return nil
	}

	return os.RemoveAll(ft.config.TempDir)
}

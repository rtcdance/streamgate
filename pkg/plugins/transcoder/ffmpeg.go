package transcoder

import (
	"bufio"
	"context"
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
	FFmpegPath      string
	FFprobePath     string
	TempDir         string
	MaxRetries      int
	Timeout         time.Duration
	EnableHardware  bool
	VideoCodec      string
	AudioCodec      string
	MaxFileSize     int64 // Maximum input file size in bytes (0 = no limit)
	MaxDuration     float64 // Maximum input duration in seconds (0 = no limit)
}

// FFmpegTranscoder handles FFmpeg transcoding operations
type FFmpegTranscoder struct {
	config *FFmpegConfig
	logger *zap.Logger
	mu     sync.RWMutex //nolint:unused
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

	return &FFmpegTranscoder{
		config: config,
		logger: logger,
	}
}

// GetVideoInfo retrieves video file information using ffprobe
func (ft *FFmpegTranscoder) GetVideoInfo(ctx context.Context, inputPath string) (*VideoInfo, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	}

	cmd := exec.CommandContext(ctx, ft.config.FFprobePath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w, output: %s", err, string(output))
	}

	info := &VideoInfo{}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, `"duration"`) {
			re := regexp.MustCompile(`"duration":\s*"([\d.]+)"`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				if d, err := strconv.ParseFloat(matches[1], 64); err == nil {
					info.Duration = d
				}
			}
		}

		if strings.Contains(line, `"width"`) {
			re := regexp.MustCompile(`"width":\s*(\d+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				if w, err := strconv.Atoi(matches[1]); err == nil {
					info.Width = w
				}
			}
		}

		if strings.Contains(line, `"height"`) {
			re := regexp.MustCompile(`"height":\s*(\d+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				if h, err := strconv.Atoi(matches[1]); err == nil {
					info.Height = h
				}
			}
		}

		if strings.Contains(line, `"codec_name"`) && strings.Contains(line, `"codec_type":\s*"video"`) {
			re := regexp.MustCompile(`"codec_name":\s*"([^"]+)"`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				info.VideoCodec = matches[1]
			}
		}

		if strings.Contains(line, `"codec_name"`) && strings.Contains(line, `"codec_type":\s*"audio"`) {
			re := regexp.MustCompile(`"codec_name":\s*"([^"]+)"`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				info.AudioCodec = matches[1]
			}
		}

		if strings.Contains(line, `"bit_rate"`) {
			re := regexp.MustCompile(`"bit_rate":\s*"(\d+)"`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				if br, err := strconv.Atoi(matches[1]); err == nil {
					if info.VideoBitrate == 0 {
						info.VideoBitrate = br
					} else {
						info.AudioBitrate = br
					}
				}
			}
		}

		if strings.Contains(line, `"r_frame_rate"`) {
			re := regexp.MustCompile(`"r_frame_rate":\s*"(\d+)/(\d+)"`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 2 {
				if num, err := strconv.Atoi(matches[1]); err == nil {
					if den, err := strconv.Atoi(matches[2]); err == nil && den > 0 {
						info.FrameRate = float64(num) / float64(den)
					}
				}
			}
		}

		if strings.Contains(line, `"nb_frames"`) {
			re := regexp.MustCompile(`"nb_frames":\s*"(\d+)"`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				if frames, err := strconv.ParseInt(matches[1], 10, 64); err == nil {
					info.TotalFrames = frames
				}
			}
		}

		if strings.Contains(line, `"size"`) {
			re := regexp.MustCompile(`"size":\s*"(\d+)"`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				if size, err := strconv.ParseInt(matches[1], 10, 64); err == nil {
					info.FileSize = size
				}
			}
		}
	}

	if info.Duration == 0 {
		return nil, fmt.Errorf("failed to extract video duration")
	}

	return info, nil
}

// ValidateMediaFile validates that an input file is a playable media file
// within configured size and duration limits. Returns VideoInfo on success.
func (ft *FFmpegTranscoder) ValidateMediaFile(ctx context.Context, inputPath string) (*VideoInfo, error) {
	// Check file exists and get size
	stat, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("cannot stat input file: %w", err)
	}

	if ft.config.MaxFileSize > 0 && stat.Size() > ft.config.MaxFileSize {
		return nil, fmt.Errorf("input file size %d exceeds maximum %d bytes", stat.Size(), ft.config.MaxFileSize)
	}

	// Use ffprobe to validate the file is a playable media file
	info, err := ft.GetVideoInfo(ctx, inputPath)
	if err != nil {
		return nil, fmt.Errorf("input file is not a valid media file: %w", err)
	}

	if info.Duration == 0 {
		return nil, fmt.Errorf("input file has zero duration (possibly corrupted)")
	}

	if ft.config.MaxDuration > 0 && info.Duration > ft.config.MaxDuration {
		return nil, fmt.Errorf("input duration %.1fs exceeds maximum %.1fs", info.Duration, ft.config.MaxDuration)
	}

	ft.logger.Debug("Media file validated",
		zap.String("path", inputPath),
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
		"-preset", "medium",
		"-crf", "23",
		"-c:a", audioCodec,
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y",
		outputPath,
	}

	return ft.runFFmpeg(ctx, args, callback)
}

// TranscodeToHLS transcodes video to HLS format with multiple quality levels.
// It validates the input file before transcoding and cleans up partial outputs on failure.
func (ft *FFmpegTranscoder) TranscodeToHLS(ctx context.Context, inputPath, outputDir string, profiles []TranscodeProfile, callback ProgressCallback) error {
	// Validate input before starting expensive transcoding
	if _, err := ft.ValidateMediaFile(ctx, inputPath); err != nil {
		return fmt.Errorf("input validation failed: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(profiles))

	for _, profile := range profiles {
		wg.Add(1)
		go func(p TranscodeProfile) {
			defer wg.Done()

			outputPath := filepath.Join(outputDir, fmt.Sprintf("%s.m3u8", p.Resolution))
			if err := ft.transcodeToHLSVariant(ctx, inputPath, outputPath, p, callback); err != nil {
				errChan <- fmt.Errorf("failed to transcode to %s: %w", p.Resolution, err)
			}
		}(profile)
	}

	wg.Wait()
	close(errChan)

	var firstErr error
	for err := range errChan {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		// Clean up partial outputs on failure
		ft.cleanupPartialOutput(outputDir)
		return firstErr
	}

	return ft.generateHLSMasterPlaylist(outputDir, profiles)
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
func (ft *FFmpegTranscoder) transcodeToHLSVariant(ctx context.Context, inputPath, outputPath string, profile TranscodeProfile, callback ProgressCallback) error {
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
		"-preset", "medium",
		"-crf", "23",
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

	return ft.runFFmpeg(ctx, args, callback)
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

		builder.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bandwidth, profile.Resolution))
		builder.WriteString(fmt.Sprintf("%s\n", variantPath))
	}

	return os.WriteFile(masterPath, []byte(builder.String()), 0o644)
}

// TranscodeToDASH transcodes video to DASH format with multiple quality levels
func (ft *FFmpegTranscoder) TranscodeToDASH(ctx context.Context, inputPath, outputDir string, profiles []TranscodeProfile, callback ProgressCallback) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

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
		"-preset", "medium",
		"-crf", "23",
		"-c:a", audioCodec,
		"-b:a", "128k",
		"-ac", "2",
		"-f", "dash",
		"-seg_duration", "6",
		"-use_template", "1",
		"-use_timeline", "1",
		"-init_seg_name", "init-$RepresentationID$.m4s",
		"-media_seg_name", "chunk-$RepresentationID$-$Number%05d$.m4s",
		"-y",
		filepath.Join(outputDir, "manifest.mpd"),
	}

	return ft.runFFmpeg(ctx, args, callback)
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

	return ft.runFFmpeg(ctx, args, nil)
}

// ExtractAudio extracts audio track from video
func (ft *FFmpegTranscoder) ExtractAudio(ctx context.Context, inputPath, outputPath string) error {
	args := []string{
		"-i", inputPath,
		"-vn",
		"-acodec", "copy",
		"-y",
		outputPath,
	}

	return ft.runFFmpeg(ctx, args, nil)
}

// ConcatVideos concatenates multiple videos
func (ft *FFmpegTranscoder) ConcatVideos(ctx context.Context, inputPaths []string, outputPath string, callback ProgressCallback) error {
	listFile := filepath.Join(ft.config.TempDir, fmt.Sprintf("concat_%d.txt", time.Now().UnixNano()))
	defer func() { _ = os.Remove(listFile) }()

	var listContent strings.Builder
	for _, path := range inputPaths {
		listContent.WriteString(fmt.Sprintf("file '%s'\n", path))
	}

	if err := os.WriteFile(listFile, []byte(listContent.String()), 0o644); err != nil {
		return fmt.Errorf("failed to create concat list: %w", err)
	}

	args := []string{
		"-f", "concat",
		"-safe", "0",
		"-i", listFile,
		"-c", "copy",
		"-y",
		outputPath,
	}

	return ft.runFFmpeg(ctx, args, callback)
}

// runFFmpeg executes FFmpeg command with progress monitoring
func (ft *FFmpegTranscoder) runFFmpeg(ctx context.Context, args []string, callback ProgressCallback) error {
	cmd := exec.CommandContext(ctx, ft.config.FFmpegPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	if callback != nil {
		go ft.monitorProgress(stderr, callback)
	}

	if _, err := cmd.Process.Wait(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return fmt.Errorf("FFmpeg process failed: %w", err)
	}

	_ = stdout.Close()
	_ = stderr.Close()

	return nil
}

// monitorProgress monitors FFmpeg progress output
func (ft *FFmpegTranscoder) monitorProgress(stderrPipe io.Reader, callback ProgressCallback) {
	scanner := bufio.NewScanner(stderrPipe)

	progressRegex := regexp.MustCompile(`frame=\s*(\d+)\s+fps=\s*([\d.]+)\s+q=\s*([\d.]+)\s+size=\s*(\d+)\s+time=\s*([\d:]+)\s+bitrate=\s*([\d.]+)kbits/s\s+speed=\s*([\d.]+)x`)

	for scanner.Scan() {
		line := scanner.Text()
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

			callback(progress)
		}
	}
}

// parseTime parses time string in format HH:MM:SS.mmm
func parseTime(timeStr string) time.Duration {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return 0
	}

	hours, _ := strconv.Atoi(parts[0])
	minutes, _ := strconv.Atoi(parts[1])
	seconds, _ := strconv.ParseFloat(parts[2], 64)

	duration := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds*1000)*time.Millisecond

	return duration
}

// parseBitrate parses bitrate string (e.g., "5000k" -> 5000)
func parseBitrate(bitrate string) int {
	bitrate = strings.ToLower(bitrate)
	bitrate = strings.TrimSuffix(bitrate, "k")
	bitrate = strings.TrimSuffix(bitrate, "kbps")
	bitrate = strings.TrimSuffix(bitrate, "m")

	val, err := strconv.Atoi(bitrate)
	if err != nil {
		return 0
	}

	if strings.Contains(bitrate, "m") {
		return val * 1000
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

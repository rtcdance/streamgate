package transcoder

// FFmpegTranscoder handles FFmpeg transcoding
type FFmpegTranscoder struct{}

// Transcode transcodes video
func (t *FFmpegTranscoder) Transcode(inputPath, outputPath, profile string) error {
	return nil
}

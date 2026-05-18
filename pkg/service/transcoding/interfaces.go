package transcoding

import (
	"context"
	"io"
)

//go:generate mockgen -destination=mocks/mock_video_transcoder.go -package=mocks streamgate/pkg/service/transcoding VideoTranscoder
type VideoTranscoder interface {
	TranscodeHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn ProgressCallback) error
}

type TranscodingProfile struct {
	Name       string `json:"name"`
	VideoCodec string `json:"video_codec"`
	AudioCodec string `json:"audio_codec"`
	Resolution string `json:"resolution"`
	Bitrate    int    `json:"bitrate"`
	Framerate  int    `json:"framerate"`
	Format     string `json:"format"`
}

type TranscodeProgress struct {
	TaskID     string
	Profile    string
	Percentage float64
	Status     string
}

type ProgressCallback func(progress float64)

type SegmentStorage interface {
	Upload(ctx context.Context, bucket, objectName string, data []byte) error
	UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error
	UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error
	UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, bucket, objectName string) ([]byte, error)
	DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error)
	Delete(ctx context.Context, bucket, objectName string) error
	ListObjects(ctx context.Context, bucket, prefix string) ([]string, error)
	Exists(ctx context.Context, bucket, objectName string) (bool, error)
}

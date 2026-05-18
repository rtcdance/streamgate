package streaming

import (
	"context"
	"io"
	"time"
)

//go:generate mockgen -destination=mocks/mock_stream_service.go -package=mocks streamgate/pkg/service/streaming StreamService
type StreamService interface {
	GetStream(ctx context.Context, contentID string) (*StreamInfo, error)
	GetStreamByID(ctx context.Context, streamID string) (*StreamInfo, error)
	CreateStream(ctx context.Context, contentID, streamType string) (string, error)
	UpdateStreamStatus(ctx context.Context, streamID, status string) error
	UpdateStreamPlaylist(ctx context.Context, streamID, playlist string) error
	AddStreamQuality(ctx context.Context, streamID string, quality Quality) error
	DeleteStream(ctx context.Context, streamID string) error
}

//go:generate mockgen -destination=mocks/mock_segment_storage.go -package=mocks streamgate/pkg/service/streaming SegmentStorage
type SegmentStorage interface {
	Download(ctx context.Context, bucket, key string) ([]byte, error)
	Exists(ctx context.Context, bucket, key string) (bool, error)
	Upload(ctx context.Context, bucket, objectName string, data []byte) error
	UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error
	UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error
	UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error
	DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error)
	Delete(ctx context.Context, bucket, objectName string) error
	ListObjects(ctx context.Context, bucket, prefix string) ([]string, error)
}

type StreamInfo struct {
	ID        string    `json:"id"`
	ContentID string    `json:"content_id"`
	Type      string    `json:"type"`
	URL       string    `json:"url"`
	Playlist  string    `json:"playlist"`
	Qualities []Quality `json:"qualities"`
	Duration  int       `json:"duration"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Quality struct {
	Name       string `json:"name"`
	Resolution string `json:"resolution"`
	Bitrate    int    `json:"bitrate"`
	URL        string `json:"url"`
}

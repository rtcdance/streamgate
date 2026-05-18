package repository

import (
	"context"

	"streamgate/pkg/service/streaming"
)

type StreamingRepository interface {
	GetStreamByContentID(ctx context.Context, contentID string) (*streaming.StreamInfo, error)
	GetStreamByID(ctx context.Context, streamID string) (*streaming.StreamInfo, error)
	GetStreamQualities(ctx context.Context, streamID string) ([]streaming.Quality, error)
	CreateStream(ctx context.Context, contentID, streamType string) (string, error)
	UpdateStreamStatus(ctx context.Context, streamID, status string) (currentStatus, contentID string, err error)
	UpdateStreamPlaylist(ctx context.Context, streamID, playlist, url string) (contentID string, err error)
	AddStreamQuality(ctx context.Context, streamID string, q streaming.Quality) (contentID string, err error)
	DeleteStream(ctx context.Context, streamID string) error
	GetStreamContentID(ctx context.Context, streamID string) (string, error)
}

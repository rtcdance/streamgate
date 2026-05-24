package streamingv1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGetStreamURLRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetStreamURLRequest{
			ContentId: "c1",
			UserId:    "u1",
			Format:    "hls",
			Quality:   "720p",
			Protocol:  "https",
		}
		assert.Equal(t, "c1", req.GetContentId())
		assert.Equal(t, "u1", req.GetUserId())
		assert.Equal(t, "hls", req.GetFormat())
		assert.Equal(t, "720p", req.GetQuality())
		assert.Equal(t, "https", req.GetProtocol())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetStreamURLRequest
		assert.Equal(t, "", req.GetContentId())
		assert.Equal(t, "", req.GetUserId())
		assert.Equal(t, "", req.GetFormat())
		assert.Equal(t, "", req.GetQuality())
		assert.Equal(t, "", req.GetProtocol())
	})
}

func TestGetStreamURLResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		qualities := []*QualityOption{{Quality: "720p"}}
		resp := &GetStreamURLResponse{
			StreamUrl:          "https://stream.example.com/c1",
			ManifestUrl:        "https://stream.example.com/c1/manifest.m3u8",
			AvailableQualities: qualities,
			ExpiresAt:          999,
		}
		assert.Equal(t, "https://stream.example.com/c1", resp.GetStreamUrl())
		assert.Equal(t, "https://stream.example.com/c1/manifest.m3u8", resp.GetManifestUrl())
		assert.Equal(t, qualities, resp.GetAvailableQualities())
		assert.Equal(t, int64(999), resp.GetExpiresAt())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetStreamURLResponse
		assert.Equal(t, "", resp.GetStreamUrl())
		assert.Equal(t, "", resp.GetManifestUrl())
		assert.Nil(t, resp.GetAvailableQualities())
		assert.Equal(t, int64(0), resp.GetExpiresAt())
	})
}

func TestGetManifestRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetManifestRequest{ContentId: "c1", Format: "hls"}
		assert.Equal(t, "c1", req.GetContentId())
		assert.Equal(t, "hls", req.GetFormat())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetManifestRequest
		assert.Equal(t, "", req.GetContentId())
		assert.Equal(t, "", req.GetFormat())
	})
}

func TestGetManifestResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		segments := []*SegmentInfo{{SegmentId: "s1"}}
		resp := &GetManifestResponse{
			Manifest:    "#EXTM3U",
			ContentType: "application/vnd.apple.mpegurl",
			Segments:    segments,
		}
		assert.Equal(t, "#EXTM3U", resp.GetManifest())
		assert.Equal(t, "application/vnd.apple.mpegurl", resp.GetContentType())
		assert.Equal(t, segments, resp.GetSegments())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetManifestResponse
		assert.Equal(t, "", resp.GetManifest())
		assert.Equal(t, "", resp.GetContentType())
		assert.Nil(t, resp.GetSegments())
	})
}

func TestGetSegmentRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetSegmentRequest{
			ContentId: "c1",
			SegmentId: "s1",
			Format:    "hls",
			Quality:   "720p",
		}
		assert.Equal(t, "c1", req.GetContentId())
		assert.Equal(t, "s1", req.GetSegmentId())
		assert.Equal(t, "hls", req.GetFormat())
		assert.Equal(t, "720p", req.GetQuality())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetSegmentRequest
		assert.Equal(t, "", req.GetContentId())
		assert.Equal(t, "", req.GetSegmentId())
		assert.Equal(t, "", req.GetFormat())
		assert.Equal(t, "", req.GetQuality())
	})
}

func TestGetSegmentResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &GetSegmentResponse{
			Data:        []byte("segment-data"),
			ContentType: "video/mp2t",
			Size:        1024,
			Etag:        "abc123",
		}
		assert.Equal(t, []byte("segment-data"), resp.GetData())
		assert.Equal(t, "video/mp2t", resp.GetContentType())
		assert.Equal(t, int64(1024), resp.GetSize())
		assert.Equal(t, "abc123", resp.GetEtag())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetSegmentResponse
		assert.Nil(t, resp.GetData())
		assert.Equal(t, "", resp.GetContentType())
		assert.Equal(t, int64(0), resp.GetSize())
		assert.Equal(t, "", resp.GetEtag())
	})
}

func TestStartStreamRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &StartStreamRequest{
			ContentId:    "c1",
			UserId:       "u1",
			WalletAddress: "0xABC",
		}
		assert.Equal(t, "c1", req.GetContentId())
		assert.Equal(t, "u1", req.GetUserId())
		assert.Equal(t, "0xABC", req.GetWalletAddress())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *StartStreamRequest
		assert.Equal(t, "", req.GetContentId())
		assert.Equal(t, "", req.GetUserId())
		assert.Equal(t, "", req.GetWalletAddress())
	})
}

func TestStartStreamResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &StartStreamResponse{
			StreamId:  "s1",
			StreamUrl: "https://stream.example.com/s1",
			ExpiresAt: 999,
		}
		assert.Equal(t, "s1", resp.GetStreamId())
		assert.Equal(t, "https://stream.example.com/s1", resp.GetStreamUrl())
		assert.Equal(t, int64(999), resp.GetExpiresAt())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *StartStreamResponse
		assert.Equal(t, "", resp.GetStreamId())
		assert.Equal(t, "", resp.GetStreamUrl())
		assert.Equal(t, int64(0), resp.GetExpiresAt())
	})
}

func TestStopStreamRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &StopStreamRequest{StreamId: "s1", UserId: "u1"}
		assert.Equal(t, "s1", req.GetStreamId())
		assert.Equal(t, "u1", req.GetUserId())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *StopStreamRequest
		assert.Equal(t, "", req.GetStreamId())
		assert.Equal(t, "", req.GetUserId())
	})
}

func TestStopStreamResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &StopStreamResponse{
			Success:         true,
			Duration:        120,
			BytesTransferred: 1024000,
		}
		assert.True(t, resp.GetSuccess())
		assert.Equal(t, int64(120), resp.GetDuration())
		assert.Equal(t, int64(1024000), resp.GetBytesTransferred())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *StopStreamResponse
		assert.False(t, resp.GetSuccess())
		assert.Equal(t, int64(0), resp.GetDuration())
		assert.Equal(t, int64(0), resp.GetBytesTransferred())
	})
}

func TestGetStreamStatsRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetStreamStatsRequest{StreamId: "s1"}
		assert.Equal(t, "s1", req.GetStreamId())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetStreamStatsRequest
		assert.Equal(t, "", req.GetStreamId())
	})
}

func TestGetStreamStatsResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		events := []*BufferEvent{{Timestamp: 100, EventType: "stall"}}
		resp := &GetStreamStatsResponse{
			StreamId:        "s1",
			Status:          "active",
			StartedAt:       100,
			Duration:        60,
			BytesTransferred: 500000,
			AverageBitrate:  2500.5,
			BufferEvents:    events,
		}
		assert.Equal(t, "s1", resp.GetStreamId())
		assert.Equal(t, "active", resp.GetStatus())
		assert.Equal(t, int64(100), resp.GetStartedAt())
		assert.Equal(t, int64(60), resp.GetDuration())
		assert.Equal(t, int64(500000), resp.GetBytesTransferred())
		assert.Equal(t, 2500.5, resp.GetAverageBitrate())
		assert.Equal(t, events, resp.GetBufferEvents())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetStreamStatsResponse
		assert.Equal(t, "", resp.GetStreamId())
		assert.Equal(t, "", resp.GetStatus())
		assert.Equal(t, int64(0), resp.GetStartedAt())
		assert.Equal(t, int64(0), resp.GetDuration())
		assert.Equal(t, int64(0), resp.GetBytesTransferred())
		assert.Equal(t, 0.0, resp.GetAverageBitrate())
		assert.Nil(t, resp.GetBufferEvents())
	})
}

func TestQualityOption_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		q := &QualityOption{
			Quality: "720p",
			Width:   1280,
			Height:  720,
			Bitrate: 2500,
			Url:     "https://stream.example.com/720p",
		}
		assert.Equal(t, "720p", q.GetQuality())
		assert.Equal(t, int32(1280), q.GetWidth())
		assert.Equal(t, int32(720), q.GetHeight())
		assert.Equal(t, int32(2500), q.GetBitrate())
		assert.Equal(t, "https://stream.example.com/720p", q.GetUrl())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var q *QualityOption
		assert.Equal(t, "", q.GetQuality())
		assert.Equal(t, int32(0), q.GetWidth())
		assert.Equal(t, int32(0), q.GetHeight())
		assert.Equal(t, int32(0), q.GetBitrate())
		assert.Equal(t, "", q.GetUrl())
	})
}

func TestSegmentInfo_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		s := &SegmentInfo{
			SegmentId: "s1",
			Quality:   "720p",
			Duration:  10,
			Size:      50000,
			Url:       "https://stream.example.com/s1.ts",
		}
		assert.Equal(t, "s1", s.GetSegmentId())
		assert.Equal(t, "720p", s.GetQuality())
		assert.Equal(t, int64(10), s.GetDuration())
		assert.Equal(t, int64(50000), s.GetSize())
		assert.Equal(t, "https://stream.example.com/s1.ts", s.GetUrl())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var s *SegmentInfo
		assert.Equal(t, "", s.GetSegmentId())
		assert.Equal(t, "", s.GetQuality())
		assert.Equal(t, int64(0), s.GetDuration())
		assert.Equal(t, int64(0), s.GetSize())
		assert.Equal(t, "", s.GetUrl())
	})
}

func TestBufferEvent_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		e := &BufferEvent{
			Timestamp:    100,
			EventType:    "stall",
			BufferLength: 2.5,
		}
		assert.Equal(t, int64(100), e.GetTimestamp())
		assert.Equal(t, "stall", e.GetEventType())
		assert.Equal(t, 2.5, e.GetBufferLength())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var e *BufferEvent
		assert.Equal(t, int64(0), e.GetTimestamp())
		assert.Equal(t, "", e.GetEventType())
		assert.Equal(t, 0.0, e.GetBufferLength())
	})
}

func TestStreamingService_UnimplementedServer(t *testing.T) {
	server := UnimplementedStreamingServiceServer{}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"GetStreamURL", func() error { _, err := server.GetStreamURL(context.Background(), &GetStreamURLRequest{}); return err }},
		{"GetManifest", func() error { _, err := server.GetManifest(context.Background(), &GetManifestRequest{}); return err }},
		{"GetSegment", func() error { _, err := server.GetSegment(context.Background(), &GetSegmentRequest{}); return err }},
		{"StartStream", func() error { _, err := server.StartStream(context.Background(), &StartStreamRequest{}); return err }},
		{"StopStream", func() error { _, err := server.StopStream(context.Background(), &StopStreamRequest{}); return err }},
		{"GetStreamStats", func() error { _, err := server.GetStreamStats(context.Background(), &GetStreamStatsRequest{}); return err }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			require.Error(t, err)
			assert.Equal(t, codes.Unimplemented, status.Code(err))
		})
	}
}

func TestAllMessages_ProtoMethods(t *testing.T) {
	msgs := []interface {
		Reset()
		String() string
		ProtoMessage()
	}{
		&GetStreamURLRequest{},
		&GetStreamURLResponse{},
		&GetManifestRequest{},
		&GetManifestResponse{},
		&GetSegmentRequest{},
		&GetSegmentResponse{},
		&StartStreamRequest{},
		&StartStreamResponse{},
		&StopStreamRequest{},
		&StopStreamResponse{},
		&GetStreamStatsRequest{},
		&GetStreamStatsResponse{},
		&QualityOption{},
		&SegmentInfo{},
		&BufferEvent{},
	}

	for _, msg := range msgs {
		assert.NotPanics(t, msg.Reset)
		assert.NotPanics(t, msg.ProtoMessage)
		_ = msg.String()
	}
}

package uploadv1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInitUploadRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &InitUploadRequest{
			UserId:    "u1",
			Filename:  "video.mp4",
			FileSize:  1024000,
			MimeType:  "video/mp4",
			Checksum:  "abc123",
			ChunkSize: 5,
			Metadata:  map[string]string{"key": "val"},
		}
		assert.Equal(t, "u1", req.GetUserId())
		assert.Equal(t, "video.mp4", req.GetFilename())
		assert.Equal(t, int64(1024000), req.GetFileSize())
		assert.Equal(t, "video/mp4", req.GetMimeType())
		assert.Equal(t, "abc123", req.GetChecksum())
		assert.Equal(t, int32(5), req.GetChunkSize())
		assert.Equal(t, map[string]string{"key": "val"}, req.GetMetadata())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *InitUploadRequest
		assert.Equal(t, "", req.GetUserId())
		assert.Equal(t, "", req.GetFilename())
		assert.Equal(t, int64(0), req.GetFileSize())
		assert.Equal(t, "", req.GetMimeType())
		assert.Equal(t, "", req.GetChecksum())
		assert.Equal(t, int32(0), req.GetChunkSize())
		assert.Nil(t, req.GetMetadata())
	})
}

func TestInitUploadResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &InitUploadResponse{
			UploadId:   "up1",
			UploadUrls: []string{"https://upload.example.com/1", "https://upload.example.com/2"},
			ChunkCount: 2,
			ExpiresAt:  999,
		}
		assert.Equal(t, "up1", resp.GetUploadId())
		assert.Equal(t, []string{"https://upload.example.com/1", "https://upload.example.com/2"}, resp.GetUploadUrls())
		assert.Equal(t, int32(2), resp.GetChunkCount())
		assert.Equal(t, int64(999), resp.GetExpiresAt())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *InitUploadResponse
		assert.Equal(t, "", resp.GetUploadId())
		assert.Nil(t, resp.GetUploadUrls())
		assert.Equal(t, int32(0), resp.GetChunkCount())
		assert.Equal(t, int64(0), resp.GetExpiresAt())
	})
}

func TestUploadPartRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &UploadPartRequest{
			UploadId:   "up1",
			PartNumber: 1,
			Data:       []byte("chunk-data"),
			Checksum:   "def456",
		}
		assert.Equal(t, "up1", req.GetUploadId())
		assert.Equal(t, int32(1), req.GetPartNumber())
		assert.Equal(t, []byte("chunk-data"), req.GetData())
		assert.Equal(t, "def456", req.GetChecksum())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *UploadPartRequest
		assert.Equal(t, "", req.GetUploadId())
		assert.Equal(t, int32(0), req.GetPartNumber())
		assert.Nil(t, req.GetData())
		assert.Equal(t, "", req.GetChecksum())
	})
}

func TestUploadPartResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &UploadPartResponse{
			Success:       true,
			Etag:          "etag1",
			BytesReceived: 1024,
		}
		assert.True(t, resp.GetSuccess())
		assert.Equal(t, "etag1", resp.GetEtag())
		assert.Equal(t, int64(1024), resp.GetBytesReceived())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *UploadPartResponse
		assert.False(t, resp.GetSuccess())
		assert.Equal(t, "", resp.GetEtag())
		assert.Equal(t, int64(0), resp.GetBytesReceived())
	})
}

func TestCompleteUploadRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		parts := []*PartETag{{PartNumber: 1, Etag: "etag1"}}
		req := &CompleteUploadRequest{
			UploadId: "up1",
			Parts:    parts,
		}
		assert.Equal(t, "up1", req.GetUploadId())
		assert.Equal(t, parts, req.GetParts())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *CompleteUploadRequest
		assert.Equal(t, "", req.GetUploadId())
		assert.Nil(t, req.GetParts())
	})
}

func TestCompleteUploadResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &CompleteUploadResponse{
			Success:   true,
			ContentId: "c1",
			Url:       "https://cdn.example.com/c1/video.mp4",
			Size:      1024000,
		}
		assert.True(t, resp.GetSuccess())
		assert.Equal(t, "c1", resp.GetContentId())
		assert.Equal(t, "https://cdn.example.com/c1/video.mp4", resp.GetUrl())
		assert.Equal(t, int64(1024000), resp.GetSize())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *CompleteUploadResponse
		assert.False(t, resp.GetSuccess())
		assert.Equal(t, "", resp.GetContentId())
		assert.Equal(t, "", resp.GetUrl())
		assert.Equal(t, int64(0), resp.GetSize())
	})
}

func TestAbortUploadRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &AbortUploadRequest{UploadId: "up1", Reason: "cancelled"}
		assert.Equal(t, "up1", req.GetUploadId())
		assert.Equal(t, "cancelled", req.GetReason())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *AbortUploadRequest
		assert.Equal(t, "", req.GetUploadId())
		assert.Equal(t, "", req.GetReason())
	})
}

func TestAbortUploadResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &AbortUploadResponse{Success: true}
		assert.True(t, resp.GetSuccess())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *AbortUploadResponse
		assert.False(t, resp.GetSuccess())
	})
}

func TestGetUploadStatusRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetUploadStatusRequest{UploadId: "up1"}
		assert.Equal(t, "up1", req.GetUploadId())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetUploadStatusRequest
		assert.Equal(t, "", req.GetUploadId())
	})
}

func TestGetUploadStatusResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &GetUploadStatusResponse{
			UploadId:      "up1",
			Status:        "in_progress",
			PartsUploaded: 3,
			TotalParts:    5,
			BytesUploaded: 3072,
			TotalBytes:    5120,
			StartedAt:     100,
			UpdatedAt:     200,
		}
		assert.Equal(t, "up1", resp.GetUploadId())
		assert.Equal(t, "in_progress", resp.GetStatus())
		assert.Equal(t, int32(3), resp.GetPartsUploaded())
		assert.Equal(t, int32(5), resp.GetTotalParts())
		assert.Equal(t, int64(3072), resp.GetBytesUploaded())
		assert.Equal(t, int64(5120), resp.GetTotalBytes())
		assert.Equal(t, int64(100), resp.GetStartedAt())
		assert.Equal(t, int64(200), resp.GetUpdatedAt())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetUploadStatusResponse
		assert.Equal(t, "", resp.GetUploadId())
		assert.Equal(t, "", resp.GetStatus())
		assert.Equal(t, int32(0), resp.GetPartsUploaded())
		assert.Equal(t, int32(0), resp.GetTotalParts())
		assert.Equal(t, int64(0), resp.GetBytesUploaded())
		assert.Equal(t, int64(0), resp.GetTotalBytes())
		assert.Equal(t, int64(0), resp.GetStartedAt())
		assert.Equal(t, int64(0), resp.GetUpdatedAt())
	})
}

func TestPartETag_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		p := &PartETag{PartNumber: 1, Etag: "etag1"}
		assert.Equal(t, int32(1), p.GetPartNumber())
		assert.Equal(t, "etag1", p.GetEtag())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var p *PartETag
		assert.Equal(t, int32(0), p.GetPartNumber())
		assert.Equal(t, "", p.GetEtag())
	})
}

func TestUploadService_UnimplementedServer(t *testing.T) {
	server := UnimplementedUploadServiceServer{}

	t.Run("InitUpload", func(t *testing.T) {
		_, err := server.InitUpload(context.Background(), &InitUploadRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("CompleteUpload", func(t *testing.T) {
		_, err := server.CompleteUpload(context.Background(), &CompleteUploadRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("AbortUpload", func(t *testing.T) {
		_, err := server.AbortUpload(context.Background(), &AbortUploadRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetUploadStatus", func(t *testing.T) {
		_, err := server.GetUploadStatus(context.Background(), &GetUploadStatusRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

func TestAllMessages_ProtoMethods(t *testing.T) {
	msgs := []interface {
		Reset()
		String() string
		ProtoMessage()
	}{
		&InitUploadRequest{},
		&InitUploadResponse{},
		&UploadPartRequest{},
		&UploadPartResponse{},
		&CompleteUploadRequest{},
		&CompleteUploadResponse{},
		&AbortUploadRequest{},
		&AbortUploadResponse{},
		&GetUploadStatusRequest{},
		&GetUploadStatusResponse{},
		&PartETag{},
	}

	for _, msg := range msgs {
		assert.NotPanics(t, msg.Reset)
		assert.NotPanics(t, msg.ProtoMessage)
		_ = msg.String()
	}
}

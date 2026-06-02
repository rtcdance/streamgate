package contentv1

import (
	"context"
	"testing"

	commonv1 "github.com/rtcdance/streamgate/pkg/api/v1/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGetContentRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetContentRequest{ContentId: "c1", UserId: "u1"}
		assert.Equal(t, "c1", req.GetContentId())
		assert.Equal(t, "u1", req.GetUserId())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetContentRequest
		assert.Equal(t, "", req.GetContentId())
		assert.Equal(t, "", req.GetUserId())
	})
}

func TestGetContentResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		content := &Content{Id: "c1"}
		metadata := &commonv1.Metadata{RequestId: "r1"}
		resp := &GetContentResponse{
			Content:       content,
			StreamingUrls: []string{"url1", "url2"},
			Metadata:      metadata,
		}
		assert.Equal(t, content, resp.GetContent())
		assert.Equal(t, []string{"url1", "url2"}, resp.GetStreamingUrls())
		assert.Equal(t, metadata, resp.GetMetadata())
	})

	t.Run("nil_nested", func(t *testing.T) {
		resp := &GetContentResponse{}
		assert.Nil(t, resp.GetContent())
		assert.Nil(t, resp.GetStreamingUrls())
		assert.Nil(t, resp.GetMetadata())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetContentResponse
		assert.Nil(t, resp.GetContent())
		assert.Nil(t, resp.GetStreamingUrls())
		assert.Nil(t, resp.GetMetadata())
	})
}

func TestVerifyAccessRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &VerifyAccessRequest{
			ContentId:       "c1",
			UserId:          "u1",
			WalletAddress:   "0xABC",
			ContractAddress: "0xCON",
			TokenId:         "t1",
		}
		assert.Equal(t, "c1", req.GetContentId())
		assert.Equal(t, "u1", req.GetUserId())
		assert.Equal(t, "0xABC", req.GetWalletAddress())
		assert.Equal(t, "0xCON", req.GetContractAddress())
		assert.Equal(t, "t1", req.GetTokenId())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *VerifyAccessRequest
		assert.Equal(t, "", req.GetContentId())
		assert.Equal(t, "", req.GetUserId())
		assert.Equal(t, "", req.GetWalletAddress())
		assert.Equal(t, "", req.GetContractAddress())
		assert.Equal(t, "", req.GetTokenId())
	})
}

func TestVerifyAccessResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &VerifyAccessResponse{
			HasAccess:    true,
			AccessType:   "nft",
			ExpiresAt:    999,
			RequiredNfts: []string{"nft1"},
		}
		assert.True(t, resp.GetHasAccess())
		assert.Equal(t, "nft", resp.GetAccessType())
		assert.Equal(t, int64(999), resp.GetExpiresAt())
		assert.Equal(t, []string{"nft1"}, resp.GetRequiredNfts())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *VerifyAccessResponse
		assert.False(t, resp.GetHasAccess())
		assert.Equal(t, "", resp.GetAccessType())
		assert.Equal(t, int64(0), resp.GetExpiresAt())
		assert.Nil(t, resp.GetRequiredNfts())
	})
}

func TestGetTranscodeStatusRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetTranscodeStatusRequest{ContentId: "c1"}
		assert.Equal(t, "c1", req.GetContentId())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetTranscodeStatusRequest
		assert.Equal(t, "", req.GetContentId())
	})
}

func TestGetTranscodeStatusResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		outputs := []*TranscodeOutput{{Format: "hls", Quality: "720p"}}
		resp := &GetTranscodeStatusResponse{
			Status:       "completed",
			Progress:     100,
			Outputs:      outputs,
			ErrorMessage: "",
			StartedAt:    100,
			CompletedAt:  200,
		}
		assert.Equal(t, "completed", resp.GetStatus())
		assert.Equal(t, int32(100), resp.GetProgress())
		assert.Equal(t, outputs, resp.GetOutputs())
		assert.Equal(t, "", resp.GetErrorMessage())
		assert.Equal(t, int64(100), resp.GetStartedAt())
		assert.Equal(t, int64(200), resp.GetCompletedAt())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetTranscodeStatusResponse
		assert.Equal(t, "", resp.GetStatus())
		assert.Equal(t, int32(0), resp.GetProgress())
		assert.Nil(t, resp.GetOutputs())
		assert.Equal(t, "", resp.GetErrorMessage())
		assert.Equal(t, int64(0), resp.GetStartedAt())
		assert.Equal(t, int64(0), resp.GetCompletedAt())
	})
}

func TestListContentRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &ListContentRequest{
			UserId:   "u1",
			Page:     1,
			PageSize: 10,
			Status:   "active",
		}
		assert.Equal(t, "u1", req.GetUserId())
		assert.Equal(t, int32(1), req.GetPage())
		assert.Equal(t, int32(10), req.GetPageSize())
		assert.Equal(t, "active", req.GetStatus())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *ListContentRequest
		assert.Equal(t, "", req.GetUserId())
		assert.Equal(t, int32(0), req.GetPage())
		assert.Equal(t, int32(0), req.GetPageSize())
		assert.Equal(t, "", req.GetStatus())
	})
}

func TestListContentResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		contents := []*Content{{Id: "c1"}, {Id: "c2"}}
		resp := &ListContentResponse{
			Contents: contents,
			Total:    2,
			Page:     1,
			PageSize: 10,
		}
		assert.Equal(t, contents, resp.GetContents())
		assert.Equal(t, int32(2), resp.GetTotal())
		assert.Equal(t, int32(1), resp.GetPage())
		assert.Equal(t, int32(10), resp.GetPageSize())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *ListContentResponse
		assert.Nil(t, resp.GetContents())
		assert.Equal(t, int32(0), resp.GetTotal())
		assert.Equal(t, int32(0), resp.GetPage())
		assert.Equal(t, int32(0), resp.GetPageSize())
	})
}

func TestDeleteContentRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &DeleteContentRequest{ContentId: "c1", UserId: "u1"}
		assert.Equal(t, "c1", req.GetContentId())
		assert.Equal(t, "u1", req.GetUserId())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *DeleteContentRequest
		assert.Equal(t, "", req.GetContentId())
		assert.Equal(t, "", req.GetUserId())
	})
}

func TestDeleteContentResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &DeleteContentResponse{Success: true}
		assert.True(t, resp.GetSuccess())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *DeleteContentResponse
		assert.False(t, resp.GetSuccess())
	})
}

func TestContent_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		c := &Content{
			Id:           "c1",
			Title:        "Test Video",
			Description:  "A test",
			OriginalUrl:  "https://example.com/video.mp4",
			ThumbnailUrl: "https://example.com/thumb.jpg",
			Duration:     120,
			Size:         1024000,
			MimeType:     "video/mp4",
			Status:       "active",
			CreatedAt:    1000,
			UpdatedAt:    2000,
			Tags:         []string{"test", "video"},
			Metadata:     map[string]string{"key": "val"},
		}
		assert.Equal(t, "c1", c.GetId())
		assert.Equal(t, "Test Video", c.GetTitle())
		assert.Equal(t, "A test", c.GetDescription())
		assert.Equal(t, "https://example.com/video.mp4", c.GetOriginalUrl())
		assert.Equal(t, "https://example.com/thumb.jpg", c.GetThumbnailUrl())
		assert.Equal(t, int64(120), c.GetDuration())
		assert.Equal(t, int64(1024000), c.GetSize())
		assert.Equal(t, "video/mp4", c.GetMimeType())
		assert.Equal(t, "active", c.GetStatus())
		assert.Equal(t, int64(1000), c.GetCreatedAt())
		assert.Equal(t, int64(2000), c.GetUpdatedAt())
		assert.Equal(t, []string{"test", "video"}, c.GetTags())
		assert.Equal(t, map[string]string{"key": "val"}, c.GetMetadata())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var c *Content
		assert.Equal(t, "", c.GetId())
		assert.Equal(t, "", c.GetTitle())
		assert.Equal(t, "", c.GetDescription())
		assert.Equal(t, "", c.GetOriginalUrl())
		assert.Equal(t, "", c.GetThumbnailUrl())
		assert.Equal(t, int64(0), c.GetDuration())
		assert.Equal(t, int64(0), c.GetSize())
		assert.Equal(t, "", c.GetMimeType())
		assert.Equal(t, "", c.GetStatus())
		assert.Equal(t, int64(0), c.GetCreatedAt())
		assert.Equal(t, int64(0), c.GetUpdatedAt())
		assert.Nil(t, c.GetTags())
		assert.Nil(t, c.GetMetadata())
	})
}

func TestTranscodeOutput_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		out := &TranscodeOutput{
			Format:  "hls",
			Quality: "720p",
			Width:   1280,
			Height:  720,
			Bitrate: 2500,
			Url:     "https://example.com/out.m3u8",
			Size:    500000,
		}
		assert.Equal(t, "hls", out.GetFormat())
		assert.Equal(t, "720p", out.GetQuality())
		assert.Equal(t, int32(1280), out.GetWidth())
		assert.Equal(t, int32(720), out.GetHeight())
		assert.Equal(t, int32(2500), out.GetBitrate())
		assert.Equal(t, "https://example.com/out.m3u8", out.GetUrl())
		assert.Equal(t, int64(500000), out.GetSize())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var out *TranscodeOutput
		assert.Equal(t, "", out.GetFormat())
		assert.Equal(t, "", out.GetQuality())
		assert.Equal(t, int32(0), out.GetWidth())
		assert.Equal(t, int32(0), out.GetHeight())
		assert.Equal(t, int32(0), out.GetBitrate())
		assert.Equal(t, "", out.GetUrl())
		assert.Equal(t, int64(0), out.GetSize())
	})
}

func TestContentService_UnimplementedServer(t *testing.T) {
	server := UnimplementedContentServiceServer{}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"GetContent", func() error { _, err := server.GetContent(context.Background(), &GetContentRequest{}); return err }},
		{"VerifyAccess", func() error { _, err := server.VerifyAccess(context.Background(), &VerifyAccessRequest{}); return err }},
		{"GetTranscodeStatus", func() error {
			_, err := server.GetTranscodeStatus(context.Background(), &GetTranscodeStatusRequest{})
			return err
		}},
		{"ListContent", func() error { _, err := server.ListContent(context.Background(), &ListContentRequest{}); return err }},
		{"DeleteContent", func() error {
			_, err := server.DeleteContent(context.Background(), &DeleteContentRequest{})
			return err
		}},
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
		&GetContentRequest{},
		&GetContentResponse{},
		&VerifyAccessRequest{},
		&VerifyAccessResponse{},
		&GetTranscodeStatusRequest{},
		&GetTranscodeStatusResponse{},
		&ListContentRequest{},
		&ListContentResponse{},
		&DeleteContentRequest{},
		&DeleteContentResponse{},
		&Content{},
		&TranscodeOutput{},
	}

	for _, msg := range msgs {
		assert.NotPanics(t, msg.Reset)
		assert.NotPanics(t, msg.ProtoMessage)
		_ = msg.String()
	}
}

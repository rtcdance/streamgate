package nftv1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestVerifyOwnershipRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &VerifyOwnershipRequest{
			WalletAddress:   "0xABC",
			ContractAddress: "0xCON",
			TokenId:         "1",
			ChainId:         1,
			ChainType:       "ethereum",
		}
		assert.Equal(t, "0xABC", req.GetWalletAddress())
		assert.Equal(t, "0xCON", req.GetContractAddress())
		assert.Equal(t, "1", req.GetTokenId())
		assert.Equal(t, int64(1), req.GetChainId())
		assert.Equal(t, "ethereum", req.GetChainType())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *VerifyOwnershipRequest
		assert.Equal(t, "", req.GetWalletAddress())
		assert.Equal(t, "", req.GetContractAddress())
		assert.Equal(t, "", req.GetTokenId())
		assert.Equal(t, int64(0), req.GetChainId())
		assert.Equal(t, "", req.GetChainType())
	})
}

func TestVerifyOwnershipResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		meta := &NFTMetadata{Name: "TestNFT"}
		resp := &VerifyOwnershipResponse{
			OwnsNft:      true,
			OwnerAddress: "0xABC",
			Metadata:     meta,
			VerifiedAt:   999,
		}
		assert.True(t, resp.GetOwnsNft())
		assert.Equal(t, "0xABC", resp.GetOwnerAddress())
		assert.Equal(t, meta, resp.GetMetadata())
		assert.Equal(t, int64(999), resp.GetVerifiedAt())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *VerifyOwnershipResponse
		assert.False(t, resp.GetOwnsNft())
		assert.Equal(t, "", resp.GetOwnerAddress())
		assert.Nil(t, resp.GetMetadata())
		assert.Equal(t, int64(0), resp.GetVerifiedAt())
	})
}

func TestGetNFTMetadataRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetNFTMetadataRequest{
			ContractAddress: "0xCON",
			TokenId:         "1",
			ChainId:         1,
			ChainType:       "ethereum",
		}
		assert.Equal(t, "0xCON", req.GetContractAddress())
		assert.Equal(t, "1", req.GetTokenId())
		assert.Equal(t, int64(1), req.GetChainId())
		assert.Equal(t, "ethereum", req.GetChainType())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetNFTMetadataRequest
		assert.Equal(t, "", req.GetContractAddress())
		assert.Equal(t, "", req.GetTokenId())
		assert.Equal(t, int64(0), req.GetChainId())
		assert.Equal(t, "", req.GetChainType())
	})
}

func TestGetNFTMetadataResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		meta := &NFTMetadata{Name: "TestNFT"}
		resp := &GetNFTMetadataResponse{Metadata: meta, Found: true}
		assert.Equal(t, meta, resp.GetMetadata())
		assert.True(t, resp.GetFound())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetNFTMetadataResponse
		assert.Nil(t, resp.GetMetadata())
		assert.False(t, resp.GetFound())
	})
}

func TestGetNFTBalanceRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetNFTBalanceRequest{
			WalletAddress:   "0xABC",
			ContractAddress: "0xCON",
			ChainId:         1,
			ChainType:       "ethereum",
		}
		assert.Equal(t, "0xABC", req.GetWalletAddress())
		assert.Equal(t, "0xCON", req.GetContractAddress())
		assert.Equal(t, int64(1), req.GetChainId())
		assert.Equal(t, "ethereum", req.GetChainType())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetNFTBalanceRequest
		assert.Equal(t, "", req.GetWalletAddress())
		assert.Equal(t, "", req.GetContractAddress())
		assert.Equal(t, int64(0), req.GetChainId())
		assert.Equal(t, "", req.GetChainType())
	})
}

func TestGetNFTBalanceResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &GetNFTBalanceResponse{Balance: 5, TokenIds: []string{"1", "2"}}
		assert.Equal(t, int64(5), resp.GetBalance())
		assert.Equal(t, []string{"1", "2"}, resp.GetTokenIds())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetNFTBalanceResponse
		assert.Equal(t, int64(0), resp.GetBalance())
		assert.Nil(t, resp.GetTokenIds())
	})
}

func TestListUserNFTsRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &ListUserNFTsRequest{
			WalletAddress: "0xABC",
			ChainId:       1,
			ChainType:     "ethereum",
			Page:          1,
			PageSize:      10,
		}
		assert.Equal(t, "0xABC", req.GetWalletAddress())
		assert.Equal(t, int64(1), req.GetChainId())
		assert.Equal(t, "ethereum", req.GetChainType())
		assert.Equal(t, int32(1), req.GetPage())
		assert.Equal(t, int32(10), req.GetPageSize())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *ListUserNFTsRequest
		assert.Equal(t, "", req.GetWalletAddress())
		assert.Equal(t, int64(0), req.GetChainId())
		assert.Equal(t, "", req.GetChainType())
		assert.Equal(t, int32(0), req.GetPage())
		assert.Equal(t, int32(0), req.GetPageSize())
	})
}

func TestListUserNFTsResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		nfts := []*NFTItem{{ContractAddress: "0xCON", TokenId: "1"}}
		resp := &ListUserNFTsResponse{
			Nfts:     nfts,
			Total:    1,
			Page:     1,
			PageSize: 10,
		}
		assert.Equal(t, nfts, resp.GetNfts())
		assert.Equal(t, int32(1), resp.GetTotal())
		assert.Equal(t, int32(1), resp.GetPage())
		assert.Equal(t, int32(10), resp.GetPageSize())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *ListUserNFTsResponse
		assert.Nil(t, resp.GetNfts())
		assert.Equal(t, int32(0), resp.GetTotal())
		assert.Equal(t, int32(0), resp.GetPage())
		assert.Equal(t, int32(0), resp.GetPageSize())
	})
}

func TestGetContractInfoRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetContractInfoRequest{
			ContractAddress: "0xCON",
			ChainId:         1,
			ChainType:       "ethereum",
		}
		assert.Equal(t, "0xCON", req.GetContractAddress())
		assert.Equal(t, int64(1), req.GetChainId())
		assert.Equal(t, "ethereum", req.GetChainType())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetContractInfoRequest
		assert.Equal(t, "", req.GetContractAddress())
		assert.Equal(t, int64(0), req.GetChainId())
		assert.Equal(t, "", req.GetChainType())
	})
}

func TestGetContractInfoResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		info := &ContractInfo{Address: "0xCON"}
		resp := &GetContractInfoResponse{Info: info, Found: true}
		assert.Equal(t, info, resp.GetInfo())
		assert.True(t, resp.GetFound())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetContractInfoResponse
		assert.Nil(t, resp.GetInfo())
		assert.False(t, resp.GetFound())
	})
}

func TestNFTMetadata_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		attrs := []*NFTAttribute{{TraitType: "Color", Value: "Blue"}}
		meta := &NFTMetadata{
			Name:         "TestNFT",
			Description:  "A test",
			Image:        "https://example.com/img.png",
			AnimationUrl: "https://example.com/anim.mp4",
			ExternalUrl:  "https://example.com",
			Attributes:   attrs,
			Properties:   map[string]string{"key": "val"},
		}
		assert.Equal(t, "TestNFT", meta.GetName())
		assert.Equal(t, "A test", meta.GetDescription())
		assert.Equal(t, "https://example.com/img.png", meta.GetImage())
		assert.Equal(t, "https://example.com/anim.mp4", meta.GetAnimationUrl())
		assert.Equal(t, "https://example.com", meta.GetExternalUrl())
		assert.Equal(t, attrs, meta.GetAttributes())
		assert.Equal(t, map[string]string{"key": "val"}, meta.GetProperties())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var meta *NFTMetadata
		assert.Equal(t, "", meta.GetName())
		assert.Equal(t, "", meta.GetDescription())
		assert.Equal(t, "", meta.GetImage())
		assert.Equal(t, "", meta.GetAnimationUrl())
		assert.Equal(t, "", meta.GetExternalUrl())
		assert.Nil(t, meta.GetAttributes())
		assert.Nil(t, meta.GetProperties())
	})
}

func TestNFTAttribute_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		attr := &NFTAttribute{
			TraitType:   "Color",
			Value:       "Blue",
			DisplayType: "string",
			MaxValue:    100,
		}
		assert.Equal(t, "Color", attr.GetTraitType())
		assert.Equal(t, "Blue", attr.GetValue())
		assert.Equal(t, "string", attr.GetDisplayType())
		assert.Equal(t, int32(100), attr.GetMaxValue())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var attr *NFTAttribute
		assert.Equal(t, "", attr.GetTraitType())
		assert.Equal(t, "", attr.GetValue())
		assert.Equal(t, "", attr.GetDisplayType())
		assert.Equal(t, int32(0), attr.GetMaxValue())
	})
}

func TestNFTItem_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		meta := &NFTMetadata{Name: "Test"}
		item := &NFTItem{
			ContractAddress: "0xCON",
			TokenId:         "1",
			Name:            "Item1",
			Image:           "https://example.com/img.png",
			Balance:         2,
			Metadata:        meta,
		}
		assert.Equal(t, "0xCON", item.GetContractAddress())
		assert.Equal(t, "1", item.GetTokenId())
		assert.Equal(t, "Item1", item.GetName())
		assert.Equal(t, "https://example.com/img.png", item.GetImage())
		assert.Equal(t, int64(2), item.GetBalance())
		assert.Equal(t, meta, item.GetMetadata())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var item *NFTItem
		assert.Equal(t, "", item.GetContractAddress())
		assert.Equal(t, "", item.GetTokenId())
		assert.Equal(t, "", item.GetName())
		assert.Equal(t, "", item.GetImage())
		assert.Equal(t, int64(0), item.GetBalance())
		assert.Nil(t, item.GetMetadata())
	})
}

func TestContractInfo_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		info := &ContractInfo{
			Address:      "0xCON",
			Name:         "TestContract",
			Symbol:       "TST",
			ContractType: "ERC-721",
			ChainId:      1,
			ChainName:    "Ethereum",
		}
		assert.Equal(t, "0xCON", info.GetAddress())
		assert.Equal(t, "TestContract", info.GetName())
		assert.Equal(t, "TST", info.GetSymbol())
		assert.Equal(t, "ERC-721", info.GetContractType())
		assert.Equal(t, int64(1), info.GetChainId())
		assert.Equal(t, "Ethereum", info.GetChainName())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var info *ContractInfo
		assert.Equal(t, "", info.GetAddress())
		assert.Equal(t, "", info.GetName())
		assert.Equal(t, "", info.GetSymbol())
		assert.Equal(t, "", info.GetContractType())
		assert.Equal(t, int64(0), info.GetChainId())
		assert.Equal(t, "", info.GetChainName())
	})
}

func TestNFTService_UnimplementedServer(t *testing.T) {
	server := UnimplementedNFTServiceServer{}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"VerifyOwnership", func() error {
			_, err := server.VerifyOwnership(context.Background(), &VerifyOwnershipRequest{})
			return err
		}},
		{"GetNFTMetadata", func() error {
			_, err := server.GetNFTMetadata(context.Background(), &GetNFTMetadataRequest{})
			return err
		}},
		{"GetNFTBalance", func() error {
			_, err := server.GetNFTBalance(context.Background(), &GetNFTBalanceRequest{})
			return err
		}},
		{"ListUserNFTs", func() error { _, err := server.ListUserNFTs(context.Background(), &ListUserNFTsRequest{}); return err }},
		{"GetContractInfo", func() error {
			_, err := server.GetContractInfo(context.Background(), &GetContractInfoRequest{})
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
		&VerifyOwnershipRequest{},
		&VerifyOwnershipResponse{},
		&GetNFTMetadataRequest{},
		&GetNFTMetadataResponse{},
		&GetNFTBalanceRequest{},
		&GetNFTBalanceResponse{},
		&ListUserNFTsRequest{},
		&ListUserNFTsResponse{},
		&GetContractInfoRequest{},
		&GetContractInfoResponse{},
		&NFTMetadata{},
		&NFTAttribute{},
		&NFTItem{},
		&ContractInfo{},
	}

	for _, msg := range msgs {
		assert.NotPanics(t, msg.Reset)
		assert.NotPanics(t, msg.ProtoMessage)
		_ = msg.String()
	}
}

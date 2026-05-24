package authv1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGetNonceRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &GetNonceRequest{WalletAddress: "0xABC", ChainType: "ethereum"}
		assert.Equal(t, "0xABC", req.GetWalletAddress())
		assert.Equal(t, "ethereum", req.GetChainType())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *GetNonceRequest
		assert.Equal(t, "", req.GetWalletAddress())
		assert.Equal(t, "", req.GetChainType())
	})
}

func TestGetNonceResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &GetNonceResponse{Nonce: "abc123", Message: "Sign this", ExpiresAt: 1234567890}
		assert.Equal(t, "abc123", resp.GetNonce())
		assert.Equal(t, "Sign this", resp.GetMessage())
		assert.Equal(t, int64(1234567890), resp.GetExpiresAt())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *GetNonceResponse
		assert.Equal(t, "", resp.GetNonce())
		assert.Equal(t, "", resp.GetMessage())
		assert.Equal(t, int64(0), resp.GetExpiresAt())
	})
}

func TestVerifySignatureRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &VerifySignatureRequest{
			WalletAddress: "0xABC",
			Signature:     "0xSIG",
			Nonce:         "nonce123",
			ChainType:     "ethereum",
			Message:       "sign me",
		}
		assert.Equal(t, "0xABC", req.GetWalletAddress())
		assert.Equal(t, "0xSIG", req.GetSignature())
		assert.Equal(t, "nonce123", req.GetNonce())
		assert.Equal(t, "ethereum", req.GetChainType())
		assert.Equal(t, "sign me", req.GetMessage())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *VerifySignatureRequest
		assert.Equal(t, "", req.GetWalletAddress())
		assert.Equal(t, "", req.GetSignature())
		assert.Equal(t, "", req.GetNonce())
		assert.Equal(t, "", req.GetChainType())
		assert.Equal(t, "", req.GetMessage())
	})
}

func TestVerifySignatureResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		userInfo := &UserInfo{UserId: "u1", WalletAddress: "0xABC"}
		resp := &VerifySignatureResponse{
			Valid:        true,
			AccessToken:  "at",
			RefreshToken: "rt",
			ExpiresAt:    999,
			UserInfo:     userInfo,
		}
		assert.True(t, resp.GetValid())
		assert.Equal(t, "at", resp.GetAccessToken())
		assert.Equal(t, "rt", resp.GetRefreshToken())
		assert.Equal(t, int64(999), resp.GetExpiresAt())
		assert.Equal(t, userInfo, resp.GetUserInfo())
	})

	t.Run("nil_user_info", func(t *testing.T) {
		resp := &VerifySignatureResponse{}
		assert.Nil(t, resp.GetUserInfo())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *VerifySignatureResponse
		assert.False(t, resp.GetValid())
		assert.Equal(t, "", resp.GetAccessToken())
		assert.Equal(t, "", resp.GetRefreshToken())
		assert.Equal(t, int64(0), resp.GetExpiresAt())
		assert.Nil(t, resp.GetUserInfo())
	})
}

func TestRefreshTokenRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &RefreshTokenRequest{RefreshToken: "rt123"}
		assert.Equal(t, "rt123", req.GetRefreshToken())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *RefreshTokenRequest
		assert.Equal(t, "", req.GetRefreshToken())
	})
}

func TestRefreshTokenResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &RefreshTokenResponse{
			Success:      true,
			AccessToken:  "at",
			RefreshToken: "rt",
			ExpiresAt:    999,
		}
		assert.True(t, resp.GetSuccess())
		assert.Equal(t, "at", resp.GetAccessToken())
		assert.Equal(t, "rt", resp.GetRefreshToken())
		assert.Equal(t, int64(999), resp.GetExpiresAt())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *RefreshTokenResponse
		assert.False(t, resp.GetSuccess())
		assert.Equal(t, "", resp.GetAccessToken())
		assert.Equal(t, "", resp.GetRefreshToken())
		assert.Equal(t, int64(0), resp.GetExpiresAt())
	})
}

func TestRevokeTokenRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &RevokeTokenRequest{Token: "tok"}
		assert.Equal(t, "tok", req.GetToken())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *RevokeTokenRequest
		assert.Equal(t, "", req.GetToken())
	})
}

func TestRevokeTokenResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &RevokeTokenResponse{Success: true}
		assert.True(t, resp.GetSuccess())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *RevokeTokenResponse
		assert.False(t, resp.GetSuccess())
	})
}

func TestVerifyTokenRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &VerifyTokenRequest{Token: "tok"}
		assert.Equal(t, "tok", req.GetToken())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *VerifyTokenRequest
		assert.Equal(t, "", req.GetToken())
	})
}

func TestVerifyTokenResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		userInfo := &UserInfo{UserId: "u1", WalletAddress: "0xABC"}
		resp := &VerifyTokenResponse{Valid: true, UserInfo: userInfo, ExpiresAt: 999}
		assert.True(t, resp.GetValid())
		assert.Equal(t, userInfo, resp.GetUserInfo())
		assert.Equal(t, int64(999), resp.GetExpiresAt())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *VerifyTokenResponse
		assert.False(t, resp.GetValid())
		assert.Nil(t, resp.GetUserInfo())
		assert.Equal(t, int64(0), resp.GetExpiresAt())
	})
}

func TestUserInfo_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		info := &UserInfo{
			UserId:        "u1",
			WalletAddress: "0xABC",
			ChainType:     "ethereum",
			Roles:         []string{"admin", "user"},
			Metadata:      map[string]string{"k1": "v1"},
		}
		assert.Equal(t, "u1", info.GetUserId())
		assert.Equal(t, "0xABC", info.GetWalletAddress())
		assert.Equal(t, "ethereum", info.GetChainType())
		assert.Equal(t, []string{"admin", "user"}, info.GetRoles())
		assert.Equal(t, map[string]string{"k1": "v1"}, info.GetMetadata())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var info *UserInfo
		assert.Equal(t, "", info.GetUserId())
		assert.Equal(t, "", info.GetWalletAddress())
		assert.Equal(t, "", info.GetChainType())
		assert.Nil(t, info.GetRoles())
		assert.Nil(t, info.GetMetadata())
	})
}

func TestAuthService_UnimplementedServer(t *testing.T) {
	server := UnimplementedAuthServiceServer{}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"GetNonce", func() error { _, err := server.GetNonce(context.Background(), &GetNonceRequest{}); return err }},
		{"VerifySignature", func() error { _, err := server.VerifySignature(context.Background(), &VerifySignatureRequest{}); return err }},
		{"RefreshToken", func() error { _, err := server.RefreshToken(context.Background(), &RefreshTokenRequest{}); return err }},
		{"RevokeToken", func() error { _, err := server.RevokeToken(context.Background(), &RevokeTokenRequest{}); return err }},
		{"VerifyToken", func() error { _, err := server.VerifyToken(context.Background(), &VerifyTokenRequest{}); return err }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			require.Error(t, err)
			assert.Equal(t, codes.Unimplemented, status.Code(err))
		})
	}
}

func TestMessage_ProtoMethods(t *testing.T) {
	msgs := []interface {
		Reset()
		String() string
		ProtoMessage()
	}{
		&GetNonceRequest{},
		&GetNonceResponse{},
		&VerifySignatureRequest{},
		&VerifySignatureResponse{},
		&RefreshTokenRequest{},
		&RefreshTokenResponse{},
		&RevokeTokenRequest{},
		&RevokeTokenResponse{},
		&VerifyTokenRequest{},
		&VerifyTokenResponse{},
		&UserInfo{},
	}

	for _, msg := range msgs {
		assert.NotPanics(t, msg.Reset)
		assert.NotPanics(t, msg.ProtoMessage)
		_ = msg.String()
	}
}

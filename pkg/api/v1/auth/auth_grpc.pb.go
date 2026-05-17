package authv1

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

type AuthServiceClient interface {
	GetNonce(ctx context.Context, in *GetNonceRequest, opts ...grpc.CallOption) (*GetNonceResponse, error)
	VerifySignature(ctx context.Context, in *VerifySignatureRequest, opts ...grpc.CallOption) (*VerifySignatureResponse, error)
	RefreshToken(ctx context.Context, in *RefreshTokenRequest, opts ...grpc.CallOption) (*RefreshTokenResponse, error)
	RevokeToken(ctx context.Context, in *RevokeTokenRequest, opts ...grpc.CallOption) (*RevokeTokenResponse, error)
	VerifyToken(ctx context.Context, in *VerifyTokenRequest, opts ...grpc.CallOption) (*VerifyTokenResponse, error)
}

type authServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewAuthServiceClient(cc grpc.ClientConnInterface) AuthServiceClient {
	return &authServiceClient{cc}
}

func (c *authServiceClient) GetNonce(ctx context.Context, in *GetNonceRequest, opts ...grpc.CallOption) (*GetNonceResponse, error) {
	out := new(GetNonceResponse)
	err := c.cc.Invoke(ctx, "/auth.v1.AuthService/GetNonce", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *authServiceClient) VerifySignature(ctx context.Context, in *VerifySignatureRequest, opts ...grpc.CallOption) (*VerifySignatureResponse, error) {
	out := new(VerifySignatureResponse)
	err := c.cc.Invoke(ctx, "/auth.v1.AuthService/VerifySignature", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *authServiceClient) RefreshToken(ctx context.Context, in *RefreshTokenRequest, opts ...grpc.CallOption) (*RefreshTokenResponse, error) {
	out := new(RefreshTokenResponse)
	err := c.cc.Invoke(ctx, "/auth.v1.AuthService/RefreshToken", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *authServiceClient) RevokeToken(ctx context.Context, in *RevokeTokenRequest, opts ...grpc.CallOption) (*RevokeTokenResponse, error) {
	out := new(RevokeTokenResponse)
	err := c.cc.Invoke(ctx, "/auth.v1.AuthService/RevokeToken", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *authServiceClient) VerifyToken(ctx context.Context, in *VerifyTokenRequest, opts ...grpc.CallOption) (*VerifyTokenResponse, error) {
	out := new(VerifyTokenResponse)
	err := c.cc.Invoke(ctx, "/auth.v1.AuthService/VerifyToken", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type AuthServiceServer interface {
	GetNonce(context.Context, *GetNonceRequest) (*GetNonceResponse, error)
	VerifySignature(context.Context, *VerifySignatureRequest) (*VerifySignatureResponse, error)
	RefreshToken(context.Context, *RefreshTokenRequest) (*RefreshTokenResponse, error)
	RevokeToken(context.Context, *RevokeTokenRequest) (*RevokeTokenResponse, error)
	VerifyToken(context.Context, *VerifyTokenRequest) (*VerifyTokenResponse, error)
	mustEmbedUnimplementedAuthServiceServer()
}

type UnimplementedAuthServiceServer struct{}

func (UnimplementedAuthServiceServer) GetNonce(context.Context, *GetNonceRequest) (*GetNonceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetNonce not implemented")
}
func (UnimplementedAuthServiceServer) VerifySignature(context.Context, *VerifySignatureRequest) (*VerifySignatureResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VerifySignature not implemented")
}
func (UnimplementedAuthServiceServer) RefreshToken(context.Context, *RefreshTokenRequest) (*RefreshTokenResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RefreshToken not implemented")
}
func (UnimplementedAuthServiceServer) RevokeToken(context.Context, *RevokeTokenRequest) (*RevokeTokenResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RevokeToken not implemented")
}
func (UnimplementedAuthServiceServer) VerifyToken(context.Context, *VerifyTokenRequest) (*VerifyTokenResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VerifyToken not implemented")
}
func (UnimplementedAuthServiceServer) mustEmbedUnimplementedAuthServiceServer() {}

type UnsafeAuthServiceServer interface {
	mustEmbedUnimplementedAuthServiceServer()
}

func RegisterAuthServiceServer(s grpc.ServiceRegistrar, srv AuthServiceServer) {
	s.RegisterService(&AuthService_ServiceDesc, srv)
}

func _AuthService_GetNonce_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetNonceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AuthServiceServer).GetNonce(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/auth.v1.AuthService/GetNonce",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AuthServiceServer).GetNonce(ctx, req.(*GetNonceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AuthService_VerifySignature_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(VerifySignatureRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AuthServiceServer).VerifySignature(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/auth.v1.AuthService/VerifySignature",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AuthServiceServer).VerifySignature(ctx, req.(*VerifySignatureRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AuthService_RefreshToken_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RefreshTokenRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AuthServiceServer).RefreshToken(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/auth.v1.AuthService/RefreshToken",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AuthServiceServer).RefreshToken(ctx, req.(*RefreshTokenRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AuthService_RevokeToken_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RevokeTokenRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AuthServiceServer).RevokeToken(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/auth.v1.AuthService/RevokeToken",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AuthServiceServer).RevokeToken(ctx, req.(*RevokeTokenRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AuthService_VerifyToken_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(VerifyTokenRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AuthServiceServer).VerifyToken(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/auth.v1.AuthService/VerifyToken",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AuthServiceServer).VerifyToken(ctx, req.(*VerifyTokenRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var AuthService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "auth.v1.AuthService",
	HandlerType: (*AuthServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetNonce",
			Handler:    _AuthService_GetNonce_Handler,
		},
		{
			MethodName: "VerifySignature",
			Handler:    _AuthService_VerifySignature_Handler,
		},
		{
			MethodName: "RefreshToken",
			Handler:    _AuthService_RefreshToken_Handler,
		},
		{
			MethodName: "RevokeToken",
			Handler:    _AuthService_RevokeToken_Handler,
		},
		{
			MethodName: "VerifyToken",
			Handler:    _AuthService_VerifyToken_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/auth.proto",
}
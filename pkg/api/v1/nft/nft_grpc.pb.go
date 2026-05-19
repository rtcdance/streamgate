package nftv1

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

type NFTServiceClient interface {
	VerifyOwnership(ctx context.Context, in *VerifyOwnershipRequest, opts ...grpc.CallOption) (*VerifyOwnershipResponse, error)
	GetNFTMetadata(ctx context.Context, in *GetNFTMetadataRequest, opts ...grpc.CallOption) (*GetNFTMetadataResponse, error)
	GetNFTBalance(ctx context.Context, in *GetNFTBalanceRequest, opts ...grpc.CallOption) (*GetNFTBalanceResponse, error)
	ListUserNFTs(ctx context.Context, in *ListUserNFTsRequest, opts ...grpc.CallOption) (*ListUserNFTsResponse, error)
	GetContractInfo(ctx context.Context, in *GetContractInfoRequest, opts ...grpc.CallOption) (*GetContractInfoResponse, error)
}

type nftServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewNFTServiceClient(cc grpc.ClientConnInterface) NFTServiceClient {
	return &nftServiceClient{cc}
}

func (c *nftServiceClient) VerifyOwnership(ctx context.Context, in *VerifyOwnershipRequest, opts ...grpc.CallOption) (*VerifyOwnershipResponse, error) {
	out := new(VerifyOwnershipResponse)
	err := c.cc.Invoke(ctx, "/nft.v1.NFTService/VerifyOwnership", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *nftServiceClient) GetNFTMetadata(ctx context.Context, in *GetNFTMetadataRequest, opts ...grpc.CallOption) (*GetNFTMetadataResponse, error) {
	out := new(GetNFTMetadataResponse)
	err := c.cc.Invoke(ctx, "/nft.v1.NFTService/GetNFTMetadata", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *nftServiceClient) GetNFTBalance(ctx context.Context, in *GetNFTBalanceRequest, opts ...grpc.CallOption) (*GetNFTBalanceResponse, error) {
	out := new(GetNFTBalanceResponse)
	err := c.cc.Invoke(ctx, "/nft.v1.NFTService/GetNFTBalance", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *nftServiceClient) ListUserNFTs(ctx context.Context, in *ListUserNFTsRequest, opts ...grpc.CallOption) (*ListUserNFTsResponse, error) {
	out := new(ListUserNFTsResponse)
	err := c.cc.Invoke(ctx, "/nft.v1.NFTService/ListUserNFTs", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *nftServiceClient) GetContractInfo(ctx context.Context, in *GetContractInfoRequest, opts ...grpc.CallOption) (*GetContractInfoResponse, error) {
	out := new(GetContractInfoResponse)
	err := c.cc.Invoke(ctx, "/nft.v1.NFTService/GetContractInfo", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type NFTServiceServer interface {
	VerifyOwnership(context.Context, *VerifyOwnershipRequest) (*VerifyOwnershipResponse, error)
	GetNFTMetadata(context.Context, *GetNFTMetadataRequest) (*GetNFTMetadataResponse, error)
	GetNFTBalance(context.Context, *GetNFTBalanceRequest) (*GetNFTBalanceResponse, error)
	ListUserNFTs(context.Context, *ListUserNFTsRequest) (*ListUserNFTsResponse, error)
	GetContractInfo(context.Context, *GetContractInfoRequest) (*GetContractInfoResponse, error)
	mustEmbedUnimplementedNFTServiceServer()
}

type UnimplementedNFTServiceServer struct{}

func (UnimplementedNFTServiceServer) VerifyOwnership(context.Context, *VerifyOwnershipRequest) (*VerifyOwnershipResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VerifyOwnership not implemented")
}
func (UnimplementedNFTServiceServer) GetNFTMetadata(context.Context, *GetNFTMetadataRequest) (*GetNFTMetadataResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetNFTMetadata not implemented")
}
func (UnimplementedNFTServiceServer) GetNFTBalance(context.Context, *GetNFTBalanceRequest) (*GetNFTBalanceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetNFTBalance not implemented")
}
func (UnimplementedNFTServiceServer) ListUserNFTs(context.Context, *ListUserNFTsRequest) (*ListUserNFTsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListUserNFTs not implemented")
}
func (UnimplementedNFTServiceServer) GetContractInfo(context.Context, *GetContractInfoRequest) (*GetContractInfoResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetContractInfo not implemented")
}
func (UnimplementedNFTServiceServer) mustEmbedUnimplementedNFTServiceServer() {}

type UnsafeNFTServiceServer interface {
	mustEmbedUnimplementedNFTServiceServer()
}

func RegisterNFTServiceServer(s grpc.ServiceRegistrar, srv NFTServiceServer) {
	s.RegisterService(&NFTService_ServiceDesc, srv)
}

func _NFTService_VerifyOwnership_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(VerifyOwnershipRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NFTServiceServer).VerifyOwnership(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nft.v1.NFTService/VerifyOwnership",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NFTServiceServer).VerifyOwnership(ctx, req.(*VerifyOwnershipRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NFTService_GetNFTMetadata_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetNFTMetadataRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NFTServiceServer).GetNFTMetadata(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nft.v1.NFTService/GetNFTMetadata",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NFTServiceServer).GetNFTMetadata(ctx, req.(*GetNFTMetadataRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NFTService_GetNFTBalance_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetNFTBalanceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NFTServiceServer).GetNFTBalance(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nft.v1.NFTService/GetNFTBalance",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NFTServiceServer).GetNFTBalance(ctx, req.(*GetNFTBalanceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NFTService_ListUserNFTs_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListUserNFTsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NFTServiceServer).ListUserNFTs(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nft.v1.NFTService/ListUserNFTs",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NFTServiceServer).ListUserNFTs(ctx, req.(*ListUserNFTsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NFTService_GetContractInfo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetContractInfoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NFTServiceServer).GetContractInfo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nft.v1.NFTService/GetContractInfo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NFTServiceServer).GetContractInfo(ctx, req.(*GetContractInfoRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var NFTService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "nft.v1.NFTService",
	HandlerType: (*NFTServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "VerifyOwnership",
			Handler:    _NFTService_VerifyOwnership_Handler,
		},
		{
			MethodName: "GetNFTMetadata",
			Handler:    _NFTService_GetNFTMetadata_Handler,
		},
		{
			MethodName: "GetNFTBalance",
			Handler:    _NFTService_GetNFTBalance_Handler,
		},
		{
			MethodName: "ListUserNFTs",
			Handler:    _NFTService_ListUserNFTs_Handler,
		},
		{
			MethodName: "GetContractInfo",
			Handler:    _NFTService_GetContractInfo_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/nft.proto",
}

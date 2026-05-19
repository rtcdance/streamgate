package servicev1

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

// ============================== HealthService ==============================

type HealthServiceClient interface {
	Check(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error)
	Watch(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (HealthService_WatchClient, error)
}

type healthServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewHealthServiceClient(cc grpc.ClientConnInterface) HealthServiceClient {
	return &healthServiceClient{cc}
}

func (c *healthServiceClient) Check(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error) {
	out := new(HealthCheckResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.HealthService/Check", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *healthServiceClient) Watch(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (HealthService_WatchClient, error) {
	stream, err := c.cc.NewStream(ctx, &HealthService_ServiceDesc.Streams[0], "/streamgate.v1.HealthService/Watch", opts...)
	if err != nil {
		return nil, err
	}
	x := &healthServiceWatchClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type HealthService_WatchClient interface {
	Recv() (*HealthCheckResponse, error)
	grpc.ClientStream
}

type healthServiceWatchClient struct {
	grpc.ClientStream
}

func (x *healthServiceWatchClient) Recv() (*HealthCheckResponse, error) {
	m := new(HealthCheckResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type HealthServiceServer interface {
	Check(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error)
	Watch(*HealthCheckRequest, HealthService_WatchServer) error
	mustEmbedUnimplementedHealthServiceServer()
}

type UnimplementedHealthServiceServer struct{}

func (UnimplementedHealthServiceServer) Check(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Check not implemented")
}
func (UnimplementedHealthServiceServer) Watch(*HealthCheckRequest, HealthService_WatchServer) error {
	return status.Errorf(codes.Unimplemented, "method Watch not implemented")
}
func (UnimplementedHealthServiceServer) mustEmbedUnimplementedHealthServiceServer() {}

type UnsafeHealthServiceServer interface {
	mustEmbedUnimplementedHealthServiceServer()
}

func RegisterHealthServiceServer(s grpc.ServiceRegistrar, srv HealthServiceServer) {
	s.RegisterService(&HealthService_ServiceDesc, srv)
}

func _HealthService_Check_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HealthCheckRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(HealthServiceServer).Check(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.HealthService/Check",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(HealthServiceServer).Check(ctx, req.(*HealthCheckRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _HealthService_Watch_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(HealthCheckRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(HealthServiceServer).Watch(m, &healthServiceWatchServer{stream})
}

type HealthService_WatchServer interface {
	Send(*HealthCheckResponse) error
	grpc.ServerStream
}

type healthServiceWatchServer struct {
	grpc.ServerStream
}

func (x *healthServiceWatchServer) Send(m *HealthCheckResponse) error {
	return x.ServerStream.SendMsg(m)
}

var HealthService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streamgate.v1.HealthService",
	HandlerType: (*HealthServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Check",
			Handler:    _HealthService_Check_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Watch",
			Handler:       _HealthService_Watch_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "proto/v1/service.proto",
}

// ============================== UploadService ==============================

type SvcUploadServiceClient interface {
	UploadFile(ctx context.Context, in *UploadFileRequest, opts ...grpc.CallOption) (*UploadFileResponse, error)
	GetUploadStatus(ctx context.Context, in *GetUploadStatusRequest, opts ...grpc.CallOption) (*UploadStatus, error)
	CompleteUpload(ctx context.Context, in *CompleteUploadRequest, opts ...grpc.CallOption) (*CompleteUploadResponse, error)
}

type svcUploadServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewSvcUploadServiceClient(cc grpc.ClientConnInterface) SvcUploadServiceClient {
	return &svcUploadServiceClient{cc}
}

func (c *svcUploadServiceClient) UploadFile(ctx context.Context, in *UploadFileRequest, opts ...grpc.CallOption) (*UploadFileResponse, error) {
	out := new(UploadFileResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.UploadService/UploadFile", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *svcUploadServiceClient) GetUploadStatus(ctx context.Context, in *GetUploadStatusRequest, opts ...grpc.CallOption) (*UploadStatus, error) {
	out := new(UploadStatus)
	err := c.cc.Invoke(ctx, "/streamgate.v1.UploadService/GetUploadStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *svcUploadServiceClient) CompleteUpload(ctx context.Context, in *CompleteUploadRequest, opts ...grpc.CallOption) (*CompleteUploadResponse, error) {
	out := new(CompleteUploadResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.UploadService/CompleteUpload", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type SvcUploadServiceServer interface {
	UploadFile(context.Context, *UploadFileRequest) (*UploadFileResponse, error)
	GetUploadStatus(context.Context, *GetUploadStatusRequest) (*UploadStatus, error)
	CompleteUpload(context.Context, *CompleteUploadRequest) (*CompleteUploadResponse, error)
	mustEmbedUnimplementedSvcUploadServiceServer()
}

type UnimplementedSvcUploadServiceServer struct{}

func (UnimplementedSvcUploadServiceServer) UploadFile(context.Context, *UploadFileRequest) (*UploadFileResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UploadFile not implemented")
}
func (UnimplementedSvcUploadServiceServer) GetUploadStatus(context.Context, *GetUploadStatusRequest) (*UploadStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetUploadStatus not implemented")
}
func (UnimplementedSvcUploadServiceServer) CompleteUpload(context.Context, *CompleteUploadRequest) (*CompleteUploadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CompleteUpload not implemented")
}
func (UnimplementedSvcUploadServiceServer) mustEmbedUnimplementedSvcUploadServiceServer() {}

func RegisterSvcUploadServiceServer(s grpc.ServiceRegistrar, srv SvcUploadServiceServer) {
	s.RegisterService(&SvcUploadService_ServiceDesc, srv)
}

func _SvcUploadService_UploadFile_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UploadFileRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcUploadServiceServer).UploadFile(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.UploadService/UploadFile",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcUploadServiceServer).UploadFile(ctx, req.(*UploadFileRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SvcUploadService_GetUploadStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetUploadStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcUploadServiceServer).GetUploadStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.UploadService/GetUploadStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcUploadServiceServer).GetUploadStatus(ctx, req.(*GetUploadStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SvcUploadService_CompleteUpload_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CompleteUploadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcUploadServiceServer).CompleteUpload(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.UploadService/CompleteUpload",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcUploadServiceServer).CompleteUpload(ctx, req.(*CompleteUploadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var SvcUploadService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streamgate.v1.UploadService",
	HandlerType: (*SvcUploadServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "UploadFile", Handler: _SvcUploadService_UploadFile_Handler},
		{MethodName: "GetUploadStatus", Handler: _SvcUploadService_GetUploadStatus_Handler},
		{MethodName: "CompleteUpload", Handler: _SvcUploadService_CompleteUpload_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/service.proto",
}

// ============================== SvcStreamingService ==============================

type SvcStreamingServiceClient interface {
	GetHLSPlaylist(ctx context.Context, in *GetPlaylistRequest, opts ...grpc.CallOption) (*PlaylistResponse, error)
	GetDASHManifest(ctx context.Context, in *GetManifestRequest, opts ...grpc.CallOption) (*ManifestResponse, error)
	GetSegment(ctx context.Context, in *GetSegmentRequest, opts ...grpc.CallOption) (*SegmentResponse, error)
}

type svcStreamingServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewSvcStreamingServiceClient(cc grpc.ClientConnInterface) SvcStreamingServiceClient {
	return &svcStreamingServiceClient{cc}
}

func (c *svcStreamingServiceClient) GetHLSPlaylist(ctx context.Context, in *GetPlaylistRequest, opts ...grpc.CallOption) (*PlaylistResponse, error) {
	out := new(PlaylistResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.StreamingService/GetHLSPlaylist", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *svcStreamingServiceClient) GetDASHManifest(ctx context.Context, in *GetManifestRequest, opts ...grpc.CallOption) (*ManifestResponse, error) {
	out := new(ManifestResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.StreamingService/GetDASHManifest", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *svcStreamingServiceClient) GetSegment(ctx context.Context, in *GetSegmentRequest, opts ...grpc.CallOption) (*SegmentResponse, error) {
	out := new(SegmentResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.StreamingService/GetSegment", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type SvcStreamingServiceServer interface {
	GetHLSPlaylist(context.Context, *GetPlaylistRequest) (*PlaylistResponse, error)
	GetDASHManifest(context.Context, *GetManifestRequest) (*ManifestResponse, error)
	GetSegment(context.Context, *GetSegmentRequest) (*SegmentResponse, error)
	mustEmbedUnimplementedSvcStreamingServiceServer()
}

type UnimplementedSvcStreamingServiceServer struct{}

func (UnimplementedSvcStreamingServiceServer) GetHLSPlaylist(context.Context, *GetPlaylistRequest) (*PlaylistResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetHLSPlaylist not implemented")
}
func (UnimplementedSvcStreamingServiceServer) GetDASHManifest(context.Context, *GetManifestRequest) (*ManifestResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetDASHManifest not implemented")
}
func (UnimplementedSvcStreamingServiceServer) GetSegment(context.Context, *GetSegmentRequest) (*SegmentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSegment not implemented")
}
func (UnimplementedSvcStreamingServiceServer) mustEmbedUnimplementedSvcStreamingServiceServer() {}

func RegisterSvcStreamingServiceServer(s grpc.ServiceRegistrar, srv SvcStreamingServiceServer) {
	s.RegisterService(&SvcStreamingService_ServiceDesc, srv)
}

func _SvcStreamingService_GetHLSPlaylist_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetPlaylistRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcStreamingServiceServer).GetHLSPlaylist(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.StreamingService/GetHLSPlaylist",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcStreamingServiceServer).GetHLSPlaylist(ctx, req.(*GetPlaylistRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SvcStreamingService_GetDASHManifest_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetManifestRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcStreamingServiceServer).GetDASHManifest(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.StreamingService/GetDASHManifest",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcStreamingServiceServer).GetDASHManifest(ctx, req.(*GetManifestRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SvcStreamingService_GetSegment_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetSegmentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcStreamingServiceServer).GetSegment(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.StreamingService/GetSegment",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcStreamingServiceServer).GetSegment(ctx, req.(*GetSegmentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var SvcStreamingService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streamgate.v1.StreamingService",
	HandlerType: (*SvcStreamingServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetHLSPlaylist", Handler: _SvcStreamingService_GetHLSPlaylist_Handler},
		{MethodName: "GetDASHManifest", Handler: _SvcStreamingService_GetDASHManifest_Handler},
		{MethodName: "GetSegment", Handler: _SvcStreamingService_GetSegment_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/service.proto",
}

// ============================== MetadataService ==============================

type MetadataServiceClient interface {
	GetMetadata(ctx context.Context, in *GetMetadataRequest, opts ...grpc.CallOption) (*Metadata, error)
	CreateMetadata(ctx context.Context, in *CreateMetadataRequest, opts ...grpc.CallOption) (*Metadata, error)
	UpdateMetadata(ctx context.Context, in *UpdateMetadataRequest, opts ...grpc.CallOption) (*Metadata, error)
	DeleteMetadata(ctx context.Context, in *DeleteMetadataRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	SearchMetadata(ctx context.Context, in *SearchMetadataRequest, opts ...grpc.CallOption) (*SearchMetadataResponse, error)
}

type metadataServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewMetadataServiceClient(cc grpc.ClientConnInterface) MetadataServiceClient {
	return &metadataServiceClient{cc}
}

func (c *metadataServiceClient) GetMetadata(ctx context.Context, in *GetMetadataRequest, opts ...grpc.CallOption) (*Metadata, error) {
	out := new(Metadata)
	err := c.cc.Invoke(ctx, "/streamgate.v1.MetadataService/GetMetadata", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *metadataServiceClient) CreateMetadata(ctx context.Context, in *CreateMetadataRequest, opts ...grpc.CallOption) (*Metadata, error) {
	out := new(Metadata)
	err := c.cc.Invoke(ctx, "/streamgate.v1.MetadataService/CreateMetadata", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *metadataServiceClient) UpdateMetadata(ctx context.Context, in *UpdateMetadataRequest, opts ...grpc.CallOption) (*Metadata, error) {
	out := new(Metadata)
	err := c.cc.Invoke(ctx, "/streamgate.v1.MetadataService/UpdateMetadata", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *metadataServiceClient) DeleteMetadata(ctx context.Context, in *DeleteMetadataRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/streamgate.v1.MetadataService/DeleteMetadata", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *metadataServiceClient) SearchMetadata(ctx context.Context, in *SearchMetadataRequest, opts ...grpc.CallOption) (*SearchMetadataResponse, error) {
	out := new(SearchMetadataResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.MetadataService/SearchMetadata", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type MetadataServiceServer interface {
	GetMetadata(context.Context, *GetMetadataRequest) (*Metadata, error)
	CreateMetadata(context.Context, *CreateMetadataRequest) (*Metadata, error)
	UpdateMetadata(context.Context, *UpdateMetadataRequest) (*Metadata, error)
	DeleteMetadata(context.Context, *DeleteMetadataRequest) (*emptypb.Empty, error)
	SearchMetadata(context.Context, *SearchMetadataRequest) (*SearchMetadataResponse, error)
	mustEmbedUnimplementedMetadataServiceServer()
}

type UnimplementedMetadataServiceServer struct{}

func (UnimplementedMetadataServiceServer) GetMetadata(context.Context, *GetMetadataRequest) (*Metadata, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetMetadata not implemented")
}
func (UnimplementedMetadataServiceServer) CreateMetadata(context.Context, *CreateMetadataRequest) (*Metadata, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateMetadata not implemented")
}
func (UnimplementedMetadataServiceServer) UpdateMetadata(context.Context, *UpdateMetadataRequest) (*Metadata, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateMetadata not implemented")
}
func (UnimplementedMetadataServiceServer) DeleteMetadata(context.Context, *DeleteMetadataRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteMetadata not implemented")
}
func (UnimplementedMetadataServiceServer) SearchMetadata(context.Context, *SearchMetadataRequest) (*SearchMetadataResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SearchMetadata not implemented")
}
func (UnimplementedMetadataServiceServer) mustEmbedUnimplementedMetadataServiceServer() {}

func RegisterMetadataServiceServer(s grpc.ServiceRegistrar, srv MetadataServiceServer) {
	s.RegisterService(&MetadataService_ServiceDesc, srv)
}

func _MetadataService_GetMetadata_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetMetadataRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MetadataServiceServer).GetMetadata(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.MetadataService/GetMetadata",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MetadataServiceServer).GetMetadata(ctx, req.(*GetMetadataRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _MetadataService_CreateMetadata_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateMetadataRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MetadataServiceServer).CreateMetadata(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.MetadataService/CreateMetadata",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MetadataServiceServer).CreateMetadata(ctx, req.(*CreateMetadataRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _MetadataService_UpdateMetadata_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UpdateMetadataRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MetadataServiceServer).UpdateMetadata(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.MetadataService/UpdateMetadata",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MetadataServiceServer).UpdateMetadata(ctx, req.(*UpdateMetadataRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _MetadataService_DeleteMetadata_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteMetadataRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MetadataServiceServer).DeleteMetadata(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.MetadataService/DeleteMetadata",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MetadataServiceServer).DeleteMetadata(ctx, req.(*DeleteMetadataRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _MetadataService_SearchMetadata_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SearchMetadataRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MetadataServiceServer).SearchMetadata(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.MetadataService/SearchMetadata",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MetadataServiceServer).SearchMetadata(ctx, req.(*SearchMetadataRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var MetadataService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streamgate.v1.MetadataService",
	HandlerType: (*MetadataServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetMetadata", Handler: _MetadataService_GetMetadata_Handler},
		{MethodName: "CreateMetadata", Handler: _MetadataService_CreateMetadata_Handler},
		{MethodName: "UpdateMetadata", Handler: _MetadataService_UpdateMetadata_Handler},
		{MethodName: "DeleteMetadata", Handler: _MetadataService_DeleteMetadata_Handler},
		{MethodName: "SearchMetadata", Handler: _MetadataService_SearchMetadata_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/service.proto",
}

// ============================== SvcAuthService ==============================

type SvcAuthServiceClient interface {
	VerifySignature(ctx context.Context, in *VerifySignatureRequest, opts ...grpc.CallOption) (*VerifySignatureResponse, error)
	VerifyNFT(ctx context.Context, in *VerifyNFTRequest, opts ...grpc.CallOption) (*VerifyNFTResponse, error)
	VerifyToken(ctx context.Context, in *VerifyTokenRequest, opts ...grpc.CallOption) (*VerifyTokenResponse, error)
	GetChallenge(ctx context.Context, in *GetChallengeRequest, opts ...grpc.CallOption) (*GetChallengeResponse, error)
}

type svcAuthServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewSvcAuthServiceClient(cc grpc.ClientConnInterface) SvcAuthServiceClient {
	return &svcAuthServiceClient{cc}
}

func (c *svcAuthServiceClient) VerifySignature(ctx context.Context, in *VerifySignatureRequest, opts ...grpc.CallOption) (*VerifySignatureResponse, error) {
	out := new(VerifySignatureResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.AuthService/VerifySignature", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *svcAuthServiceClient) VerifyNFT(ctx context.Context, in *VerifyNFTRequest, opts ...grpc.CallOption) (*VerifyNFTResponse, error) {
	out := new(VerifyNFTResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.AuthService/VerifyNFT", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *svcAuthServiceClient) VerifyToken(ctx context.Context, in *VerifyTokenRequest, opts ...grpc.CallOption) (*VerifyTokenResponse, error) {
	out := new(VerifyTokenResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.AuthService/VerifyToken", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *svcAuthServiceClient) GetChallenge(ctx context.Context, in *GetChallengeRequest, opts ...grpc.CallOption) (*GetChallengeResponse, error) {
	out := new(GetChallengeResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.AuthService/GetChallenge", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type SvcAuthServiceServer interface {
	VerifySignature(context.Context, *VerifySignatureRequest) (*VerifySignatureResponse, error)
	VerifyNFT(context.Context, *VerifyNFTRequest) (*VerifyNFTResponse, error)
	VerifyToken(context.Context, *VerifyTokenRequest) (*VerifyTokenResponse, error)
	GetChallenge(context.Context, *GetChallengeRequest) (*GetChallengeResponse, error)
	mustEmbedUnimplementedSvcAuthServiceServer()
}

type UnimplementedSvcAuthServiceServer struct{}

func (UnimplementedSvcAuthServiceServer) VerifySignature(context.Context, *VerifySignatureRequest) (*VerifySignatureResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VerifySignature not implemented")
}
func (UnimplementedSvcAuthServiceServer) VerifyNFT(context.Context, *VerifyNFTRequest) (*VerifyNFTResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VerifyNFT not implemented")
}
func (UnimplementedSvcAuthServiceServer) VerifyToken(context.Context, *VerifyTokenRequest) (*VerifyTokenResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VerifyToken not implemented")
}
func (UnimplementedSvcAuthServiceServer) GetChallenge(context.Context, *GetChallengeRequest) (*GetChallengeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetChallenge not implemented")
}
func (UnimplementedSvcAuthServiceServer) mustEmbedUnimplementedSvcAuthServiceServer() {}

func RegisterSvcAuthServiceServer(s grpc.ServiceRegistrar, srv SvcAuthServiceServer) {
	s.RegisterService(&SvcAuthService_ServiceDesc, srv)
}

func _SvcAuthService_VerifySignature_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(VerifySignatureRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcAuthServiceServer).VerifySignature(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.AuthService/VerifySignature",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcAuthServiceServer).VerifySignature(ctx, req.(*VerifySignatureRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SvcAuthService_VerifyNFT_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(VerifyNFTRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcAuthServiceServer).VerifyNFT(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.AuthService/VerifyNFT",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcAuthServiceServer).VerifyNFT(ctx, req.(*VerifyNFTRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SvcAuthService_VerifyToken_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(VerifyTokenRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcAuthServiceServer).VerifyToken(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.AuthService/VerifyToken",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcAuthServiceServer).VerifyToken(ctx, req.(*VerifyTokenRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SvcAuthService_GetChallenge_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetChallengeRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SvcAuthServiceServer).GetChallenge(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.AuthService/GetChallenge",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SvcAuthServiceServer).GetChallenge(ctx, req.(*GetChallengeRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var SvcAuthService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streamgate.v1.AuthService",
	HandlerType: (*SvcAuthServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "VerifySignature", Handler: _SvcAuthService_VerifySignature_Handler},
		{MethodName: "VerifyNFT", Handler: _SvcAuthService_VerifyNFT_Handler},
		{MethodName: "VerifyToken", Handler: _SvcAuthService_VerifyToken_Handler},
		{MethodName: "GetChallenge", Handler: _SvcAuthService_GetChallenge_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/service.proto",
}

// ============================== CacheService ==============================

type CacheServiceClient interface {
	Get(ctx context.Context, in *GetCacheRequest, opts ...grpc.CallOption) (*GetCacheResponse, error)
	Set(ctx context.Context, in *SetCacheRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	Delete(ctx context.Context, in *DeleteCacheRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	Clear(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

type cacheServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewCacheServiceClient(cc grpc.ClientConnInterface) CacheServiceClient {
	return &cacheServiceClient{cc}
}

func (c *cacheServiceClient) Get(ctx context.Context, in *GetCacheRequest, opts ...grpc.CallOption) (*GetCacheResponse, error) {
	out := new(GetCacheResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.CacheService/Get", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cacheServiceClient) Set(ctx context.Context, in *SetCacheRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/streamgate.v1.CacheService/Set", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cacheServiceClient) Delete(ctx context.Context, in *DeleteCacheRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/streamgate.v1.CacheService/Delete", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cacheServiceClient) Clear(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/streamgate.v1.CacheService/Clear", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type CacheServiceServer interface {
	Get(context.Context, *GetCacheRequest) (*GetCacheResponse, error)
	Set(context.Context, *SetCacheRequest) (*emptypb.Empty, error)
	Delete(context.Context, *DeleteCacheRequest) (*emptypb.Empty, error)
	Clear(context.Context, *emptypb.Empty) (*emptypb.Empty, error)
	mustEmbedUnimplementedCacheServiceServer()
}

type UnimplementedCacheServiceServer struct{}

func (UnimplementedCacheServiceServer) Get(context.Context, *GetCacheRequest) (*GetCacheResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Get not implemented")
}
func (UnimplementedCacheServiceServer) Set(context.Context, *SetCacheRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Set not implemented")
}
func (UnimplementedCacheServiceServer) Delete(context.Context, *DeleteCacheRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Delete not implemented")
}
func (UnimplementedCacheServiceServer) Clear(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Clear not implemented")
}
func (UnimplementedCacheServiceServer) mustEmbedUnimplementedCacheServiceServer() {}

func RegisterCacheServiceServer(s grpc.ServiceRegistrar, srv CacheServiceServer) {
	s.RegisterService(&CacheService_ServiceDesc, srv)
}

func _CacheService_Get_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetCacheRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CacheServiceServer).Get(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.CacheService/Get",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CacheServiceServer).Get(ctx, req.(*GetCacheRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CacheService_Set_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SetCacheRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CacheServiceServer).Set(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.CacheService/Set",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CacheServiceServer).Set(ctx, req.(*SetCacheRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CacheService_Delete_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteCacheRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CacheServiceServer).Delete(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.CacheService/Delete",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CacheServiceServer).Delete(ctx, req.(*DeleteCacheRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CacheService_Clear_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CacheServiceServer).Clear(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.CacheService/Clear",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CacheServiceServer).Clear(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

var CacheService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streamgate.v1.CacheService",
	HandlerType: (*CacheServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Get", Handler: _CacheService_Get_Handler},
		{MethodName: "Set", Handler: _CacheService_Set_Handler},
		{MethodName: "Delete", Handler: _CacheService_Delete_Handler},
		{MethodName: "Clear", Handler: _CacheService_Clear_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/service.proto",
}

// ============================== TranscoderService ==============================

type TranscoderServiceClient interface {
	SubmitJob(ctx context.Context, in *SubmitJobRequest, opts ...grpc.CallOption) (*SubmitJobResponse, error)
	GetJobStatus(ctx context.Context, in *GetJobStatusRequest, opts ...grpc.CallOption) (*JobStatus, error)
	CancelJob(ctx context.Context, in *CancelJobRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

type transcoderServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTranscoderServiceClient(cc grpc.ClientConnInterface) TranscoderServiceClient {
	return &transcoderServiceClient{cc}
}

func (c *transcoderServiceClient) SubmitJob(ctx context.Context, in *SubmitJobRequest, opts ...grpc.CallOption) (*SubmitJobResponse, error) {
	out := new(SubmitJobResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.TranscoderService/SubmitJob", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *transcoderServiceClient) GetJobStatus(ctx context.Context, in *GetJobStatusRequest, opts ...grpc.CallOption) (*JobStatus, error) {
	out := new(JobStatus)
	err := c.cc.Invoke(ctx, "/streamgate.v1.TranscoderService/GetJobStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *transcoderServiceClient) CancelJob(ctx context.Context, in *CancelJobRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/streamgate.v1.TranscoderService/CancelJob", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type TranscoderServiceServer interface {
	SubmitJob(context.Context, *SubmitJobRequest) (*SubmitJobResponse, error)
	GetJobStatus(context.Context, *GetJobStatusRequest) (*JobStatus, error)
	CancelJob(context.Context, *CancelJobRequest) (*emptypb.Empty, error)
	mustEmbedUnimplementedTranscoderServiceServer()
}

type UnimplementedTranscoderServiceServer struct{}

func (UnimplementedTranscoderServiceServer) SubmitJob(context.Context, *SubmitJobRequest) (*SubmitJobResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SubmitJob not implemented")
}
func (UnimplementedTranscoderServiceServer) GetJobStatus(context.Context, *GetJobStatusRequest) (*JobStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetJobStatus not implemented")
}
func (UnimplementedTranscoderServiceServer) CancelJob(context.Context, *CancelJobRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CancelJob not implemented")
}
func (UnimplementedTranscoderServiceServer) mustEmbedUnimplementedTranscoderServiceServer() {}

func RegisterTranscoderServiceServer(s grpc.ServiceRegistrar, srv TranscoderServiceServer) {
	s.RegisterService(&TranscoderService_ServiceDesc, srv)
}

func _TranscoderService_SubmitJob_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SubmitJobRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TranscoderServiceServer).SubmitJob(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.TranscoderService/SubmitJob",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TranscoderServiceServer).SubmitJob(ctx, req.(*SubmitJobRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TranscoderService_GetJobStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetJobStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TranscoderServiceServer).GetJobStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.TranscoderService/GetJobStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TranscoderServiceServer).GetJobStatus(ctx, req.(*GetJobStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _TranscoderService_CancelJob_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CancelJobRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TranscoderServiceServer).CancelJob(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.TranscoderService/CancelJob",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TranscoderServiceServer).CancelJob(ctx, req.(*CancelJobRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var TranscoderService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streamgate.v1.TranscoderService",
	HandlerType: (*TranscoderServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "SubmitJob", Handler: _TranscoderService_SubmitJob_Handler},
		{MethodName: "GetJobStatus", Handler: _TranscoderService_GetJobStatus_Handler},
		{MethodName: "CancelJob", Handler: _TranscoderService_CancelJob_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/service.proto",
}

// ============================== WorkerService ==============================

type WorkerServiceClient interface {
	SubmitJob(ctx context.Context, in *SubmitWorkerJobRequest, opts ...grpc.CallOption) (*SubmitWorkerJobResponse, error)
	GetJobStatus(ctx context.Context, in *GetWorkerJobStatusRequest, opts ...grpc.CallOption) (*WorkerJobStatus, error)
	CancelJob(ctx context.Context, in *CancelWorkerJobRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ScheduleJob(ctx context.Context, in *ScheduleJobRequest, opts ...grpc.CallOption) (*ScheduleJobResponse, error)
}

type workerServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewWorkerServiceClient(cc grpc.ClientConnInterface) WorkerServiceClient {
	return &workerServiceClient{cc}
}

func (c *workerServiceClient) SubmitJob(ctx context.Context, in *SubmitWorkerJobRequest, opts ...grpc.CallOption) (*SubmitWorkerJobResponse, error) {
	out := new(SubmitWorkerJobResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.WorkerService/SubmitJob", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *workerServiceClient) GetJobStatus(ctx context.Context, in *GetWorkerJobStatusRequest, opts ...grpc.CallOption) (*WorkerJobStatus, error) {
	out := new(WorkerJobStatus)
	err := c.cc.Invoke(ctx, "/streamgate.v1.WorkerService/GetJobStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *workerServiceClient) CancelJob(ctx context.Context, in *CancelWorkerJobRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/streamgate.v1.WorkerService/CancelJob", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *workerServiceClient) ScheduleJob(ctx context.Context, in *ScheduleJobRequest, opts ...grpc.CallOption) (*ScheduleJobResponse, error) {
	out := new(ScheduleJobResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.WorkerService/ScheduleJob", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type WorkerServiceServer interface {
	SubmitJob(context.Context, *SubmitWorkerJobRequest) (*SubmitWorkerJobResponse, error)
	GetJobStatus(context.Context, *GetWorkerJobStatusRequest) (*WorkerJobStatus, error)
	CancelJob(context.Context, *CancelWorkerJobRequest) (*emptypb.Empty, error)
	ScheduleJob(context.Context, *ScheduleJobRequest) (*ScheduleJobResponse, error)
	mustEmbedUnimplementedWorkerServiceServer()
}

type UnimplementedWorkerServiceServer struct{}

func (UnimplementedWorkerServiceServer) SubmitJob(context.Context, *SubmitWorkerJobRequest) (*SubmitWorkerJobResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SubmitJob not implemented")
}
func (UnimplementedWorkerServiceServer) GetJobStatus(context.Context, *GetWorkerJobStatusRequest) (*WorkerJobStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetJobStatus not implemented")
}
func (UnimplementedWorkerServiceServer) CancelJob(context.Context, *CancelWorkerJobRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CancelJob not implemented")
}
func (UnimplementedWorkerServiceServer) ScheduleJob(context.Context, *ScheduleJobRequest) (*ScheduleJobResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ScheduleJob not implemented")
}
func (UnimplementedWorkerServiceServer) mustEmbedUnimplementedWorkerServiceServer() {}

func RegisterWorkerServiceServer(s grpc.ServiceRegistrar, srv WorkerServiceServer) {
	s.RegisterService(&WorkerService_ServiceDesc, srv)
}

func _WorkerService_SubmitJob_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SubmitWorkerJobRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerServiceServer).SubmitJob(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.WorkerService/SubmitJob",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerServiceServer).SubmitJob(ctx, req.(*SubmitWorkerJobRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _WorkerService_GetJobStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetWorkerJobStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerServiceServer).GetJobStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.WorkerService/GetJobStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerServiceServer).GetJobStatus(ctx, req.(*GetWorkerJobStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _WorkerService_CancelJob_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CancelWorkerJobRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerServiceServer).CancelJob(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.WorkerService/CancelJob",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerServiceServer).CancelJob(ctx, req.(*CancelWorkerJobRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _WorkerService_ScheduleJob_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ScheduleJobRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerServiceServer).ScheduleJob(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.WorkerService/ScheduleJob",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerServiceServer).ScheduleJob(ctx, req.(*ScheduleJobRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var WorkerService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streamgate.v1.WorkerService",
	HandlerType: (*WorkerServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "SubmitJob", Handler: _WorkerService_SubmitJob_Handler},
		{MethodName: "GetJobStatus", Handler: _WorkerService_GetJobStatus_Handler},
		{MethodName: "CancelJob", Handler: _WorkerService_CancelJob_Handler},
		{MethodName: "ScheduleJob", Handler: _WorkerService_ScheduleJob_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/service.proto",
}

// ============================== MonitorService ==============================

type MonitorServiceClient interface {
	GetHealth(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*HealthStatus, error)
	GetMetrics(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*SystemMetrics, error)
	GetAlerts(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*AlertsResponse, error)
}

type monitorServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewMonitorServiceClient(cc grpc.ClientConnInterface) MonitorServiceClient {
	return &monitorServiceClient{cc}
}

func (c *monitorServiceClient) GetHealth(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*HealthStatus, error) {
	out := new(HealthStatus)
	err := c.cc.Invoke(ctx, "/streamgate.v1.MonitorService/GetHealth", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *monitorServiceClient) GetMetrics(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*SystemMetrics, error) {
	out := new(SystemMetrics)
	err := c.cc.Invoke(ctx, "/streamgate.v1.MonitorService/GetMetrics", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *monitorServiceClient) GetAlerts(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*AlertsResponse, error) {
	out := new(AlertsResponse)
	err := c.cc.Invoke(ctx, "/streamgate.v1.MonitorService/GetAlerts", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type MonitorServiceServer interface {
	GetHealth(context.Context, *emptypb.Empty) (*HealthStatus, error)
	GetMetrics(context.Context, *emptypb.Empty) (*SystemMetrics, error)
	GetAlerts(context.Context, *emptypb.Empty) (*AlertsResponse, error)
	mustEmbedUnimplementedMonitorServiceServer()
}

type UnimplementedMonitorServiceServer struct{}

func (UnimplementedMonitorServiceServer) GetHealth(context.Context, *emptypb.Empty) (*HealthStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetHealth not implemented")
}
func (UnimplementedMonitorServiceServer) GetMetrics(context.Context, *emptypb.Empty) (*SystemMetrics, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetMetrics not implemented")
}
func (UnimplementedMonitorServiceServer) GetAlerts(context.Context, *emptypb.Empty) (*AlertsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetAlerts not implemented")
}
func (UnimplementedMonitorServiceServer) mustEmbedUnimplementedMonitorServiceServer() {}

func RegisterMonitorServiceServer(s grpc.ServiceRegistrar, srv MonitorServiceServer) {
	s.RegisterService(&MonitorService_ServiceDesc, srv)
}

func _MonitorService_GetHealth_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MonitorServiceServer).GetHealth(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.MonitorService/GetHealth",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MonitorServiceServer).GetHealth(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _MonitorService_GetMetrics_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MonitorServiceServer).GetMetrics(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.MonitorService/GetMetrics",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MonitorServiceServer).GetMetrics(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _MonitorService_GetAlerts_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MonitorServiceServer).GetAlerts(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/streamgate.v1.MonitorService/GetAlerts",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MonitorServiceServer).GetAlerts(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

var MonitorService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "streamgate.v1.MonitorService",
	HandlerType: (*MonitorServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetHealth", Handler: _MonitorService_GetHealth_Handler},
		{MethodName: "GetMetrics", Handler: _MonitorService_GetMetrics_Handler},
		{MethodName: "GetAlerts", Handler: _MonitorService_GetAlerts_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/service.proto",
}

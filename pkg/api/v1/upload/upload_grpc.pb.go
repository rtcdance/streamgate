package uploadv1

import (
	context "context"
	io "io"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

type UploadServiceClient interface {
	InitUpload(ctx context.Context, in *InitUploadRequest, opts ...grpc.CallOption) (*InitUploadResponse, error)
	UploadPart(ctx context.Context, opts ...grpc.CallOption) (UploadService_UploadPartClient, error)
	CompleteUpload(ctx context.Context, in *CompleteUploadRequest, opts ...grpc.CallOption) (*CompleteUploadResponse, error)
	AbortUpload(ctx context.Context, in *AbortUploadRequest, opts ...grpc.CallOption) (*AbortUploadResponse, error)
	GetUploadStatus(ctx context.Context, in *GetUploadStatusRequest, opts ...grpc.CallOption) (*GetUploadStatusResponse, error)
}

type uploadServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewUploadServiceClient(cc grpc.ClientConnInterface) UploadServiceClient {
	return &uploadServiceClient{cc}
}

func (c *uploadServiceClient) InitUpload(ctx context.Context, in *InitUploadRequest, opts ...grpc.CallOption) (*InitUploadResponse, error) {
	out := new(InitUploadResponse)
	err := c.cc.Invoke(ctx, "/upload.v1.UploadService/InitUpload", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *uploadServiceClient) UploadPart(ctx context.Context, opts ...grpc.CallOption) (UploadService_UploadPartClient, error) {
	stream, err := c.cc.NewStream(ctx, &UploadService_ServiceDesc.Streams[0], "/upload.v1.UploadService/UploadPart", opts...)
	if err != nil {
		return nil, err
	}
	x := &uploadServiceUploadPartClient{stream}
	return x, nil
}

type UploadService_UploadPartClient interface {
	Send(*UploadPartRequest) error
	CloseAndRecv() (*UploadPartResponse, error)
	grpc.ClientStream
}

type uploadServiceUploadPartClient struct {
	grpc.ClientStream
}

func (x *uploadServiceUploadPartClient) Send(m *UploadPartRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *uploadServiceUploadPartClient) CloseAndRecv() (*UploadPartResponse, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(UploadPartResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *uploadServiceClient) CompleteUpload(ctx context.Context, in *CompleteUploadRequest, opts ...grpc.CallOption) (*CompleteUploadResponse, error) {
	out := new(CompleteUploadResponse)
	err := c.cc.Invoke(ctx, "/upload.v1.UploadService/CompleteUpload", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *uploadServiceClient) AbortUpload(ctx context.Context, in *AbortUploadRequest, opts ...grpc.CallOption) (*AbortUploadResponse, error) {
	out := new(AbortUploadResponse)
	err := c.cc.Invoke(ctx, "/upload.v1.UploadService/AbortUpload", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *uploadServiceClient) GetUploadStatus(ctx context.Context, in *GetUploadStatusRequest, opts ...grpc.CallOption) (*GetUploadStatusResponse, error) {
	out := new(GetUploadStatusResponse)
	err := c.cc.Invoke(ctx, "/upload.v1.UploadService/GetUploadStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type UploadServiceServer interface {
	InitUpload(context.Context, *InitUploadRequest) (*InitUploadResponse, error)
	UploadPart(UploadService_UploadPartServer) error
	CompleteUpload(context.Context, *CompleteUploadRequest) (*CompleteUploadResponse, error)
	AbortUpload(context.Context, *AbortUploadRequest) (*AbortUploadResponse, error)
	GetUploadStatus(context.Context, *GetUploadStatusRequest) (*GetUploadStatusResponse, error)
	mustEmbedUnimplementedUploadServiceServer()
}

type UnimplementedUploadServiceServer struct{}

func (UnimplementedUploadServiceServer) InitUpload(context.Context, *InitUploadRequest) (*InitUploadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method InitUpload not implemented")
}
func (UnimplementedUploadServiceServer) UploadPart(UploadService_UploadPartServer) error {
	return status.Errorf(codes.Unimplemented, "method UploadPart not implemented")
}
func (UnimplementedUploadServiceServer) CompleteUpload(context.Context, *CompleteUploadRequest) (*CompleteUploadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CompleteUpload not implemented")
}
func (UnimplementedUploadServiceServer) AbortUpload(context.Context, *AbortUploadRequest) (*AbortUploadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AbortUpload not implemented")
}
func (UnimplementedUploadServiceServer) GetUploadStatus(context.Context, *GetUploadStatusRequest) (*GetUploadStatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetUploadStatus not implemented")
}
func (UnimplementedUploadServiceServer) mustEmbedUnimplementedUploadServiceServer() {}

type UnsafeUploadServiceServer interface {
	mustEmbedUnimplementedUploadServiceServer()
}

func RegisterUploadServiceServer(s grpc.ServiceRegistrar, srv UploadServiceServer) {
	s.RegisterService(&UploadService_ServiceDesc, srv)
}

func _UploadService_InitUpload_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(InitUploadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UploadServiceServer).InitUpload(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/upload.v1.UploadService/InitUpload",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UploadServiceServer).InitUpload(ctx, req.(*InitUploadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _UploadService_UploadPart_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(UploadServiceServer).UploadPart(&uploadServiceUploadPartServer{stream})
}

type UploadService_UploadPartServer interface {
	SendAndClose(*UploadPartResponse) error
	Recv() (*UploadPartRequest, error)
	grpc.ServerStream
}

type uploadServiceUploadPartServer struct {
	grpc.ServerStream
}

func (x *uploadServiceUploadPartServer) SendAndClose(m *UploadPartResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *uploadServiceUploadPartServer) Recv() (*UploadPartRequest, error) {
	m := new(UploadPartRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _UploadService_CompleteUpload_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CompleteUploadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UploadServiceServer).CompleteUpload(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/upload.v1.UploadService/CompleteUpload",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UploadServiceServer).CompleteUpload(ctx, req.(*CompleteUploadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _UploadService_AbortUpload_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AbortUploadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UploadServiceServer).AbortUpload(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/upload.v1.UploadService/AbortUpload",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UploadServiceServer).AbortUpload(ctx, req.(*AbortUploadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _UploadService_GetUploadStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetUploadStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UploadServiceServer).GetUploadStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/upload.v1.UploadService/GetUploadStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UploadServiceServer).GetUploadStatus(ctx, req.(*GetUploadStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var UploadService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "upload.v1.UploadService",
	HandlerType: (*UploadServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "InitUpload",
			Handler:    _UploadService_InitUpload_Handler,
		},
		{
			MethodName: "CompleteUpload",
			Handler:    _UploadService_CompleteUpload_Handler,
		},
		{
			MethodName: "AbortUpload",
			Handler:    _UploadService_AbortUpload_Handler,
		},
		{
			MethodName: "GetUploadStatus",
			Handler:    _UploadService_GetUploadStatus_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "UploadPart",
			Handler:       _UploadService_UploadPart_Handler,
			ClientStreams: true,
		},
	},
	Metadata: "proto/v1/upload.proto",
}

var _ io.Reader
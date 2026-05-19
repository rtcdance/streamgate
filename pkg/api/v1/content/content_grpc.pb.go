package contentv1

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

type ContentServiceClient interface {
	GetContent(ctx context.Context, in *GetContentRequest, opts ...grpc.CallOption) (*GetContentResponse, error)
	VerifyAccess(ctx context.Context, in *VerifyAccessRequest, opts ...grpc.CallOption) (*VerifyAccessResponse, error)
	GetTranscodeStatus(ctx context.Context, in *GetTranscodeStatusRequest, opts ...grpc.CallOption) (*GetTranscodeStatusResponse, error)
	ListContent(ctx context.Context, in *ListContentRequest, opts ...grpc.CallOption) (*ListContentResponse, error)
	DeleteContent(ctx context.Context, in *DeleteContentRequest, opts ...grpc.CallOption) (*DeleteContentResponse, error)
}

type contentServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewContentServiceClient(cc grpc.ClientConnInterface) ContentServiceClient {
	return &contentServiceClient{cc}
}

func (c *contentServiceClient) GetContent(ctx context.Context, in *GetContentRequest, opts ...grpc.CallOption) (*GetContentResponse, error) {
	out := new(GetContentResponse)
	err := c.cc.Invoke(ctx, "/content.v1.ContentService/GetContent", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *contentServiceClient) VerifyAccess(ctx context.Context, in *VerifyAccessRequest, opts ...grpc.CallOption) (*VerifyAccessResponse, error) {
	out := new(VerifyAccessResponse)
	err := c.cc.Invoke(ctx, "/content.v1.ContentService/VerifyAccess", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *contentServiceClient) GetTranscodeStatus(ctx context.Context, in *GetTranscodeStatusRequest, opts ...grpc.CallOption) (*GetTranscodeStatusResponse, error) {
	out := new(GetTranscodeStatusResponse)
	err := c.cc.Invoke(ctx, "/content.v1.ContentService/GetTranscodeStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *contentServiceClient) ListContent(ctx context.Context, in *ListContentRequest, opts ...grpc.CallOption) (*ListContentResponse, error) {
	out := new(ListContentResponse)
	err := c.cc.Invoke(ctx, "/content.v1.ContentService/ListContent", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *contentServiceClient) DeleteContent(ctx context.Context, in *DeleteContentRequest, opts ...grpc.CallOption) (*DeleteContentResponse, error) {
	out := new(DeleteContentResponse)
	err := c.cc.Invoke(ctx, "/content.v1.ContentService/DeleteContent", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type ContentServiceServer interface {
	GetContent(context.Context, *GetContentRequest) (*GetContentResponse, error)
	VerifyAccess(context.Context, *VerifyAccessRequest) (*VerifyAccessResponse, error)
	GetTranscodeStatus(context.Context, *GetTranscodeStatusRequest) (*GetTranscodeStatusResponse, error)
	ListContent(context.Context, *ListContentRequest) (*ListContentResponse, error)
	DeleteContent(context.Context, *DeleteContentRequest) (*DeleteContentResponse, error)
	mustEmbedUnimplementedContentServiceServer()
}

type UnimplementedContentServiceServer struct{}

func (UnimplementedContentServiceServer) GetContent(context.Context, *GetContentRequest) (*GetContentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetContent not implemented")
}
func (UnimplementedContentServiceServer) VerifyAccess(context.Context, *VerifyAccessRequest) (*VerifyAccessResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VerifyAccess not implemented")
}
func (UnimplementedContentServiceServer) GetTranscodeStatus(context.Context, *GetTranscodeStatusRequest) (*GetTranscodeStatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTranscodeStatus not implemented")
}
func (UnimplementedContentServiceServer) ListContent(context.Context, *ListContentRequest) (*ListContentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListContent not implemented")
}
func (UnimplementedContentServiceServer) DeleteContent(context.Context, *DeleteContentRequest) (*DeleteContentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteContent not implemented")
}
func (UnimplementedContentServiceServer) mustEmbedUnimplementedContentServiceServer() {}

type UnsafeContentServiceServer interface {
	mustEmbedUnimplementedContentServiceServer()
}

func RegisterContentServiceServer(s grpc.ServiceRegistrar, srv ContentServiceServer) {
	s.RegisterService(&ContentService_ServiceDesc, srv)
}

func _ContentService_GetContent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetContentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ContentServiceServer).GetContent(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/content.v1.ContentService/GetContent",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ContentServiceServer).GetContent(ctx, req.(*GetContentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ContentService_VerifyAccess_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(VerifyAccessRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ContentServiceServer).VerifyAccess(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/content.v1.ContentService/VerifyAccess",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ContentServiceServer).VerifyAccess(ctx, req.(*VerifyAccessRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ContentService_GetTranscodeStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetTranscodeStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ContentServiceServer).GetTranscodeStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/content.v1.ContentService/GetTranscodeStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ContentServiceServer).GetTranscodeStatus(ctx, req.(*GetTranscodeStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ContentService_ListContent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListContentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ContentServiceServer).ListContent(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/content.v1.ContentService/ListContent",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ContentServiceServer).ListContent(ctx, req.(*ListContentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ContentService_DeleteContent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteContentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ContentServiceServer).DeleteContent(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/content.v1.ContentService/DeleteContent",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ContentServiceServer).DeleteContent(ctx, req.(*DeleteContentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var ContentService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "content.v1.ContentService",
	HandlerType: (*ContentServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetContent",
			Handler:    _ContentService_GetContent_Handler,
		},
		{
			MethodName: "VerifyAccess",
			Handler:    _ContentService_VerifyAccess_Handler,
		},
		{
			MethodName: "GetTranscodeStatus",
			Handler:    _ContentService_GetTranscodeStatus_Handler,
		},
		{
			MethodName: "ListContent",
			Handler:    _ContentService_ListContent_Handler,
		},
		{
			MethodName: "DeleteContent",
			Handler:    _ContentService_DeleteContent_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v1/content.proto",
}
